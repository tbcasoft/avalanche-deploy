package main

import (
	"core"
	"flag"
	"fmt"
	"os"
)

var (
	networkName  string
	keyName      string
	outputFile   string
	validatorIPs string
	genesisFile  string
	chainName    string
)

func main() {

	flag.StringVar(&networkName, "network", "fuji", "Network: fuji or mainnet")
	flag.StringVar(&keyName, "key-name", "", "Key name from platform-cli keystore (~/.platform/keys, preferred)")
	flag.StringVar(&outputFile, "output", "l1.env", "Output file for subnet/chain IDs")
	flag.StringVar(&validatorIPs, "validators", "", "Comma-separated validator IPs")
	flag.StringVar(&genesisFile, "genesis", "", "Genesis file path (default: configs/l1/genesis/genesis.json in current or parent dirs)")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Validate chain name (must be alphanumeric only)
	if err := core.ValidateChainName(chainName); err != nil {
		return err
	}

	return nil
}
