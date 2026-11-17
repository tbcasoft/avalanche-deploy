package helpers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

/*
Based on create-l1/initialize_validator_set.go
*/

// ValidatorManagerDeployment contains addresses of deployed validator manager contracts
type ValidatorManagerDeployment struct {
	ValidatorManagerImpl  string `json:"validatorManagerImpl"`
	ValidatorManagerProxy string `json:"validatorManagerProxy"`
	ProxyAdmin            string `json:"proxyAdmin"`
	PoAManager            string `json:"poaManager,omitempty"`
	StakingManager        string `json:"stakingManager,omitempty"`
}

// InitialValidator represents a validator for the conversion data
type InitialValidator struct {
	NodeID       []byte
	BlsPublicKey []byte
	Weight       uint64
}

// ConversionData represents the data needed for initializeValidatorSet and subnet conversion to L1
type ConversionData struct {
	SubnetID                     ids.ID
	ValidatorManagerBlockchainID ids.ID
	ValidatorManagerAddress      string
	InitialValidators            []InitialValidator
}

// BuildConversionData constructs the validators metadata needed as input for smart contracts execution.
func BuildConversionData(
	subnetID ids.ID,
	chainID ids.ID,
	validatorManagerAddress string,
	nodeIDs []ids.NodeID,
	nodePoPs []*signer.ProofOfPossession,
	weights []uint64,
) ConversionData {

	validators := make([]InitialValidator, len(nodeIDs))
	for i := range nodeIDs {
		validators[i] = InitialValidator{
			NodeID:       nodeIDs[i].Bytes(),
			BlsPublicKey: nodePoPs[i].PublicKey[:],
			Weight:       weights[i],
		}
	}
	return ConversionData{
		SubnetID:                     subnetID,
		ValidatorManagerBlockchainID: chainID,
		ValidatorManagerAddress:      validatorManagerAddress,
		InitialValidators:            validators,
	}
}

// InitializeValidatorSetWithGlacier is a convenience function that:
// 1. Waits for the aggregated signature from Glacier
// 2. Calls initializeValidatorSet on the contract
func InitializeValidatorSetWithGlacier(
	ctx context.Context,
	network string,
	conversionTxHash string,
	chainRPCURL string,
	privateKey string,
	validatorManagerProxy string,
	conversionData ConversionData,
	contractsPath string,
	glacierAPIKey string,
) error {
	fmt.Println("  Fetching aggregated signature from Glacier API...")

	// Wait for signature aggregation (may take a few blocks)
	signedMessage, err := WaitForAggregatedSignature(ctx, network, conversionTxHash, glacierAPIKey, 30)
	if err != nil {
		return fmt.Errorf("failed to get aggregated signature: %w", err)
	}
	fmt.Printf("    Signature received (%d bytes)\n", len(signedMessage))

	fmt.Println("  Calling initializeValidatorSet...")
	return InitializeValidatorSet(
		ctx,
		chainRPCURL,
		privateKey,
		validatorManagerProxy,
		conversionData,
		signedMessage,
		contractsPath,
	)
}

// WaitForAggregatedSignature polls the Glacier API until the signature is available
func WaitForAggregatedSignature(
	ctx context.Context,
	network string,
	txHash string,
	apiKey string,
	maxRetries int,
) ([]byte, error) {

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		sig, err := GetAggregatedSignature(ctx, network, txHash, apiKey)
		if err == nil {
			return sig, nil
		}

		lastErr = err

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Wait before retrying
		fmt.Printf("    Waiting for signature aggregation (attempt %d/%d)...\n", i+1, maxRetries)
		time.Sleep(10 * time.Second)
	}

	return nil, fmt.Errorf("failed to get aggregated signature after %d attempts: %w", maxRetries, lastErr)
}

// GlacierSignatureResponse is the response from the Glacier signature aggregator API
type GlacierSignatureResponse struct {
	SignedMessage string `json:"signedMessage"`
}

// GetAggregatedSignature fetches the aggregated warp message signature from Glacier API
func GetAggregatedSignature(
	ctx context.Context,
	network string,
	txHash string,
	apiKey string,
) ([]byte, error) {

	// Map network names
	glacierNetwork := network
	if network == "fuji" {
		glacierNetwork = "testnet"
	}

	url := fmt.Sprintf("https://data-api.avax.network/v1/signatureAggregator/%s/aggregateSignatures/%s",
		glacierNetwork, txHash)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request to data-api.avax.network: %w", err)
	}

	if apiKey != "" {
		req.Header.Set("x-glacier-api-key", apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Glacier API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("Glacier API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result GlacierSignatureResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Decode the hex-encoded signed message
	signedMessage := strings.TrimPrefix(result.SignedMessage, "0x")
	return hex.DecodeString(signedMessage)
}

// InitializeValidatorSet calls initializeValidatorSet() on the ValidatorManager contract
//
// rpcURL endpoint of the blockchain rpc node
//
// privateKey is the wallet used to pay for the transaction gas fee
//
//	converstionData container of the validators assigned to the blockchain
//
// signedWarpMessage is a signed data packet used for direct cross-chain communication between independent Avalanche L1s without relying on 3rd party bridges. Instead of wrapping tokens or trusting external validators, AWM uses Avalanche's built-in consensus layer and validator set to prove that an event occurred on one chain so another chain can safely act on it.
//
// contractsPath is the path to icm-contracts repository (e.g. https://github.com/ava-labs/icm-services)
func InitializeValidatorSet(
	ctx context.Context,
	rpcURL string,
	privateKey string,
	validatorManagerProxy string,
	conversionData ConversionData,
	signedWarpMessage []byte,
	contractsPath string,
) error {

	// Encode the ConversionData struct for the contract call
	// struct ConversionData {
	//   bytes32 subnetID;
	//   bytes32 validatorManagerBlockchainID;
	//   address validatorManagerAddress;
	//   InitialValidator[] initialValidators;
	// }
	// struct InitialValidator {
	//   bytes nodeID;
	//   bytes blsPublicKey;
	//   uint64 weight;
	// }

	// Build the initialValidators array encoding
	validatorsEncoded := make([]string, len(conversionData.InitialValidators))
	for i, v := range conversionData.InitialValidators {
		validatorsEncoded[i] = fmt.Sprintf("(0x%s,0x%s,%d)",
			hex.EncodeToString(v.NodeID),
			hex.EncodeToString(v.BlsPublicKey),
			v.Weight,
		)
	}
	validatorsArray := "[" + strings.Join(validatorsEncoded, ",") + "]"

	// Build the ConversionData tuple
	conversionDataEncoded := fmt.Sprintf("(0x%s,0x%s,%s,%s)",
		hex.EncodeToString(conversionData.SubnetID[:]),
		hex.EncodeToString(conversionData.ValidatorManagerBlockchainID[:]),
		conversionData.ValidatorManagerAddress,
		validatorsArray,
	)

	// The messageIndex is 0 for the initial conversion
	messageIndex := uint32(0)

	// Use cast to send the transaction with the warp message in the access list
	// The warp message needs to be included in the transaction's access list
	// at the warp precompile address (0x0200000000000000000000000000000000000005)
	warpPrecompile := "0x0200000000000000000000000000000000000005" //address of the Avalanche WarpMessenger smart contract, natively baked directly into the EVM.
	warpMessageHex := "0x" + hex.EncodeToString(signedWarpMessage)

	// Build the function call
	functionSig := "initializeValidatorSet((bytes32,bytes32,address,(bytes,bytes,uint64)[]),uint32)"
	callArgs := fmt.Sprintf("%s %d", conversionDataEncoded, messageIndex)

	// Use cast with access-list for the warp message
	cmd := exec.CommandContext(ctx, "cast", "send",
		"--rpc-url", rpcURL, //endpoint of the blockchain rpc node
		"--private-key", privateKey,
		"--json",
		"--access-list", fmt.Sprintf("%s:%s", warpPrecompile, warpMessageHex), //access list pre-specifies the smart contract addresses and storage slots that your transaction plans to read or modify.
		validatorManagerProxy,
		functionSig,
		callArgs,
	)

	if contractsPath != "" {
		cmd.Dir = contractsPath
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to call initializeValidatorSet: %w\nOutput: %s", err, string(output))
	}

	// Parse the response to check success
	var result struct {
		Status string `json:"status"`
		TxHash string `json:"transactionHash"`
	}
	if err := json.Unmarshal(output, &result); err == nil {
		if result.Status == "0x1" || result.Status == "1" {
			fmt.Printf("    initializeValidatorSet succeeded: %s\n", result.TxHash)
			return nil
		}
	}

	// If we can't parse as JSON, check for success in the output
	if strings.Contains(string(output), "success") || strings.Contains(string(output), "0x1") {
		return nil
	}

	return fmt.Errorf("initializeValidatorSet may have failed. Output: %s", string(output))
}

// DeployValidatorManagerWithForge deploys the validator manager contracts using Forge.  Specifically, it will do the following:
//   - Determine the path containing the smart contracts to be deployed.
//   - Using "forge", deploy the Validator Manager smart contract implementation.
//   - Using "forge", deploy the Validator Manager proxy smart contract.
//   - Using "forge", initialize the validator manager.
//   - Using "forge, deploy icm-contracts/avalanche/validator-manager/PoAManager.sol
//
// Forge is an open source cmd line tool used to deploy smart contracts to the blockchain.
//
// Explanation of args:
//
//	ownerAddress - your wallet's shorten (20 bytes) private key which is allowed to deploy smart contracts.
//	privateKey - your wallet's private key which is allowed to deploy smart contracts.
//	validator manager type of "poa", "native-staking", or "erc20-staking".  If "poa", this function will deploy the PoAManager smart contract.
//	rpc the destination blockchain rpc endpoint.
func DeployValidatorManagerWithForge(
	ctx context.Context,
	rpcURL string,
	privateKey string,
	subnetID ids.ID,
	ownerAddress string,
	managerType ValidatorManagerType,
	icmContractsPath string,
) (*ValidatorManagerDeployment, error) {
	// Check if forge is available
	if _, err := exec.LookPath("forge"); err != nil {
		return nil, fmt.Errorf("forge not found. Please install foundry: curl -L https://foundry.paradigm.xyz | bash && foundryup")
	}

	// Determine the contracts path
	if icmContractsPath == "" {
		// Try common locations. ICM_CONTRACTS_PATH is the canonical env var;
		// ICM_SERVICES_PATH is deprecated and checked only for backwards
		// compatibility with older setups.
		possiblePaths := []string{
			os.Getenv("ICM_CONTRACTS_PATH"),
			os.Getenv("ICM_SERVICES_PATH"), // Deprecated: use ICM_CONTRACTS_PATH instead
			filepath.Join(os.Getenv("HOME"), "code/icm-contracts"),
			"../../../icm-contracts",
			"../../icm-contracts",
		}
		for _, p := range possiblePaths {
			if p != "" {
				foundryPath := filepath.Join(p, "foundry.toml")
				if _, err := os.Stat(foundryPath); err == nil {
					icmContractsPath = p
					break
				}
			}
		}
		if icmContractsPath == "" {
			return nil, fmt.Errorf("icm-contracts path not found. Set ICM_CONTRACTS_PATH or provide --contracts-path")
		}
	}

	fmt.Printf(" Using path %s for contracts to be deploy.\n", icmContractsPath)

	deployment := &ValidatorManagerDeployment{}

	// Step 1: Deploy ValidatorManager implementation
	fmt.Println("  Deploying ValidatorManager implementation...")
	vmImplAddr, err := ForgeCreate(ctx, icmContractsPath, rpcURL, privateKey,
		"icm-contracts/avalanche/validator-manager/ValidatorManager.sol:ValidatorManager", //actual solidity code, relative to core.ContractsPath
		"--constructor-args", "0") // ICMInitializable.Allowed = 0
	if err != nil {
		return nil, fmt.Errorf("failed to deploy ValidatorManager: %w", err)
	}

	deployment.ValidatorManagerImpl = vmImplAddr
	fmt.Printf("    Implementation: %s\n", vmImplAddr)

	// Step 2: Deploy TransparentUpgradeableProxy
	fmt.Println("  Deploying TransparentUpgradeableProxy...")
	// Initialize data is empty - we'll call initialize separately
	proxyAddr, err := ForgeCreate(ctx, icmContractsPath, rpcURL, privateKey,
		"lib/openzeppelin-contracts/contracts/proxy/transparent/TransparentUpgradeableProxy.sol:TransparentUpgradeableProxy",
		"--constructor-args", vmImplAddr, ownerAddress, "0x")
	if err != nil {
		// Try alternative path for OpenZeppelin contracts
		proxyAddr, err = ForgeCreate(ctx, icmContractsPath, rpcURL, privateKey,
			"lib/openzeppelin-contracts-upgradeable/lib/openzeppelin-contracts/contracts/proxy/transparent/TransparentUpgradeableProxy.sol:TransparentUpgradeableProxy",
			"--constructor-args", vmImplAddr, ownerAddress, "0x")
		if err != nil {
			return nil, fmt.Errorf("failed to deploy proxy: %w", err)
		}
	}

	deployment.ValidatorManagerProxy = proxyAddr
	deployment.ProxyAdmin = ownerAddress
	fmt.Printf("    Proxy: %s, owner address:%s\n", proxyAddr, ownerAddress)

	// Step 3: Initialize ValidatorManager
	fmt.Println("  Initializing ValidatorManager...")
	subnetIDHex := "0x" + hex.EncodeToString(subnetID[:])
	initCalldata := EncodeValidatorManagerInit(ownerAddress, subnetIDHex, 0, 20)

	_, err = ForgeSend(ctx, icmContractsPath, rpcURL, privateKey, proxyAddr, initCalldata)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ValidatorManager: %w", err)
	}
	fmt.Println("    ValidatorManager initialized")

	// Step 4: Deploy PoAManager
	switch managerType {
	case PoAValidatorManagerType:
		fmt.Println("  Deploying PoAManager...")
		poaAddr, err := ForgeCreate(ctx, icmContractsPath, rpcURL, privateKey,
			"icm-contracts/avalanche/validator-manager/PoAManager.sol:PoAManager",
			"--constructor-args", ownerAddress, proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy PoAManager: %w", err)
		}
		deployment.PoAManager = poaAddr
		fmt.Printf("    PoAManager: %s\n", poaAddr)

		// Step 5: Transfer ownership to PoAManager
		fmt.Println("  Transferring ValidatorManager ownership to PoAManager...")
		transferCalldata := EncodeTransferOwnership(poaAddr)
		_, err = ForgeSend(ctx, icmContractsPath, rpcURL, privateKey, proxyAddr, transferCalldata)
		if err != nil {
			return nil, fmt.Errorf("failed to transfer ownership: %w", err)
		}
		fmt.Println("    Ownership transferred")

	case NativeTokenStakingManagerType, ERC20TokenStakingManagerType:
		return nil, fmt.Errorf("staking manager deployment not yet implemented - use PoA for now")
	}

	return deployment, nil

}

// ForgeCreate deploys new smart contracts from Solidity source files.
//
//	Arguments:
//		rpcURL encapsulates the blockchain network destination of the smart contract.
//		privateKey - the wallet private key use to sign the transaction.
func ForgeCreate(ctx context.Context, workDir, rpcURL, privateKey, contract string, args ...string) (string, error) {
	cmdArgs := []string{
		"create",
		"--rpc-url", rpcURL,
		"--private-key", privateKey,
		"--json",
		contract,
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "forge", cmdArgs...)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("forge create failed: %w\nOutput: %s", err, string(output))
	}

	// Parse JSON output to get deployed address
	var result struct {
		DeployedTo string `json:"deployedTo"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		// Try to extract address from non-JSON output
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Deployed to:") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					return strings.TrimSpace(parts[1]), nil
				}
			}
		}
		return "", fmt.Errorf("failed to parse forge output: %w\nOutput: %s", err, string(output))
	}

	return result.DeployedTo, nil
}

// ForgeSend interact with existing contracts or send raw transactions.
//
//	Arguments:
//		wordDir path to the icm contracts Solidity source files
//		rpcURL encapsulates the blockchain network destination of the smart contract.x
//		privateKey - the wallet private key use to sign the transaction.
//		to the smart contract address (e.g. the proxy fronting the smart contract).  We will interact with this smart contract.
//		callData is the smart contract function to call.
func ForgeSend(
	ctx context.Context,
	workDir string,
	rpcURL string,
	privateKey string,
	to string,
	calldata string,
) (string, error) {

	cmd := exec.CommandContext(ctx, "cast", "send",
		"--rpc-url", rpcURL,
		"--private-key", privateKey,
		"--json",
		to,
		calldata,
	)

	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cast send failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

// EncodeValidatorManagerInit encodes the call for ValidatorManager::initialize().
//
//	Note:  this does not add any node as validator; rather, it performs admin type of actions (e.g. the subnet for the smart contract, the admin of contract).
func EncodeValidatorManagerInit(admin, subnetID string, churnPeriodSeconds uint64, maxChurnPercentage uint8) string {
	// function initialize(ValidatorManagerSettings calldata settings)
	// struct ValidatorManagerSettings { address admin; bytes32 subnetID; uint64 churnPeriodSeconds; uint8 maximumChurnPercentage; }
	// For now, use cast to encode the call
	// cast calldata "initialize((address,bytes32,uint64,uint8))" "(admin,subnetID,0,20)"
	return fmt.Sprintf("initialize((address,bytes32,uint64,uint8)) (%s,%s,%d,%d)",
		admin, subnetID, churnPeriodSeconds, maxChurnPercentage)
}

// EncodeTransferOwnership encodes the transferOwnership call.
//
// returns "transferOwnership(address) newOwnerAddress"
func EncodeTransferOwnership(newOwner string) string {
	return fmt.Sprintf("transferOwnership(address) %s", newOwner)
}

// GetManagerProxyAddressForConversion returns the manager proxy address bytes for ConvertSubnetToL1Tx
func (d *ValidatorManagerDeployment) GetManagerProxyAddressForConversion() ([]byte, error) {
	addr := d.ValidatorManagerProxy
	if addr == "" {
		return nil, fmt.Errorf("no validator manager proxy address")
	}
	addr = strings.TrimPrefix(addr, "0x")
	return hex.DecodeString(addr)
}

// SaveDeployment saves the deployment info to a JSON file
func (d *ValidatorManagerDeployment) SaveDeployment(filepath string) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, data, 0644)
}
