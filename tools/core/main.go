package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tbcasoft/avalanche-deploy/tools/core/helpers"
)

func main() {
	flag.StringVar(&helpers.NetworkName, "network", "fuji", "Network: fuji or mainnet")
	flag.StringVar(&helpers.KeyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")

	flag.Parse()

	key, err := helpers.LoadPrivateKey(helpers.KeyName)
	if err != nil {
		fmt.Errorf("failed to load private key: %w", err)
		os.Exit(1)
	}

	//Use case:  get the wallet's Avalanche Address ID that will own the subnet.
	fmt.Printf("  Wallet's address that will be used as the \"owner\" of a subnet:  %v\n", helpers.DeriveAvalancheAddressIdentifier(key))

	//Use case:  get the wallet's P-chain address
	pChainAddr, _ := helpers.DerivePChainAddress(key)
	fmt.Printf("  Wallet's address that will be used as the \"owner\" of a subnet:  %s\n", pChainAddr)

	//Use case:  get the waller'a c-chain address.
	fmt.Printf("  Wallet's c-chain address: %s\n", helpers.DeriveEthAddress(key))
}
