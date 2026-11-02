package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/tbcasoft/avalanche-deploy/tools/core"
	"os"
	"strings"
	"time"
)

func main() {

	flag.StringVar(&core.GenesisProxyAddress, "genesis-proxy-address", "", "Use existing proxy address from genesis (e.g., 0xfacade0000000000000000000000000000000000)")
	flag.BoolVar(&core.DeployValidatorManager, "deploy-validator-manager", false, "Deploy ValidatorManager contracts (requires forge)")

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

}

func run() error {
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
		fmt.Printf("  Chain RPC: %s\n", chainRPCURL)

		// Parse manager type

	}
	
	return nil
}
