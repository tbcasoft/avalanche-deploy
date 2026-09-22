package core

import (
	"fmt"
	"github.com/ava-labs/platform-cli/pkg/network"
	"strings"
)

func GetNetworkConfig(networkName string) (uint32, string, error) {
	// Use platform-cli network package for configuration
	return network.GetNetworkIDAndRPC(networkName)
}

func ValidateChainName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("chain name cannot be empty")
	}

	for _, ch := range name {
		isLower := ch >= 'a' && ch <= 'z'
		isUpper := ch >= 'A' && ch <= 'Z'
		isDigit := ch >= '0' && ch <= '9'
		if !isLower && !isUpper && !isDigit {
			return fmt.Errorf("invalid chain name %q: use only letters and numbers", name)
		}
	}

	return nil
}
