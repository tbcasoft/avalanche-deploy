package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/tbcasoft/avalanche-deploy/tools/core/helpers"
)

func main() {

	flag.StringVar(&helpers.SubnetIDStr, "subnetidstr", "", "The container id of blockchain(s).")
	flag.StringVar(&helpers.GenesisProxyAddress, "genesis-proxy-address", "", "Use existing proxy address from genesis (e.g., 0xfacade0000000000000000000000000000000000)")
	flag.StringVar(&helpers.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.BoolVar(&helpers.DeployValidatorManager, "deploy-validator-manager", false, "Deploy ValidatorManager contracts (requires forge)")
	flag.StringVar(&helpers.ValidatorIPs, "validators", "", "Comma-separated validators.  Format is host:port, don't include the protocol (i.e. http and https)")
	flag.StringVar(&helpers.ManagerType, "manager-type", "poa", "Validator manager type: poa, native-staking, erc20-staking")
	flag.StringVar(&helpers.OutputFile, "output", "l1.env", "Convert subnet to L1 output file.  Don't include the suffix as -output.json will be added to the output file name.")
	/**
	* Path to the Avalanche Interchain Messaging (ICM, formerly Teleporter).  ICM is used for interchain communication.
	* The repository is https://github.com/ava-labs/icm-services.  Note: the tools just want the parent directory to icm-contracts.
	* Within icm-services/icm-contracts/:
	*	contracts/teleporter/  -- Deploy TeleporterMessenger for cross-chain messaging.
	*	contracts/teleporter/registry/ -- Deploy TeleporterRegistry for cross-chain messagin.
	*	contracts/ictt/ -- Interchain Token Transfer
	*	contracts/validator-manager/ -- L1 validator management
	*	contracts/governance/ --  L1 governance
	 */
	flag.StringVar(&helpers.ContractsPath, "contracts-path", "", "Path to icm-contracts repository (or set ICM_CONTRACTS_PATH)")

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

}

func run() error {

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

	/**
	Get the P-chain wallet used to pay for transactions.

	UTXO stands for Unspent Transaction Output. It is the accounting model used by cryptocurrencies like Bitcoin, Litecoin,
	and parts of the Avalanche network (specifically the X-Chain and P-Chain) to track digital currency ownership and balance.
	*/
	wallet, err := primary.MakePWallet(ctx, walletURI, kc, primary.WalletConfig{})
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}
	fmt.Printf("  Our Wallet (\"%s\") loaded successfully\n", helpers.KeyName)
	fmt.Printf("  Wallet's Avalanche Address Identifier (i.e. to be used as the \"owner\" of subnet): %s\n", ownerAddress)
	fmt.Printf("  Wallet's P-chain address: %s\n", helpers.DerivePChainAddress(key))
	fmt.Printf("  EVM (C-chain) Address:     %s\n", ethAddress)

	// Get the private key hex for forge when deploying validator manager smart contract.
	keyBytes := key.Bytes()
	privateKeyHex := "0x" + fmt.Sprintf("%x", keyBytes)

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

	ctx := context.Background()
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
	var managerAddress []byte //represents validator manager proxy address from genesis config.  This will be used as part of converting subnet to L1.

	// If genesis proxy address is provided, use it as the manager address.
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
		managerAddress = addrBytes
		fmt.Printf("  Proxy Address: 0x%s\n", addrStr)
		fmt.Println("  NOTE: You must deploy the ValidatorManager implementation and upgrade the proxy AFTER L1 conversion")
	}
	if helpers.DeployValidatorManager {
		fmt.Println("[4/6] Deploying ValidatorManager contracts...")

		// Wait for chain RPC to be ready
		fmt.Println("  Waiting for chain RPC to be ready...")
		time.Sleep(15 * time.Second)

		// Build the chain RPC URL
		chainRPCURL := fmt.Sprintf("%s/ext/bc/%s/rpc", strings.TrimRight(nodeURIs[0], "/"), parsedChainID)
		fmt.Printf("  The chain RPC used when deploying the smart contracts: %s\n", chainRPCURL)

		// Parse manager type, defaults to poa
		var vmType helpers.ValidatorManagerType
		switch strings.ToLower(helpers.ManagerType) {
		case "native-staking", "native":
			vmType = helpers.NativeTokenStakingManagerType
		case "erc20-staking", "erc20":
			vmType = helpers.ERC20TokenStakingManagerType
		default:
			vmType = helpers.PoAValidatorManagerType
			fmt.Printf("  Using validator manager type of %s\n", vmType)
		}

		var vmDeployment *helpers.ValidatorManagerDeployment
		vmDeployment, err = helpers.DeployValidatorManagerWithForge(
			ctx,
			chainRPCURL,
			privateKeyHex,
			parsedSubnetID,
			ownerAddress.String(),
			vmType,
			helpers.ContractsPath,
		)
		if err != nil {
			return fmt.Errorf("failed to deploy validator manager: %w", err)
		}

		managerAddress, err = vmDeployment.GetManagerAddressForConversion()
		if err != nil {
			return fmt.Errorf("failed to get manager address: %w", err)
		}

		// Save deployment info
		deploymentFile := strings.TrimSuffix(helpers.OutputFile, filepath.Ext(helpers.OutputFile)) + "-output.json"
		if err := vmDeployment.SaveDeployment(deploymentFile); err != nil {
			fmt.Printf("  Warning: failed to save deployment info: %v\n", err)
		} else {
			fmt.Printf("  Contract addresses saved to: %s\n", deploymentFile)
		}

		fmt.Println("[5/6] Gathering validator info...")
	} else {
		fmt.Println("[4/5] Gathering validator info...")
	}

	// Gather validator info and convert to L1
	fmt.Println("  Gathering validator info...")
	validators := make([]*platform.ConvertSubnetToL1Validator, 0, len(nodeURIs))
	nodeIDs := make([]ids.NodeID, 0, len(nodeURIs))
	/**
	Proof-of-Possession (PoP) is a cryptographic signature that proves a node operator owns the private key corresponding to their public validating key.
	By forcing every validator to provide a Proof-of-Possession when joining the network, they prove they actually know the private key for their specific public key.
	*/
	nodePoPs := make([]*signer.ProofOfPossession, 0, len(nodeURIs))

	weights := make([]uint64, 0, len(nodeURIs))

	for i, uri := range nodeURIs {
		infoClient := info.NewClient(uri)
		nodeID, nodePoP, err := infoClient.GetNodeID(ctx)
		if err != nil {
			return fmt.Errorf("failed to get node %d info from %s: %w", i+1, uri, err)
		}
		fmt.Printf("    Node %d: %s\n", i+1, nodeID)

		weight := units.Schmeckle

		validators = append(validators, &platform.ConvertSubnetToL1Validator{
			NodeID:  nodeID.Bytes(),
			Weight:  weight, should this be 100 since we only have one validator?
			Balance: uint64(balanceAVAX * float64(units.Avax)),
			Signer:  *nodePoP,
		})

		nodeIDs = append(nodeIDs, nodeID)
		nodePoPs = append(nodePoPs, nodePoP)
		weights = append(weights, weight)
	}

	if helpers.DeployValidatorManager {
		fmt.Println("[6/7] Converting subnet to L1...")
	} else {
		fmt.Println("[5/5] Converting subnet to L1...")
	}

	fmt.Println("  Issuing ConvertSubnetToL1Tx...")
	conversionTx, err := wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		managerAddress, // Use deployed validator manager address or empty
		validators,
	)
	if err != nil {
		return fmt.Errorf("failed to convert subnet to L1: %w", err)
	}
	conversionTxID := conversionTx.ID()
	fmt.Printf("  Conversion Tx: %s\n", conversionTxID)

	// Wait for chain
	fmt.Println("  Waiting for chain to be ready...")
	time.Sleep(10 * time.Second)

	// Initialize validator set if validator manager was deployed
	if helpers.DeployValidatorManager && vmDeployment != nil {
		fmt.Println("[7/7] Initializing validator set...")

		// Build the chain RPC URL
		chainRPCURL := fmt.Sprintf("%s/ext/bc/%s/rpc", strings.TrimRight(nodeURIs[0], "/"), chainID)

		// Get the private key hex for cast
		keyBytes := key.Bytes()
		privateKeyHex := "0x" + fmt.Sprintf("%x", keyBytes)

		// Build ConversionData
		conversionData := BuildConversionData(
			subnetID,
			chainID,
			vmDeployment.ValidatorManagerProxy,
			nodeIDs,
			nodePoPs,
			weights,
		)

		var signedMessage []byte
		var useGlacierAPI = !useLocalSigAgg && !startSigAgg //if we are not running our own signature aggregator, use Glacier
		var sigAgg *SignatureAggregator

		if !useGlacierAPI && startSigAgg {
			todo
		} else if !useGlacierAPI && useLocalSigAgg {
			// Using existing local signature-aggregator URL
			fmt.Println("  Using local signature-aggregator for private L1...")
		}

		if !useGlacierAPI {
			toto
		}

		if useGlacierAPI {
			todo
		}

		// Call initializeValidatorSet on the contract
		err = InitializeValidatorSet(
			ctx,
			chainRPCURL,
			privateKeyHex,
			vmDeployment.ValidatorManagerProxy,
			conversionData,
			signedMessage,
			contractsPath,
		)
	}

	return nil
}
