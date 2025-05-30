package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/push-protocol/push-chain/utils/env"
	"github.com/push-protocol/push-chain/x/utv/types"
)

// VerifyExternalTransactionToLocker verifies a transaction on an external chain
// ensuring it's directed to the locker contract for that chain
func (k Keeper) VerifyExternalTransactionToLocker(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error) {
	// Check if this transaction has already been verified with the exact same address
	isVerified, err := k.IsTransactionVerified(ctx, txHash, caipAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to check transaction verification status: %w", err)
	}

	if isVerified {
		return nil, fmt.Errorf("transaction %s for address %s has already been verified", txHash, caipAddress)
	}

	// Check if this transaction hash has been verified with a different address
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
		result, err = k.verifyEVMTransactionToLocker(ctx, matchedConfig, txHash, caip.Address)
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

// verifyEVMTransactionToLocker verifies a transaction on an EVM-compatible chain
// ensuring it's directed to the locker contract for that chain
func (k Keeper) verifyEVMTransactionToLocker(ctx context.Context, config types.ChainConfigData, txHash string, address string) (*TransactionVerificationResult, error) {
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

	// SPECIAL ADDITIONAL CHECK: Verify that the transaction is TO the locker contract
	if txDetails.To == "" {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   "Transaction is a contract deployment, not a transfer to locker contract",
		}, nil
	}

	// Get the locker contract address from chain config and normalize for comparison
	lockerAddress := normalizeCaseInsensitiveAddress(config.LockerContractAddress)
	transactionTo := txDetails.To

	// Check if transaction is directed to the locker contract
	if transactionTo != lockerAddress {
		return &TransactionVerificationResult{
			Verified: false,
			TxInfo:   fmt.Sprintf("Transaction is not directed to the locker contract. Expected: %s, Got: %s", lockerAddress, transactionTo),
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
