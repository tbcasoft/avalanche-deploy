package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/tbcasoft/avalanche-deploy/tools/core"
	"os"
)

func main() {

	flag.StringVar(&core.NetworkName, "network", "fuji", "Network: fuji or mainnet")
	flag.StringVar(&core.SubnetIDStr, "subnetidstr", "", "The container id of blockchain(s).")
	flag.StringVar(&core.ChainName, "chain-name", "my-l1", "Name for the L1 chain")
	flag.StringVar(&core.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.StringVar(&core.OutputFile, "output", "l1.env", "Output file for subnet/chain IDs")
	flag.StringVar(&core.GenesisFile, "genesis-file", "", "Genesis file path (default: configs/l1/genesis/genesis.json in current or parent dirs)")
	flag.Float64Var(&core.BalanceAVAX, "validator-balance", 1.0, "Initial balance per validator in AVAX (supports decimals, e.g., 0.1)")
	flag.StringVar(&core.RpcUrL, "rpcurl", "", "Rpc node http endpoint.")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var err error

	// Validate chain name (must be alphanumeric only)
	if err = core.ValidateChainName(core.ChainName); err != nil {
		return err
	}

	// Resolve genesis file path
	if core.GenesisFile == "" {
		core.GenesisFile, err = core.FindGenesisFile()
		if err != nil {
			return fmt.Errorf("failed to find genesis file: %w", err)
		}
	}
	// Load genesis
	genesisBytes, err := os.ReadFile(core.GenesisFile)
	if err != nil {
		return fmt.Errorf("failed to read genesis file %s: %w", core.GenesisFile, err)
	}
	evmChainID, err := core.ExtractEVMChainID(genesisBytes)
	if err != nil {
		return fmt.Errorf("Error:  not able to dervie EVM Chain id from genesis file (%s), error: %v\n", core.GenesisFile, err)
	}

	// Get network configuration
	networkID, rpcEndpoint, err := core.GetCustomNetworkConfig(core.NetworkName, core.RpcUrL)

	fmt.Println("=== Create Chain ===")
	fmt.Printf("Network:    %s (ID: %d)\n", core.NetworkName, networkID)
	fmt.Printf("Chain Name: %s\n", core.ChainName)
	fmt.Printf("Chain EVM unique ID: %s\n", evmChainID)

	return nil

	ctx := context.Background()

	// Initialize keychain and wallet
	var kc *secp256k1fx.Keychain
	var ownerAddress ids.ShortID

	fmt.Println("[1/4] Creating wallet...")
	key, err := core.LoadPrivateKey(core.KeyName)
	if err != nil {
		return fmt.Errorf("failed to load private key: %w", err)
	}
	ownerAddress = key.Address()

	// Derive Ethereum address and check genesis funding
	ethAddress := core.DeriveEthAddress(key)
	funded, balance := core.CheckGenesisFunding(genesisBytes, ethAddress)

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
	fmt.Printf("  P-Chain Address: %s\n", ownerAddress)
	fmt.Printf("  EVM Address:     %s\n", ethAddress)
	fmt.Printf("  Required P-Chain balance: ~%.2f AVAX per validator\n", core.BalanceAVAX)

	// Report genesis funding status
	if funded {
		fmt.Printf("  Genesis funding: ✓ (balance: %s)\n", balance)
	} else {
		fmt.Printf("  Genesis funding: ✗ (address not in genesis alloc)\n")
		fmt.Println("  WARNING: Your EVM address is not funded in the selected genesis file")
		fmt.Println("           This is acceptable for aProof of Authority, zero gas fee L1.")
		fmt.Println("           In a PoA network, security is derived from corporate identity and legal reputation, not from locked financial capital (staking).")
	}

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
	fmt.Printf("  Chain ID: %s\n", chainID)

	return nil
}
