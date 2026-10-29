package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/tbcasoft/avalanche-deploy/tools/core"
	"os"
)

func main() {

	flag.StringVar(&core.NetworkName, "network", "fuji", "Network: fuji or mainnet")
	flag.StringVar(&core.SubnetIDStr, "subnetidstr", "", "The container id of blockchain(s).")
	flag.StringVar(&core.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.StringVar(&core.OutputFile, "output", "l1.env", "Output file for subnet/chain IDs")
	flag.StringVar(&core.ValidatorIPs, "validators", "", "Comma-separated validator IPs")
	flag.StringVar(&core.GenesisFile, "genesis", "", "Genesis file path (default: configs/l1/genesis/genesis.json in current or parent dirs)")
	flag.Float64Var(&core.BalanceAVAX, "validator-balance", 1.0, "Initial balance per validator in AVAX (supports decimals, e.g., 0.1)")
	flag.StringVar(&core.RpcUrL, "rpcurl", "", "Rpc node http endpoint.")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Validate chain name (must be alphanumeric only)
	if err := core.ValidateChainName(core.ChainName); err != nil {
		return err
	}

	// Parse validator IPs
	ips, err := core.ParseValidatorIPs(core.ValidatorIPs)
	if err != nil {
		return fmt.Errorf("failed to parse validator IPs: %w", err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("no validator IPs provided. Use --validators or set VALIDATOR_*_IP env vars")
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
	evmChainID := core.ExtractEVMChainID(genesisBytes)
	//TODO: handle blank envm chain id use case

	// Get network configuration
	networkID, rpcEndpoint, err := core.GetCustomNetworkConfig(core.NetworkName, core.RpcUrL)

	fmt.Println("=== Create Avalanche L1 ===")
	fmt.Printf("Network:    %s (ID: %d)\n", core.NetworkName, networkID)
	fmt.Printf("Chain Name: %s\n", core.ChainName)
	fmt.Printf("Validators: %d\n", len(ips))
	for i, ip := range ips {
		fmt.Printf("  [%d] %s\n", i+1, ip)
	}
	fmt.Println()

	ctx := context.Background()

	// Build node URIs from validator endpoints.
	nodeURIs := make([]string, len(ips))
	for i, endpoint := range ips {
		nodeURI, err := core.BuildNodeURI(endpoint)
		if err != nil {
			return fmt.Errorf("invalid validator endpoint %q: %w", endpoint, err)
		}
		nodeURIs[i] = nodeURI
	}

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
	if walletURI == "" {
		walletURI = nodeURIs[0]
		fmt.Printf("  Using validator %s as the rpc endpoint.\n", walletURI)
	} else {
		fmt.Printf("  Using RPC endpoint: %s\n", walletURI)
	}

	/**
	Get the P-chain wallet used to pay for transactions.

	UTXO stands for Unspent Transaction Output. It is the accounting model used by cryptocurrencies like Bitcoin, Litecoin,
	and parts of the Avalanche network (specifically the X-Chain and P-Chain) to track digital currency ownership.
	*/
	wallet, err := primary.MakePWallet(ctx, walletURI, kc, primary.WalletConfig{})
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}
	fmt.Printf("  Our P-chain Wallet loaded successfully\n")
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

	// Create chain
	fmt.Println("[2/4] Creating chain...")

	return nil
}
