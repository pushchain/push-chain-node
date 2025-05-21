package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/push-protocol/push-chain/utils/env"
	"github.com/push-protocol/push-chain/x/usvl/types"
)

// TransactionVerificationResult contains information about the verification result
type TransactionVerificationResult struct {
	Verified bool
	TxInfo   string
}

type LogEntry struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type TransactionDetails struct {
	TransactionHash  string     `json:"transactionHash"`
	BlockNumber      string     `json:"blockNumber"`
	TransactionIndex string     `json:"transactionIndex"`
	From             string     `json:"from"`
	To               string     `json:"to,omitempty"`
	Value            string     `json:"value"`
	Status           string     `json:"status"`
	GasUsed          string     `json:"gasUsed"`
	CurrentBlockNum  string     `json:"currentBlockNum"`
	Logs             []LogEntry `json:"logs"`
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

	// First check the in-memory cache for a matching chain config by CAIP prefix
	var matchedConfig types.ChainConfigData
	var found bool

	// Try to find the config in the cache first
	matchedConfig, found = k.configCache.GetByCaipPrefix(chainIdentifier)

	// If not found in cache, load from chain state
	if !found {
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

				// Add to cache for future use
				k.configCache.Set(config.ChainId, config)
				break
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("no chain configuration found for CAIP prefix %s", chainIdentifier)
	}

	// Check for environment variable override for RPC URL
	if customRPC, found := env.GetRpcUrlOverride(matchedConfig.ChainId); found {
		k.logger.Info("Using custom RPC from environment",
			"chain_id", matchedConfig.ChainId,
			"original_rpc", matchedConfig.PublicRpcUrl,
			"custom_rpc", customRPC)
		configCopy := matchedConfig
		configCopy.PublicRpcUrl = customRPC
		matchedConfig = configCopy
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

// fetches details of the transaction from the EVM-compatible chain
func (k Keeper) fetchTransactionDetails(ctx context.Context, config types.ChainConfigData, txHash string) (*TransactionDetails, error) {
	// Log the RPC URL being used
	k.logger.Info("Making RPC call to fetch transaction details",
		"txHash", txHash,
		"chainId", config.ChainId,
		"rpcUrl", config.PublicRpcUrl)

	// Get transaction details
	txResponseBytes, err := globalRPCClient.callRPC(ctx, config.PublicRpcUrl, "eth_getTransactionByHash", []interface{}{txHash})
	if err != nil {
		return nil, fmt.Errorf("EVM RPC error getting transaction: %w", err)
	}

	var txResponse struct {
		Result struct {
			Hash             string `json:"hash"`
			From             string `json:"from"`
			To               string `json:"to"`
			BlockNumber      string `json:"blockNumber"`
			TransactionIndex string `json:"transactionIndex"`
			Value            string `json:"value"`
		} `json:"result"`
	}

	if err := json.Unmarshal(txResponseBytes, &txResponse); err != nil {
		return nil, fmt.Errorf("failed to decode transaction response: %w", err)
	}

	// Check if transaction exists
	if txResponse.Result.Hash == "" {
		return nil, fmt.Errorf("transaction not found")
	}

	// Check if transaction has been mined (has a block number)
	if txResponse.Result.BlockNumber == "" {
		return nil, fmt.Errorf("transaction is pending and not yet mined")
	}

	// Get transaction receipt to get logs and status
	receiptBytes, err := globalRPCClient.callRPC(ctx, config.PublicRpcUrl, "eth_getTransactionReceipt", []interface{}{txHash})
	if err != nil {
		return nil, fmt.Errorf("EVM RPC error getting receipt: %w", err)
	}

	var receiptResponse struct {
		Result struct {
			TransactionHash  string `json:"transactionHash"`
			BlockNumber      string `json:"blockNumber"`
			TransactionIndex string `json:"transactionIndex"`
			Status           string `json:"status"`
			GasUsed          string `json:"gasUsed"`
			Logs             []struct {
				Address string   `json:"address"`
				Topics  []string `json:"topics"`
				Data    string   `json:"data"`
			} `json:"logs"`
		} `json:"result"`
	}

	if err := json.Unmarshal(receiptBytes, &receiptResponse); err != nil {
		return nil, fmt.Errorf("failed to decode receipt response: %w", err)
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

	// Convert addresses to checksum format
	from := toChecksumAddress(txResponse.Result.From)

	// Create merged transaction details
	txDetails := &TransactionDetails{
		TransactionHash:  txResponse.Result.Hash,
		BlockNumber:      txResponse.Result.BlockNumber,
		TransactionIndex: txResponse.Result.TransactionIndex,
		From:             from,
		CurrentBlockNum:  currentBlockResponse.Result,
	}

	// Add "to" address if it exists
	if txResponse.Result.To != "" {
		txDetails.To = toChecksumAddress(txResponse.Result.To)
	}

	// Add value
	txDetails.Value = txResponse.Result.Value

	// Add receipt data if available
	if receiptResponse.Result.Status != "" {
		txDetails.Status = receiptResponse.Result.Status
		txDetails.GasUsed = receiptResponse.Result.GasUsed

		// Convert logs to our LogEntry type
		logs := make([]LogEntry, len(receiptResponse.Result.Logs))
		for i, log := range receiptResponse.Result.Logs {
			logs[i] = LogEntry{
				Address: log.Address,
				Topics:  log.Topics,
				Data:    log.Data,
			}
		}
		txDetails.Logs = logs
	}

	// Print transaction details for debugging purposes
	k.logger.Info("Transaction details",
		"hash", txDetails.TransactionHash,
		"blockNumber", txDetails.BlockNumber,
		"currentBlock", txDetails.CurrentBlockNum,
		"txIndex", txDetails.TransactionIndex,
		"from", txDetails.From,
		"to", txDetails.To,
		"value", txDetails.Value,
		"status", txDetails.Status,
		"gasUsed", txDetails.GasUsed)

	// Calculate confirmations for debugging
	txBlockNum, _ := hexToUint64(txDetails.BlockNumber)
	currentBlockNum, _ := hexToUint64(txDetails.CurrentBlockNum)
	confirmations := currentBlockNum - txBlockNum
	k.logger.Info("Block confirmations", "confirmations", confirmations)

	// Print logs separately for better visibility
	if len(txDetails.Logs) > 0 {
		k.logger.Info("Transaction logs found", "count", len(txDetails.Logs))
		for i, log := range txDetails.Logs {
			topicsJSON, _ := json.Marshal(log.Topics)
			k.logger.Info(fmt.Sprintf("Log #%d", i+1),
				"address", log.Address,
				"topics", string(topicsJSON),
				"data", log.Data)
		}
	} else {
		k.logger.Info("No transaction logs/events found")
	}

	// For very detailed debugging, print the entire structure as JSON
	detailedJSON, _ := json.MarshalIndent(txDetails, "", "  ")
	k.logger.Debug("Transaction details (full JSON)", "details", string(detailedJSON))

	return txDetails, nil
}

// verifyEVMTransaction verifies a transaction on an EVM-compatible chain
func (k Keeper) verifyEVMTransaction(ctx context.Context, config types.ChainConfigData, txHash string, address string) (*TransactionVerificationResult, error) {
	// Fetch the transaction details
	txDetails, err := k.fetchTransactionDetails(ctx, config, txHash)
	if err != nil {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   fmt.Sprintf("Failed to fetch transaction details: %s", err.Error()),
		}, nil
	}

	// Convert block numbers to integers for confirmation check
	txBlockNum, err := hexToUint64(txDetails.BlockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction block number: %w", err)
	}

	currentBlockNum, err := hexToUint64(txDetails.CurrentBlockNum)
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

	// Convert input address to EIP-55 checksum format for standardized comparison
	inputAddress := normalizeCaseInsensitiveAddress(address)

	// Verify that the transaction is from the expected address
	if txDetails.From != inputAddress {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   fmt.Sprintf("Transaction exists but is from %s, not %s", txDetails.From, inputAddress),
		}, nil
	}

	// Update the response data (all addresses already in checksum format)
	txInfoResponse := struct {
		Hash             string          `json:"hash"`
		From             string          `json:"from"`
		To               string          `json:"to,omitempty"`
		BlockNumber      string          `json:"blockNumber"`
		TransactionIndex string          `json:"transactionIndex"`
		Value            string          `json:"value"`
		Status           string          `json:"status,omitempty"`
		GasUsed          string          `json:"gasUsed,omitempty"`
		Confirmations    uint64          `json:"confirmations"`
		Logs             json.RawMessage `json:"logs,omitempty"` // Include logs from receipt
	}{
		Hash:             txDetails.TransactionHash,
		From:             txDetails.From,
		BlockNumber:      txDetails.BlockNumber,
		TransactionIndex: txDetails.TransactionIndex,
		Value:            txDetails.Value,
		Confirmations:    confirmations,
		Status:           txDetails.Status,
		GasUsed:          txDetails.GasUsed,
	}

	// Add "to" address if it exists
	if txDetails.To != "" {
		txInfoResponse.To = txDetails.To
	}

	// Add logs data
	if len(txDetails.Logs) > 0 {
		logsJSON, _ := json.Marshal(txDetails.Logs)
		txInfoResponse.Logs = logsJSON
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
