package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/push-protocol/push-chain/x/usvl/types"
)

// TransactionVerificationResult contains information about the verification result
type TransactionVerificationResult struct {
	Verified bool
	TxInfo   string
}

// VerifyExternalTransaction verifies a transaction on an external chain
func (k Keeper) VerifyExternalTransaction(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error) {
	// Parse CAIP address
	caip, err := types.ParseCAIPAddress(caipAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CAIP address: %w", err)
	}

	// Get chain ID from CAIP format
	chainIdentifier := caip.GetChainIdentifier()

	// Find matching chain config
	var matchedConfig types.ChainConfigData
	var found bool

	// Get all chain configs
	chainConfigs, err := k.GetAllChainConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain configs: %w", err)
	}

	// Find the chain config with matching CAIP prefix
	for _, config := range chainConfigs {
		if config.CaipPrefix == chainIdentifier {
			matchedConfig = config
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("no chain configuration found for CAIP prefix %s", chainIdentifier)
	}

	// Use the chain configuration to determine how to verify the transaction
	switch matchedConfig.VmType {
	case types.VmTypeEvm:
		return k.verifyEVMTransaction(ctx, matchedConfig, txHash, caip.Address)

	default:
		return nil, fmt.Errorf("unsupported VM type: %d", matchedConfig.VmType)
	}
}

// verifyEVMTransaction verifies a transaction on an EVM-compatible chain
func (k Keeper) verifyEVMTransaction(ctx context.Context, config types.ChainConfigData, txHash string, address string) (*TransactionVerificationResult, error) {
	// Create a production-grade RPC client
	client := newRPCClient()

	// Call RPC with proper retry logic
	responseBytes, err := client.callRPC(ctx, config.PublicRpcUrl, "eth_getTransactionByHash", []interface{}{txHash})
	if err != nil {
		return nil, fmt.Errorf("EVM RPC error: %w", err)
	}

	// Decode the response
	var rpcResponse struct {
		Result struct {
			Hash             string `json:"hash"`
			From             string `json:"from"`
			To               string `json:"to"`
			BlockNumber      string `json:"blockNumber"`
			TransactionIndex string `json:"transactionIndex"`
			Value            string `json:"value"`
		} `json:"result"`
	}

	if err := json.Unmarshal(responseBytes, &rpcResponse); err != nil {
		return nil, fmt.Errorf("failed to decode RPC response: %w", err)
	}

	// Check if transaction exists
	if rpcResponse.Result.Hash == "" {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   "Transaction not found",
		}, nil
	}

	// Convert both addresses to EIP-55 checksum format for standardized comparison
	fromAddress := normalizeCaseInsensitiveAddress(rpcResponse.Result.From)
	inputAddress := normalizeCaseInsensitiveAddress(address)

	// Verify that the transaction is from the expected address
	if fromAddress != inputAddress {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   fmt.Sprintf("Transaction exists but is from %s, not %s", fromAddress, inputAddress),
		}, nil
	}

	// Update the response data (all addresses already in checksum format)
	txInfoResponse := struct {
		Hash             string `json:"hash"`
		From             string `json:"from"`
		To               string `json:"to,omitempty"`
		BlockNumber      string `json:"blockNumber"`
		TransactionIndex string `json:"transactionIndex"`
		Value            string `json:"value"`
	}{
		Hash:             rpcResponse.Result.Hash,
		From:             fromAddress,
		BlockNumber:      rpcResponse.Result.BlockNumber,
		TransactionIndex: rpcResponse.Result.TransactionIndex,
		Value:            rpcResponse.Result.Value,
	}

	// Convert "to" address to checksum if it exists
	if rpcResponse.Result.To != "" {
		txInfoResponse.To = toChecksumAddress(rpcResponse.Result.To)
	}

	// Transaction is verified
	txInfoJSON, _ := json.Marshal(txInfoResponse)
	return &TransactionVerificationResult{
		Verified: true,
		TxInfo:   string(txInfoJSON),
	}, nil
}

// Helper functions

// requestBodyReader is a simple io.Reader for the request body
type requestBodyReader struct {
	data []byte
}

func newRequestBodyReader(data []byte) *requestBodyReader {
	return &requestBodyReader{data: data}
}

func (r *requestBodyReader) Read(p []byte) (n int, err error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n = copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// normalizeCaseInsensitiveAddress standardizes an Ethereum address to EIP-55 checksummed format
// for consistent address format throughout the system
func normalizeCaseInsensitiveAddress(address string) string {
	// Just use the checksummed format - we want to standardize on checksum addresses everywhere
	return toChecksumAddress(address)
}

// toChecksumAddress converts an Ethereum address to EIP-55 checksum format
// Uses go-ethereum's implementation for proper industry-standard checksumming
func toChecksumAddress(address string) string {
	// Use the standard go-ethereum library's implementation which handles the checksumming properly
	return common.HexToAddress(address).Hex()
}

// ensureChecksumAddress ensures the address is in proper EIP-55 checksum format
// It's used to standardize addresses before any operations or comparisons
func ensureChecksumAddress(address string) string {
	return toChecksumAddress(address)
}
