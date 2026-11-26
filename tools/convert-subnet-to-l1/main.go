package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/tbcasoft/avalanche-deploy/tools/core/helpers"
)

func main() {

	flag.StringVar(&helpers.NetworkName, "network", "fuji", "Network: fuji or mainnet")
	flag.StringVar(&helpers.RpcUrL, "rpcurl", "", "Rpc node http endpoint (e.g. http://18.219.181.165:9650)")
	flag.StringVar(&helpers.SubnetIDStr, "subnetidstr", "", "The container id of blockchain(s).")
	flag.StringVar(&helpers.ChainIDStr, "chainidstr", "", "The container id of blockchain(s).")
	flag.StringVar(&helpers.GenesisProxyAddress, "genesis-proxy-address", "0xfacade0000000000000000000000000000000000", "Use existing proxy address from genesis (e.g., 0xfacade0000000000000000000000000000000000)")
	flag.StringVar(&helpers.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.StringVar(&helpers.ValidatorIPs, "validators", "", "Comma-separated validators.  Format is host:port, don't include the protocol (i.e. http and https)")
	flag.Float64Var(&helpers.BalanceAVAX, "validator-balance", 1.0, "Initial balance per validator in AVAX (supports decimals, e.g., 0.1)")

	/**
	* LOCAL Path to the Avalanche Interchain Messaging (ICM, formerly Teleporter) smart contracts.  ICM is used for interchain communication.
	* The repository is https://github.com/ava-labs/icm-services; however, you need to clone the repo locally.
	*
	* Note: the tools just want the parent directory to icm-contracts.
	* Within icm-services/icm-contracts/:
	*	contracts/teleporter/  -- Deploy TeleporterMessenger for cross-chain messaging.
	*	contracts/teleporter/registry/ -- Deploy TeleporterRegistry for cross-chain messagin.
	*	contracts/ictt/ -- Interchain Token Transfer
	*	contracts/validator-manager/ -- L1 validator management
	*	contracts/governance/ --  L1 governance
	 */
	flag.StringVar(&helpers.ContractsPath, "contracts-path", "", "Absolute local path to icm-contracts code base (or set ICM_CONTRACTS_PATH).")

	flag.StringVar(&helpers.OutputFile, "output-file", "convert-subnet-to-l1", "Convert subnet to L1 output file.  Don't include the suffix as -output.json will be added to the output file name.")
	flag.BoolVar(&helpers.JsonOutput, "json", true, "Output results as JSON (for scripting)")

	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

}

func run() error {

	ctx := context.Background()

	err := requisiteChecks()
	if err != nil {
		return fmt.Errorf("Prerequisites not met.  Error: %w", err)
	}

	// Get network configuration
	networkID, rpcEndpoint, err := helpers.GetCustomNetworkConfig(helpers.NetworkName, helpers.RpcUrL)

	/**
	Avalanche IDs are Base58-encoded strings, not raw byte strings. Always use ids.FromString() rather than copy() when converting them.
	*/
	parsedSubnetID, err := ids.FromString(helpers.SubnetIDStr)
	if err != nil {
		return fmt.Errorf("Unable to covert sub id (str) %s to ids.ID object.  Error: %w", helpers.SubnetIDStr, err)
	}
	fmt.Printf("Subnet ID: %s\n", parsedSubnetID)
	parsedChainID, err := helpers.FromStringToChainID(helpers.ChainIDStr)
	if err != nil {
		return fmt.Errorf("invalid chain ID: %w", err)
	}

	// Initialize keychain and wallet.  The wallet should be part of the "Contract Deployer Allowlist" of the chain's genesis.json.
	// The wallet's private key is used to deploy the validator manager smart contract to your chain.

	fmt.Println("[1/4] Creating wallet.  The wallet's private key is used to deploy the validator manager smart contract to your chain.")
	var kc *secp256k1fx.Keychain
	var ownerAddress ids.ShortID

	key, err := helpers.LoadPrivateKey(helpers.KeyName)
	if err != nil {
		return fmt.Errorf("failed to load private key: %w", err)
	}

	ownerAddress = helpers.DeriveAvalancheAddressIdentifier(key)
	ethAddress := helpers.DeriveEthAddress(key)
	//No need to check genesis funding for a PoA base chain.

	kc = secp256k1fx.NewKeychain(key)

	// Use public API for wallet/transactions since validator nodes may not be connected yet
	walletURI := rpcEndpoint

	fmt.Printf("networkID=%d rpcEndpoint=%q\n", networkID, walletURI)

	/**
	Get the P-chain wallet used to pay for transactions.

	UTXO stands for Unspent Transaction Output. It is the accounting model used by cryptocurrencies like Bitcoin, Litecoin,
	and parts of the Avalanche network (specifically the X-Chain and P-Chain) to track digital currency ownership and balance.
	*/
	wallet, err := primary.MakePWallet(ctx, walletURI, kc, primary.WalletConfig{})
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}
	fmt.Printf("  We are operating against network id: %d\n", networkID)
	fmt.Printf("  Our Wallet (\"%s\") loaded successfully\n", helpers.KeyName)
	fmt.Printf("  Wallet's Avalanche Address Identifier (i.e. to be used as the \"owner\" of subnet): %s\n", ownerAddress)
	pChainAddress, _ := helpers.DerivePChainAddress(key)
	fmt.Printf("  Wallet's P-chain address: %s\n", pChainAddress)
	fmt.Printf("  EVM (C-chain) Address:     %s\n", ethAddress)

	// Parse validator IPs.  At least one validator is needed to deploy validator manager.
	ips, err := helpers.ParseValidatorIPs(helpers.ValidatorIPs)
	if err != nil {
		return fmt.Errorf("failed to parse validator IPs: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no validator IPs provided. Use --validators or set VALIDATOR_*_IP env vars")
	}

	fmt.Printf("Validators: %d\n", len(ips))
	for i, ip := range ips {
		fmt.Printf("  [%d] %s\n", i+1, ip)
	}
	fmt.Println()

	fmt.Println("[1/4] Build node URIs from validator endpoints.")
	nodeURIs := make([]string, len(ips)) //node array will be in format of http://host:port
	for i, endpoint := range ips {
		nodeURI, err := helpers.BuildNodeURI(endpoint)
		if err != nil {
			return fmt.Errorf("invalid validator endpoint %q: %w", endpoint, err)
		}
		nodeURIs[i] = nodeURI
	}

	fmt.Println("[4/4] Deploy validator manager contracts if requested")
	var managerProxyAddress []byte //represents validator manager proxy address from genesis config.  This will be used as part of converting subnet to L1.

	// If genesis proxy address is provided, use it as the proxy address for Validator Manager.
	// If using Builder Hub to generate genesis.json and you enabled “Transparent Upgradable Proxy” option,
	// you are pre deploying the validator proxy as part of genesis.
	// The default proxy address defined within the config file is "0xfacade0000000000000000000000000000000000".
	if helpers.GenesisProxyAddress != "" {
		fmt.Println("[4/N] Using genesis proxy address as ValidatorManager...")
		// Parse the address
		addrStr := strings.TrimPrefix(helpers.GenesisProxyAddress, "0x")
		if len(addrStr) != 40 {
			return fmt.Errorf("you passed an invalid genesis proxy address: %s (expected 40 hex chars)", helpers.GenesisProxyAddress)
		}
		addrBytes, err := hex.DecodeString(addrStr)
		if err != nil {
			return fmt.Errorf("failed to decode genesis proxy address: %s, err: %w", addrStr, err)
		}
		managerProxyAddress = addrBytes
		fmt.Printf("  Proxy Address: 0x%s\n", addrStr)
		fmt.Println("  NOTE: You must deploy the ValidatorManager implementation and upgrade the proxy AFTER L1 conversion")
	}

	// Gather validator info and convert to L1
	fmt.Println("  Gathering validator info...")
	validators := make([]*platform.ConvertSubnetToL1Validator, 0, len(nodeURIs))
	nodeIDs := make([]ids.NodeID, 0, len(nodeURIs))
	/**
	Proof-of-Possession (PoP) is a cryptographic signature that proves a node operator owns the private key corresponding to their public validating key.
	By forcing every validator to provide a Proof-of-Possession when joining the network, they prove they actually know the private key for their specific public key.
	The PoP is retrieved from each validator via its info.getNodeID() endpoint.
	*/
	nodePoPs := make([]*signer.ProofOfPossession, 0, len(nodeURIs))

	weights := make([]uint64, 0, len(nodeURIs))

	for i, uri := range nodeURIs {
		infoClient := info.NewClient(uri)
		nodeID, nodePoP, err := infoClient.GetNodeID(ctx) //Get node id which includes the PoP for validator
		if err != nil {
			return fmt.Errorf("failed to get node %d info from %s: %w.  Once fix, rerun.", i+1, uri, err)
		}
		fmt.Printf("    Node %d: %s\n", i+1, nodeID)

		weight := units.Schmeckle

		validators = append(validators, &platform.ConvertSubnetToL1Validator{
			NodeID:  nodeID.Bytes(),
			Weight:  weight,
			Balance: uint64(helpers.BalanceAVAX * float64(units.Avax)), //initial balance of each validator is 1 AVAX
			Signer:  *nodePoP,
		})

		nodeIDs = append(nodeIDs, nodeID)
		nodePoPs = append(nodePoPs, nodePoP)
		weights = append(weights, weight)
	}

	fmt.Println("  Issuing ConvertSubnetToL1Tx...")
	conversionTx, err := wallet.IssueConvertSubnetToL1Tx(
		parsedSubnetID,
		parsedChainID,
		managerProxyAddress, // Use deployed validator manager address or empty
		validators,
	)
	if err != nil {
		return fmt.Errorf("failed to convert subnet to L1, once fixed, rerun.  Error: %w", err)
	}
	conversionTxID := conversionTx.ID()
	fmt.Printf("  ****** Subnet converted to L1.  Conversion Tx *********: %s\n", conversionTxID)

	return nil
}

func requisiteChecks() error {
	if len(helpers.SubnetIDStr) == 0 {
		return fmt.Errorf("what is the subnet ID to convert to a L1")
	}

	if len(helpers.ChainIDStr) == 0 {
		return fmt.Errorf("chain ID required for conversion to L1")
	}

	if len(helpers.KeyName) == 0 {
		return fmt.Errorf("wallet name required to pay transaction fees")
	}

	if len(helpers.ValidatorIPs) == 0 {
		return fmt.Errorf("at least one validator is needed as a destination for the smart contracts (e.g. ValidatorManager contract)")
	}

	if len(helpers.RpcUrL) == 0 {
		return fmt.Errorf("no RPC endpoints specified")
	}

	return nil
}
