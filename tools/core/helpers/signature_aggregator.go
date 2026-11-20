package helpers

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

/*
Based on create-l1/initialize_validator_set.go
*/

// BuildSubnetToL1ConversionMessage builds the unsigned warp message for subnet conversion
//
// Returns the unsigned message bytes and justification bytes.  "justification" is the subnetID for conversion messages.
//
// args:
//
//	nodeIDs is the set of validators for the L1
//	nodePoPs is the set of Proof of Possession for each validator.  PoP proves a node operator owns the private key corresponding to their public validating key
func BuildSubnetToL1ConversionMessage(
	networkID uint32,
	subnetID ids.ID,
	chainID ids.ID,
	validatorManagerAddress string,
	nodeIDs []ids.NodeID,
	nodePoPs []*signer.ProofOfPossession,
	weights []uint64,
) ([]byte, []byte, error) {
	// Build the ConversionData
	conversionData := ConversionData{
		SubnetID:                     subnetID,
		ValidatorManagerBlockchainID: chainID,
		ValidatorManagerAddress:      validatorManagerAddress,
		InitialValidators:            make([]InitialValidator, len(nodeIDs)),
	}
	// Build the set of validators part of ConversionData
	for i := range nodeIDs {
		conversionData.InitialValidators[i] = InitialValidator{
			NodeID:       nodeIDs[i].Bytes(),
			BlsPublicKey: nodePoPs[i].PublicKey[:],
			Weight:       weights[i],
		}
	}
	// Calculate the conversionID
	conversionID, err := CalculateConversionID(conversionData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to calculate conversionID: %w", err)
	}

	// Build the SubnetToL1ConversionMessage payload
	// This is: codecID (2) + typeID (4) + conversionID (32) = 38 bytes
	payload := make([]byte, 38)
	// codecID = 0
	payload[0] = 0
	payload[1] = 0
	// typeID = 0 (SUBNET_TO_L1_CONVERSION_MESSAGE_TYPE_ID)
	payload[2] = 0
	payload[3] = 0
	payload[4] = 0
	payload[5] = 0
	// conversionID
	copy(payload[6:38], conversionID[:])

	// Build the AddressedCall payload
	// sourceAddress length (4 bytes) = 0 (nil source)
	// sourceAddress = empty
	// payload
	addressedCall := make([]byte, 4+len(payload))
	// sourceAddress length = 0
	addressedCall[0] = 0
	addressedCall[1] = 0
	addressedCall[2] = 0
	addressedCall[3] = 0
	copy(addressedCall[4:], payload)

	// Build the unsigned warp message
	// networkID (4) + sourceChainID (32) + payload
	unsignedMsg := make([]byte, 4+32+len(addressedCall))
	unsignedMsg[0] = byte(networkID >> 24)
	unsignedMsg[1] = byte(networkID >> 16)
	unsignedMsg[2] = byte(networkID >> 8)
	unsignedMsg[3] = byte(networkID)

	// P-Chain ID is all zeros for primary network
	// Actually for warp messages from P-Chain, we use the P-Chain's blockchain ID
	// which is 11111111111111111111111111111111LpoYY (the primary network's P-Chain)
	pChainID := ids.Empty // Primary network P-Chain
	copy(unsignedMsg[4:36], pChainID[:])
	copy(unsignedMsg[36:], addressedCall)

	// Justification is the subnetID for conversion messages
	justification := subnetID[:]

	return unsignedMsg, justification, nil
}

// CalculateConversionID computes the conversionID from ConversionData
// This matches the P-Chain's calculation and the contract's verification
func CalculateConversionID(data ConversionData) (ids.ID, error) {
	// The conversionID is SHA256 of the packed ConversionData
	// This matches the packing in ValidatorMessages.sol

	// Pack: codecID (2) + subnetID (32) + managerChainID (32) +
	//       managerAddressLen (4) + managerAddress (20) +
	//       validatorCount (4) + validators...
	packed := make([]byte, 0, 94+60*len(data.InitialValidators))

	// Codec ID (0)
	packed = append(packed, 0, 0)

	// SubnetID
	packed = append(packed, data.SubnetID[:]...)

	// ValidatorManagerBlockchainID
	packed = append(packed, data.ValidatorManagerBlockchainID[:]...)

	// Manager address length (20 for EVM)
	packed = append(packed, 0, 0, 0, 20)

	// Manager address (decode from hex string)
	addrHex := strings.TrimPrefix(data.ValidatorManagerAddress, "0x")
	addrBytes, err := hex.DecodeString(addrHex)
	if err != nil {
		return ids.Empty, fmt.Errorf("invalid manager address: %w", err)
	}
	packed = append(packed, addrBytes...)

	// Validator count
	count := uint32(len(data.InitialValidators))
	packed = append(packed, byte(count>>24), byte(count>>16), byte(count>>8), byte(count))

	// Each validator: nodeIDLen (4) + nodeID + blsKey (48) + weight (8)
	for _, v := range data.InitialValidators {
		// NodeID length
		nodeIDLen := uint32(len(v.NodeID))
		packed = append(packed, byte(nodeIDLen>>24), byte(nodeIDLen>>16), byte(nodeIDLen>>8), byte(nodeIDLen))

		// NodeID
		packed = append(packed, v.NodeID...)

		// BLS public key (48 bytes)
		if len(v.BlsPublicKey) != bls.PublicKeyLen {
			return ids.Empty, fmt.Errorf("invalid BLS public key length: %d", len(v.BlsPublicKey))
		}
		packed = append(packed, v.BlsPublicKey...)

		// Weight (big-endian uint64)
		packed = append(packed,
			byte(v.Weight>>56), byte(v.Weight>>48), byte(v.Weight>>40), byte(v.Weight>>32),
			byte(v.Weight>>24), byte(v.Weight>>16), byte(v.Weight>>8), byte(v.Weight))
	}

	return ids.ToID(packed)
}

// CallLocalSignatureAggregatorWithRetry calls a running local signature-aggregator to sign the unsigned message,
//
//	args:
//	 url - local signature aggregator endpoint
//	 signingSubnetID - is just the subnet id
//	 unsignedMessage - unsigned warp message for subnet conversion.  This is the payload sent to the local signal aggregator.
//	 Justification is the subnetID for conversion messages
func CallLocalSignatureAggregatorWithRetry(
	ctx context.Context,
	url string,
	unsignedMessage []byte,
	justification []byte,
	signingSubnetID ids.ID,
	quorumPercentage int,
	maxRetries int,
) ([]byte, error) {

	var lastErr error

	for i := 0; i < maxRetries; i++ {
		sig, err := CallLocalSignatureAggregator(ctx, url, unsignedMessage, justification, signingSubnetID, quorumPercentage)
		if err == nil {
			fmt.Printf(" Local aggregator signed our message, received signature: %s\n", sig)
			return sig, nil
		}

		lastErr = err

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if i < maxRetries-1 {
			fmt.Printf("    Retry %d/%d: %v\n", i+1, maxRetries, err)
			time.Sleep(5 * time.Second)
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// SignatureAggregatorRequest is the request body for /aggregate-signatures
type SignatureAggregatorRequest struct {
	Message                string `json:"message"`
	Justification          string `json:"justification,omitempty"`
	SigningSubnetID        string `json:"signing-subnet-id,omitempty"`
	QuorumPercentage       int    `json:"quorum-percentage,omitempty"`
	QuorumPercentageBuffer int    `json:"quorum-percentage-buffer,omitempty"`
	PChainHeight           int64  `json:"p-chain-height,omitempty"`
}

// SignatureAggregatorResponse is the response from /aggregate-signatures
type SignatureAggregatorResponse struct {
	SignedMessage string `json:"signed-message"`
	Error         string `json:"error,omitempty"`
}

// CallLocalSignatureAggregator calls a running local signature-aggregator to sign the unsigned message,
// REST endpoint /aggregate-signatures.
//
// args:
//
//	url - local signature aggregator endpoint
//	signingSubnetID - is just the subnet id
//	unsignedMessage - unsigned warp message for subnet conversion.  This is the payload sent to the local signal aggregator for it to sign.
//	Justification is the subnetID for conversion messages
func CallLocalSignatureAggregator(
	ctx context.Context,
	url string,
	unsignedMessage []byte,
	justification []byte,
	signingSubnetID ids.ID,
	quorumPercentage int,
) ([]byte, error) {

	if quorumPercentage == 0 {
		quorumPercentage = 67
	}

	req := SignatureAggregatorRequest{
		Message:          hex.EncodeToString(unsignedMessage),
		QuorumPercentage: quorumPercentage,
	}

	if len(justification) > 0 {
		req.Justification = hex.EncodeToString(justification)
	}

	if signingSubnetID != ids.Empty {
		req.SigningSubnetID = signingSubnetID.String()
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/aggregate-signatures", strings.TrimSuffix(url, "/"))
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call signature-aggregator: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result SignatureAggregatorResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(body))
	}

	if result.Error != "" {
		return nil, fmt.Errorf("signature aggregation failed: %s", result.Error)
	}

	signedMessage := strings.TrimPrefix(result.SignedMessage, "0x")
	return hex.DecodeString(signedMessage)
}
