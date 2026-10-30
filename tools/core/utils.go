package core

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	pkgkeystore "github.com/ava-labs/platform-cli/pkg/keystore"
	"github.com/ava-labs/platform-cli/pkg/network"
	pkgwallet "github.com/ava-labs/platform-cli/pkg/wallet"
	dcrsecp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/sha3"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	NetworkName  string
	SubnetIDStr  string
	SubnetID     ids.ID
	KeyName      string
	OutputFile   string
	ValidatorIPs string
	GenesisFile  string
	ChainName    string
	BalanceAVAX  float64
	RpcUrL       string
)

// GenesisConfig represents a simplified genesis file structure
type GenesisConfig struct {
	Config     json.RawMessage         `json:"config"`
	Alloc      map[string]GenesisAlloc `json:"alloc"`
	Nonce      string                  `json:"nonce"`
	Timestamp  string                  `json:"timestamp"`
	ExtraData  string                  `json:"extraData"`
	GasLimit   string                  `json:"gasLimit"`
	Difficulty string                  `json:"difficulty"`
	MixHash    string                  `json:"mixHash"`
	Coinbase   string                  `json:"coinbase"`
	Number     string                  `json:"number"`
	GasUsed    string                  `json:"gasUsed"`
	ParentHash string                  `json:"parentHash"`
}

// GenesisAlloc represents the alloc section of a genesis file
type GenesisAlloc struct {
	Balance string `json:"balance"`
}

// GetNetworkConfig returns hardcoded rpc endpoint for network ("mainnet" or "fuji" network).
func GetNetworkConfig(networkName string) (uint32, string, error) {
	// Use platform-cli network package for configuration
	return network.GetNetworkIDAndRPC(networkName)
}

// GetCustomNetworkConfig returns a rpc endpoint for network ("mainnet" or "fuji" network).
// This function mimics func GetNetworkConfig with one difference, this function uses our custom rpc endpoint instead of the public rpc endpoint
// of "https://api.avax-test.network" nor "https://api.avax.network"
//
//	"networkName" must be either "mainnet" or "fuji".
//	"rpcUrl" must be in formt of "https://domain", e.g. "https://api.avax.network" or "https://api.avax-test.network"
func GetCustomNetworkConfig(networkName string, rpcUrl string) (uint32, string, error) {
	var res network.Config

	switch networkName {
	case "mainnet":
		res = network.Config{
			Name:              "mainnet",
			NetworkID:         1,
			RPCURL:            rpcUrl,
			MinValidatorStake: 2000_000_000_000,    // 2000 AVAX
			MinDelegatorStake: 25_000_000_000,      // 25 AVAX
			MinStakeDuration:  14 * 24 * time.Hour, // 14 days
		}

	case "fuji":
		res = network.Config{
			Name:              "fuji",
			NetworkID:         5,
			RPCURL:            rpcUrl,
			MinValidatorStake: 1_000_000_000,  // 1 AVAX
			MinDelegatorStake: 1_000_000_000,  // 1 AVAX
			MinStakeDuration:  24 * time.Hour, // 24 hours
		}
	default:
		return 0, "", fmt.Errorf("unsupported network %q (supported: fuji, mainnet)", networkName)
	}

	return res.NetworkID, res.RPCURL, nil
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

func ParseValidatorIPs(validatorIPs string) ([]string, error) {
	var ips []string

	// From flag
	if validatorIPs != "" {
		for _, ip := range strings.Split(validatorIPs, ",") {
			ip = strings.TrimSpace(ip)
			if ip != "" {
				ips = append(ips, ip)
			}
		}

		return ips, nil
	}

	// From environment variables (VALIDATOR_1_IP, VALIDATOR_2_IP, etc.)
	for i := 1; ; i++ {
		ip := os.Getenv(fmt.Sprintf("VALIDATOR_%d_IP", i))
		if ip == "" {
			break
		}
		ips = append(ips, ip)
	}

	return ips, nil
}

func FindGenesisFile() (string, error) {
	candidates := []string{
		filepath.Join("configs", "l1", "genesis", "genesis.json"), // configs/l1/genesis/genesis.json
		"genesis.json", // Backward-compatibility fallback
	}

	// Check current directory first
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs, nil
		}
	}

	// Walk up parent directories (up to 7 levels)
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for i := 0; i < 7; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
		for _, candidate := range candidates {
			genesisPath := filepath.Join(dir, candidate)
			if _, err := os.Stat(genesisPath); err == nil {
				return genesisPath, nil
			}
		}
	}

	return "", fmt.Errorf("genesis file not found in current or parent directories (looked for configs/l1/genesis/genesis.json and genesis.json). Use --genesis flag to specify path")
}

/*
*
ExtractEVMChainID returns the EVM chain ID from genesis config as a string.
*/
func ExtractEVMChainID(genesisBytes []byte) (string, error) {
	var parsed struct {
		Config map[string]json.RawMessage `json:"config"`
	}

	if err := json.Unmarshal(genesisBytes, &parsed); err != nil || parsed.Config == nil {
		return "", fmt.Errorf("Error:  unable to find \"config\" section within genesis file.")
	}

	rawChainID, ok := parsed.Config["chainId"]
	if !ok || len(rawChainID) == 0 {
		return "", fmt.Errorf("Error:  vallue for atribute \"chainId\" cannot be blank, i.e. the EVM unique id is wrong.")
	}

	// Try integer first (common case), then string fallback.
	var numeric json.Number
	if err := json.Unmarshal(rawChainID, &numeric); err == nil {
		fmt.Printf("  The EVM unique id for your chain is %s\n", numeric.String())
		return numeric.String(), nil
	}

	var str string
	if err := json.Unmarshal(rawChainID, &str); err == nil {
		fmt.Printf("  The EVM unique id for your chain is %s\n", strings.TrimSpace(str))
		return strings.TrimSpace(str), nil
	}

	return "", fmt.Errorf("Error:  unable to derive the EVM unique id for you chain fromm the provided genesis file.")
}

func BuildNodeURI(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("endpoint cannot be empty")
	}

	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return strings.TrimRight(endpoint, "/"), nil
	}

	if host, port, err := net.SplitHostPort(endpoint); err == nil {
		if host == "" || port == "" {
			return "", fmt.Errorf("invalid host:port endpoint")
		}
		return "http://" + endpoint, nil
	}

	if strings.Count(endpoint, ":") > 1 {
		// IPv6 without an explicit port.
		return "http://[" + endpoint + "]:9650", nil
	}
	return "http://" + endpoint + ":9650", nil
}

// LoadPrivateKey retrieve the private key of the wallet to be use for admin type of transactions.
// Key is retrieved in the following manner (listed by priority order):
//
//	--key-name, if blank, loads from Env variable AVALANCHE_PRIVATE_KEY, finally, use Default key store
func LoadPrivateKey(keyName string) (*secp256k1.PrivateKey, error) {
	// Priority: --key-name flag > AVALANCHE_PRIVATE_KEY env > keystore default key.
	if keyName != "" {
		return LoadPrivateKeyFromKeystore(keyName)
	}

	// Check for balance override from env.
	if envBalance := os.Getenv("L1_VALIDATOR_BALANCE_AVAX"); envBalance != "" && BalanceAVAX == 1.0 {
		if val, err := ParseFloat64(envBalance); err == nil {
			BalanceAVAX = val
		}
	}

	// Environment variable takes precedence over keystore default
	// so that CI/CD and scripted deployments work predictably.
	envKey := os.Getenv("AVALANCHE_PRIVATE_KEY")
	if envKey != "" {
		keyBytes, err := pkgwallet.ParsePrivateKey(envKey)
		if err != nil {
			return nil, err
		}
		return pkgwallet.ToPrivateKey(keyBytes)
	}

	// Fall back to keystore default key.
	if ks, err := pkgkeystore.Load(); err == nil {
		if defaultKey := ks.GetDefault(); defaultKey != "" {
			return LoadPrivateKeyFromKeystore(defaultKey)
		}
	}

	return nil, fmt.Errorf("no key provided. Use --key-name, AVALANCHE_PRIVATE_KEY env var, or set a platform-cli default key")
}

// LoadPrivateKeyFromKeystore retrieve the private key of the wallet base on the key name.
func LoadPrivateKeyFromKeystore(name string) (*secp256k1.PrivateKey, error) {
	if err := pkgkeystore.ValidateKeyName(name); err != nil {
		return nil, err
	}

	ks, err := pkgkeystore.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load keystore: %w", err)
	}

	if !ks.HasKey(name) {
		return nil, fmt.Errorf("key %q not found in keystore", name)
	}

	var password []byte
	if ks.IsEncrypted(name) { //if key is encrypted, use the password to decrypt
		envPwd := os.Getenv("PLATFORM_CLI_KEY_PASSWORD")
		if envPwd == "" {
			return nil, fmt.Errorf("key %q is encrypted; set PLATFORM_CLI_KEY_PASSWORD", name)
		}
		password = []byte(envPwd)
		defer ClearBytes(password) //for security reason, clear the password once we are end of this function.
	}

	keyBytes, err := ks.LoadKey(name, password)
	if err != nil {
		return nil, fmt.Errorf("failed to load key %q from keystore: %w", name, err)
	}
	defer ClearBytes(keyBytes)

	return pkgwallet.ToPrivateKey(keyBytes)
}

// ParseFloat64 return the integer  representation of the string val
func ParseFloat64(s string) (float64, error) {
	var val float64
	_, err := fmt.Sscanf(s, "%d", &val)
	return val, err
}

func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// DeriveEthAddress gets the EVM address of the wallet's public key.
//
// EVM address is also known as the P-chain address.
func DeriveEthAddress(key *secp256k1.PrivateKey) string {
	pubKey, err := dcrsecp.ParsePubKey(key.PublicKey().Bytes())
	if err != nil {
		return "0x0000000000000000000000000000000000000000"
	}

	uncompressed := pubKey.SerializeUncompressed()
	if len(uncompressed) != 65 {
		return "0x0000000000000000000000000000000000000000"
	}

	// Ethereum address is last 20 bytes of keccak256(uncompressed pubkey without 0x04 prefix).
	hash := sha3.NewLegacyKeccak256()
	_, _ = hash.Write(uncompressed[1:])
	sum := hash.Sum(nil)
	return "0x" + hex.EncodeToString(sum[12:])
}

// CheckGenesisFunding verifies if the given address is funded in the genesis config file.
// Specifically, the given address must be part of the "Alloc" section of the config file.
func CheckGenesisFunding(genesisBytes []byte, address string) (bool, string) {
	var genesis GenesisConfig

	if err := json.Unmarshal(genesisBytes, &genesis); err != nil {
		return false, ""
	}

	// Normalize address (remove 0x prefix, lowercase)
	normalizedAddr := strings.ToLower(strings.TrimPrefix(address, "0x"))

	for addr, alloc := range genesis.Alloc {
		normalizedAlloc := strings.ToLower(strings.TrimPrefix(addr, "0x"))
		if normalizedAlloc == normalizedAddr {
			return true, alloc.Balance
		}
	}

	return false, ""
}
