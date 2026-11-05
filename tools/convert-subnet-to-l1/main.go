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

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/tbcasoft/avalanche-deploy/tools/core"
	l1 "github.com/tbcasoft/avalanche-deploy/tools/create-l1"
)

func main() {

	flag.StringVar(&core.SubnetIDStr, "subnetidstr", "", "The container id of blockchain(s).")
	flag.StringVar(&core.GenesisProxyAddress, "genesis-proxy-address", "", "Use existing proxy address from genesis (e.g., 0xfacade0000000000000000000000000000000000)")
	flag.StringVar(&core.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.BoolVar(&core.DeployValidatorManager, "deploy-validator-manager", false, "Deploy ValidatorManager contracts (requires forge)")
	flag.StringVar(&core.ValidatorIPs, "validators", "", "Comma-separated validators.  Format is host:port, don't include the protocol (i.e. http and https)")
	flag.StringVar(&core.ManagerType, "manager-type", "poa", "Validator manager type: poa, native-staking, erc20-staking")
	flag.StringVar(&core.OutputFile, "output", "l1.env", "Convert subnet to L1 output file.  Don't include the suffix as -output.json will be added to the output file name.")
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
	flag.StringVar(&core.ContractsPath, "contracts-path", "", "Path to icm-contracts repository (or set ICM_CONTRACTS_PATH)")

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

}

func run() error {

	var subnetID [32]byte
	copy(subnetID[:], core.SubnetIDStr)
	fmt.Printf("Subnet id: %s\n", subnetID)

	// Initialize keychain and wallet.  The wallet should be part of the "Contract Deployer Allowlist" of the chain's genesis.json.
	// The wallet's private key is used to deploy the validator manager smart contract to your chain.

	fmt.Println("[1/4] Creating wallet.  The wallet's private key is used to deploy the validator manager smart contract to your chain.")
	var kc *secp256k1fx.Keychain
	var walletPrivateKey ids.ShortID
	fmt.Println("[1/4] Creating wallet...")
	key, err := core.LoadPrivateKey(core.KeyName)
	if err != nil {
		return fmt.Errorf("failed to load private key: %w", err)
	}

	walletPrivateKey = key.Address()
	ethAddress := core.DeriveEthAddress(key)
	//No need to check genesis funding for a PoA base chain.

	// Get the private key hex for forge when deploying validator manager smart contract.
	keyBytes := key.Bytes()
	privateKeyHex := "0x" + fmt.Sprintf("%x", keyBytes)

	fmt.Printf("  Wallet loaded successfully\n")
	fmt.Printf("  ******  TODO: is this the wallet's private key?   P-Chain Address: %s\n", walletPrivateKey)
	fmt.Printf("  EVM Address:     %s\n", ethAddress)

	// Parse validator IPs.  At least one validator is needed to deploy validator manager.
	ips, err := core.ParseValidatorIPs(core.ValidatorIPs)
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
		nodeURI, err := core.BuildNodeURI(endpoint)
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
	if core.GenesisProxyAddress != "" {
		fmt.Println("[4/N] Using genesis proxy address as ValidatorManager...")
		// Parse the address
		addrStr := strings.TrimPrefix(core.GenesisProxyAddress, "0x")
		if len(addrStr) != 40 {
			return fmt.Errorf("you passed an invalid genesis proxy address: %s (expected 40 hex chars)", core.GenesisProxyAddress)
		}
		addrBytes, err := hex.DecodeString(addrStr)
		if err != nil {
			return fmt.Errorf("failed to decode genesis proxy address: %s, err: %w", addrStr, err)
		}
		managerAddress = addrBytes
		fmt.Printf("  Proxy Address: 0x%s\n", addrStr)
		fmt.Println("  NOTE: You must deploy the ValidatorManager implementation and upgrade the proxy AFTER L1 conversion")
	}
	if core.DeployValidatorManager {
		fmt.Println("[4/6] Deploying ValidatorManager contracts...")

		// Wait for chain RPC to be ready
		fmt.Println("  Waiting for chain RPC to be ready...")
		time.Sleep(15 * time.Second)

		// Build the chain RPC URL
		chainRPCURL := fmt.Sprintf("%s/ext/bc/%s/rpc", strings.TrimRight(nodeURIs[0], "/"), chainID)
		fmt.Printf("  The chain RPC used when deploying the smart contracts: %s\n", chainRPCURL)

		// Parse manager type, defaults to poa
		var vmType l1.ValidatorManagerType
		switch strings.ToLower(core.ManagerType) {
		case "native-staking", "native":
			vmType = l1.NativeTokenStakingManagerType
		case "erc20-staking", "erc20":
			vmType = l1.ERC20TokenStakingManagerType
		default:
			vmType = l1.PoAValidatorManagerType
			fmt.Printf("  Using validator manager type of %s\n", vmType)
		}

		var vmDeployment *l1.ValidatorManagerDeployment
		vmDeployment, err = l1.DeployValidatorManagerWithForge(
			ctx,
			chainRPCURL,
			privateKeyHex,
			subnetID,
			walletPrivateKey.String(),
			vmType,
			core.ContractsPath,
		)
		if err != nil {
			return fmt.Errorf("failed to deploy validator manager: %w", err)
		}

		managerAddress, err = vmDeployment.GetManagerAddressForConversion()
		if err != nil {
			return fmt.Errorf("failed to get manager address: %w", err)
		}

		// Save deployment info
		deploymentFile := strings.TrimSuffix(core.OutputFile, filepath.Ext(core.OutputFile)) + "-output.json"
		if err := vmDeployment.SaveDeployment(deploymentFile); err != nil {
			fmt.Printf("  Warning: failed to save deployment info: %v\n", err)
		} else {
			fmt.Printf("  Contract addresses saved to: %s\n", deploymentFile)
		}

		fmt.Println("[5/6] Gathering validator info...")
	} else {
		fmt.Println("[4/5] Gathering validator info...")
	}

	return nil
}
