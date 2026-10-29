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
	"time"
)

func main() {

	flag.StringVar(&core.NetworkName, "network", "fuji", "Network: fuji or mainnet")
	flag.StringVar(&core.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.StringVar(&core.OutputFile, "output", "l1.env", "Output file for subnet/chain IDs")
	flag.StringVar(&core.RpcUrL, "rpcurl", "", "Rpc node http endpoint.")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {

	ctx := context.Background()

	// Get network configuration
	networkID, rpcEndpoint, err := core.GetCustomNetworkConfig(core.NetworkName, core.RpcUrL)

	fmt.Println("=== Create Subnet ===")
	fmt.Printf("Network:    %s (ID: %d)\n", core.NetworkName, networkID)

	fmt.Println("[1/4] Creating wallet...")
	key, err := core.LoadPrivateKey(core.KeyName) //load key from default keystore
	if err != nil {
		return fmt.Errorf("failed to load private key: %w", err)
	}
	ownerAddress := key.Address()

	// Derive Ethereum address and check genesis funding
	ethAddress := core.DeriveEthAddress(key)
	//No need to check genesis funding for a PoA base chain.

	kc := secp256k1fx.NewKeychain(key)

	// Use public API for wallet/transactions since validator nodes may not be connected yet
	walletURI := rpcEndpoint

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

	// Create subnet
	fmt.Println("[2/4] Creating subnet...")
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ownerAddress},
	}

	fmt.Println("  Building and issuing CreateSubnetTx...")
	subnetTx, err := wallet.IssueCreateSubnetTx(owner)
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w", err)
	}

	subnetID := subnetTx.ID()
	fmt.Printf("  Subnet ID: %s\n", subnetID)

	/**
	Re-sync wallet with subnet (use public API).

	Immediately after IssueCreateSubnetTx,
	the code calls MakePWallet.  This P-state sync can fail as we are querying P-Chain state immediately, not allowing P-chain state to materialize.

	For best practice, we are going to perform retries with exponential backoff.
	*/
	for attempt := 1; attempt <= 10; attempt++ {
		wallet, err = primary.MakePWallet(ctx, walletURI, kc, primary.WalletConfig{
			SubnetIDs: []ids.ID{subnetID},
		})
		if err == nil {
			break
		}
		if attempt == 10 {
			return fmt.Errorf("failed to re-sync wallet after creating subnet %s: %w", subnetID, err)
		}

		fmt.Printf("  Wallet re-sync attempt %d failed; retrying...\n", attempt)
		time.Sleep(2 * time.Second)
	}

	fmt.Printf("==== Subnet created, and we were ")
	return nil
}
