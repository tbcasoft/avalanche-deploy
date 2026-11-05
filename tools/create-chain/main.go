package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/tbcasoft/avalanche-deploy/tools/core"
)

type CreatedChain struct {
	TransactionId    string `json:"transaction-id"`
	ChainName        string `json:"chain-name"`
	ChainID          string `json:"chain-id"`
	GenesisFile      string `json:"genesis-file"`
	EvmChainID       string `json:"evm-chain-id"`
	NetworkName      string `json:"network-name"`
	RpcEndpoint      string `json:"rpc-endpoint"`
	WalletName       string `json:"wallet-name"`
	WalletEthAddress string `json:"wallet-eth-address"`
}

func main() {

	flag.StringVar(&core.NetworkName, "network", "fuji", "Network: fuji or mainnet")
	flag.StringVar(&core.SubnetIDStr, "subnetidstr", "", "The container id of blockchain(s).")
	flag.StringVar(&core.ChainName, "chain-name", "my-l1", "Name for the L1 chain")
	flag.StringVar(&core.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.StringVar(&core.GenesisFile, "genesis-file", "", "Genesis file path (default: configs/l1/genesis/genesis.json in current or parent dirs)")
	flag.Float64Var(&core.BalanceAVAX, "validator-balance", 1.0, "Initial balance per validator in AVAX (supports decimals, e.g., 0.1)")
	flag.StringVar(&core.RpcUrL, "rpcurl", "", "Rpc node http endpoint (e.g. http://18.219.181.165:9650)")
	flag.StringVar(&core.OutputFile, "output-file", "create-chain", "Output from create-chain execution.  Don't include the suffix as -output.json will be added to the output file name.")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var err error

	createdChain := &CreatedChain{}

	// Validate chain name (must be alphanumeric only)
	if err = core.ValidateChainName(core.ChainName); err != nil {
		return err
	}
	createdChain.ChainName = core.ChainName

	// Resolve genesis file path
	if core.GenesisFile == "" {
		core.GenesisFile, err = core.FindGenesisFile()
		if err != nil {
			return fmt.Errorf("failed to find genesis file: %w", err)
		}
	}
	createdChain.GenesisFile = core.GenesisFile

	// Load genesis
	genesisBytes, err := os.ReadFile(core.GenesisFile)
	if err != nil {
		return fmt.Errorf("failed to read genesis file %s: %w", core.GenesisFile, err)
	}
	evmChainID, err := core.ExtractEVMChainID(genesisBytes)
	if err != nil {
		return fmt.Errorf("Error:  not able to dervie EVM Chain id from genesis file (%s), error: %v\n", core.GenesisFile, err)
	}
	createdChain.EvmChainID = evmChainID

	// Get network configuration
	networkID, rpcEndpoint, err := core.GetCustomNetworkConfig(core.NetworkName, core.RpcUrL)
	createdChain.NetworkName = core.NetworkName
	createdChain.RpcEndpoint = rpcEndpoint

	fmt.Println("=== Create Chain Task ===")
	fmt.Printf("Network:    %s (ID: %d)\n", core.NetworkName, networkID)
	fmt.Printf("Chain Name: %s\n", core.ChainName)
	fmt.Printf("Chain EVM unique ID: %s\n", evmChainID)
	fmt.Printf("RPC endpoint: %s\n", rpcEndpoint)

	ctx := context.Background()

	// Initialize keychain and wallet
	var kc *secp256k1fx.Keychain
	var wallketPrivateKey ids.ShortID

	fmt.Println("[1/4] Creating wallet...")
	key, err := core.LoadPrivateKey(core.KeyName)
	if err != nil {
		return fmt.Errorf("failed to load private key: %w", err)
	}
	wallketPrivateKey = key.Address()
	createdChain.WalletName = core.KeyName

	// Derive Ethereum address and check genesis funding
	ethAddress := core.DeriveEthAddress(key)
	createdChain.WalletEthAddress = ethAddress
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
	fmt.Printf("  Our P-chain Wallet (\"%s\") loaded successfully\n", core.KeyName)
	fmt.Printf("  ******  TODO: is this the wallet's private key?   P-Chain Address: %s\n", wallketPrivateKey)
	fmt.Printf("  EVM Address:     %s\n", ethAddress)
	fmt.Printf("  Required P-Chain balance: ~%.2f AVAX per validator\n", core.BalanceAVAX)

	// This is normally the check funding step (i.e "Report genesis funding status").  However, funding is not required for a Proof of Authority, zero gas fee L1.
	// In a PoA network, security is derived from corporate identity and legal reputation, not from locked financial capital (staking).

	var subnetID [32]byte
	copy(subnetID[:], core.SubnetIDStr)
	fmt.Printf("Subnet id: %s\n", subnetID)

	// Create chain
	fmt.Println("[3/4] Creating chain...")
	chainTx, err := wallet.IssueCreateChainTx(
		subnetID,
		genesisBytes,
		constants.SubnetEVMID,
		nil,
		core.ChainName,
	)
	if err != nil {
		return fmt.Errorf("failed to create chain: %w", err)
	}
	chainID := chainTx.ID()
	createdChain.ChainID = chainID.String()
	createdChain.TransactionId = chainTx.TxID.String()
	fmt.Printf("  Chain created.  Txn ID: %s,  chain ID: %s\n", chainTx.TxID, chainID)

	// Save deployment info
	deploymentFile := strings.TrimSuffix(core.OutputFile, filepath.Ext(core.OutputFile)) + "-output.json"
	if err := createdChain.SaveDeployment(deploymentFile); err != nil {
		fmt.Printf("  Warning: failed to save deployment info: %v\n", err)
	} else {
		fmt.Printf("  Contract addresses saved to: %s\n", deploymentFile)
	}
	fmt.Println("**** Now setup the validators! ***")

	return nil
}

func (d *CreatedChain) SaveDeployment(filepath string) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, data, 0644)
}
