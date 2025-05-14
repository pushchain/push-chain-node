package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	// Check if this transaction has already been verified with the exact same address
	isVerified, err := k.IsTransactionVerified(ctx, txHash, caipAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to check transaction verification status: %w", err)
	}

	if isVerified {
		return nil, fmt.Errorf("transaction %s for address %s has already been verified", txHash, caipAddress)
	}

	// Check if this transaction hash has been verified with a different address
	// by searching through all verified transactions
	iterator, err := k.VerifiedTxs.Iterate(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate verified transactions: %w", err)
	}
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		key, err := iterator.Key()
		if err != nil {
			return nil, fmt.Errorf("failed to get key from iterator: %w", err)
		}

		// The key format is txHash:caipAddress
		parts := strings.Split(key, ":")
		if len(parts) >= 2 && parts[0] == txHash && parts[1] != caipAddress {
			// Found the same transaction hash but with a different address
			serialized, err := k.VerifiedTxs.Get(ctx, key)
			if err != nil {
				return nil, fmt.Errorf("failed to get verified transaction: %w", err)
			}

			var storedTx types.VerifiedTransaction
			err = json.Unmarshal([]byte(serialized), &storedTx)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal stored transaction: %w", err)
			}

			return nil, fmt.Errorf("transaction %s exists but is from %s, not %s",
				txHash, storedTx.CaipAddress, caipAddress)
		}
	}

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
	var result *TransactionVerificationResult

	switch matchedConfig.VmType {
	case types.VmTypeEvm:
		result, err = k.verifyEVMTransaction(ctx, matchedConfig, txHash, caip.Address)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unsupported VM type: %d", matchedConfig.VmType)
	}

	// If verification was successful, store the transaction in our KV store
	if result.Verified {
		if err := k.StoreVerifiedTransaction(ctx, txHash, caipAddress, matchedConfig.ChainId); err != nil {
			return nil, fmt.Errorf("failed to store verified transaction: %w", err)
		}
	}

	return result, nil
}

// verifyEVMTransaction verifies a transaction on an EVM-compatible chain
func (k Keeper) verifyEVMTransaction(ctx context.Context, config types.ChainConfigData, txHash string, address string) (*TransactionVerificationResult, error) {
	// Use the global RPC client so it can be mocked in tests
	responseBytes, err := globalRPCClient.callRPC(ctx, config.PublicRpcUrl, "eth_getTransactionByHash", []interface{}{txHash})
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

	// Check if transaction has been mined (has a block number)
	if rpcResponse.Result.BlockNumber == "" {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   "Transaction is pending and not yet mined",
		}, nil
	}

	// Get current block number for confirmation check
	currentBlockBytes, err := globalRPCClient.callRPC(ctx, config.PublicRpcUrl, "eth_blockNumber", []interface{}{})
	if err != nil {
		return nil, fmt.Errorf("failed to get current block number: %w", err)
	}

	var currentBlockResponse struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(currentBlockBytes, &currentBlockResponse); err != nil {
		return nil, fmt.Errorf("failed to decode current block response: %w", err)
	}

	// Convert hex block numbers to integers
	txBlockNumHex := rpcResponse.Result.BlockNumber
	currentBlockNumHex := currentBlockResponse.Result

	txBlockNum, err := hexToUint64(txBlockNumHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction block number: %w", err)
	}

	currentBlockNum, err := hexToUint64(currentBlockNumHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse current block number: %w", err)
	}

	// Calculate confirmations
	confirmations := currentBlockNum - txBlockNum
	// Check if transaction has enough confirmations
	if confirmations < config.BlockConfirmation {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   fmt.Sprintf("Transaction has only %d confirmations, %d required", confirmations, config.BlockConfirmation),
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
		Confirmations    uint64 `json:"confirmations"`
	}{
		Hash:             rpcResponse.Result.Hash,
		From:             fromAddress,
		BlockNumber:      rpcResponse.Result.BlockNumber,
		TransactionIndex: rpcResponse.Result.TransactionIndex,
		Value:            rpcResponse.Result.Value,
		Confirmations:    confirmations,
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

// hexToUint64 converts a hexadecimal string to uint64
func hexToUint64(hex string) (uint64, error) {
	// Remove "0x" prefix if present
	if len(hex) >= 2 && hex[0] == '0' && (hex[1] == 'x' || hex[1] == 'X') {
		hex = hex[2:]
	}

	// Parse the hexadecimal string
	var result uint64
	_, err := fmt.Sscanf(hex, "%x", &result)
	if err != nil {
		return 0, fmt.Errorf("failed to parse hex string %s: %w", hex, err)
	}

	return result, nil
}
