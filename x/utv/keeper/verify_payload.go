package keeper

import (
	"context"
	"fmt"
	"strings"

	"bytes"
	"encoding/base64"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/decred/base58"
	"github.com/rollchains/pchain/utils"
	"github.com/rollchains/pchain/utils/rpc"
	evmrpc "github.com/rollchains/pchain/utils/rpc/evm"
	svmrpc "github.com/rollchains/pchain/utils/rpc/svm"
	uetypes "github.com/rollchains/pchain/x/ue/types"
)

// VerifyTxHashWithPayload verifies a transaction hash against a provided payload hash using Universal Account ID
func (k Keeper) VerifyTxHashWithPayload(ctx sdk.Context, universalAccountId uetypes.UniversalAccountId, payloadHash, txHash string) (bool, error) {
	fmt.Printf("[UTV] 🔍 Starting verification process\n")
	fmt.Printf("[UTV] Universal Account ID: %+v\n", universalAccountId)
	fmt.Printf("[UTV] Payload hash: %s\n", payloadHash)
	fmt.Printf("[UTV] Transaction hash: %s\n", txHash)

	// Extract owner and chain from Universal Account ID
	ownerKey := universalAccountId.Owner
	chain := fmt.Sprintf("%s:%s", universalAccountId.ChainNamespace, universalAccountId.ChainId)

	// Get chain configuration from UE keeper (now using real SDK context!)
	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chain)
	if err != nil {
		return false, fmt.Errorf("failed to get chain config from UE keeper: %v", err)
	}

	fmt.Printf("[UTV] ✅ Successfully retrieved chain config from UE keeper: RPC=%s, Gateway=%s\n",
		chainConfig.PublicRpcUrl, chainConfig.GatewayAddress)

	// Check if chain is enabled (matching existing pattern)
	if !chainConfig.Enabled {
		return false, fmt.Errorf("chain %s is not enabled", chain)
	}

	// Convert SDK context to regular context for RPC calls (matching existing pattern)
	ctxBackground := context.Background()

	// Verify owner and extract execution hash from transaction (using VM type like existing code)
	var executionHash string
	var isOwnerVerified bool

	switch chainConfig.VmType {
	case uetypes.VM_TYPE_SVM:
		verified, hash, err := k.verifySolanaTransaction(ctxBackground, ownerKey, txHash, chainConfig)
		if err != nil {
			return false, fmt.Errorf("svm verification failed: %v", err)
		}
		isOwnerVerified = verified
		executionHash = hash
	case uetypes.VM_TYPE_EVM:
		verified, hash, err := k.verifyEVMTransaction(ctxBackground, ownerKey, txHash, chainConfig)
		if err != nil {
			return false, fmt.Errorf("evm verification failed: %v", err)
		}
		isOwnerVerified = verified
		executionHash = hash
	default:
		return false, fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chain)
	}

	if !isOwnerVerified {
		fmt.Printf("[UTV] ❌ Owner verification failed\n")
		return false, nil
	}

	fmt.Printf("[UTV] ✅ Owner verification successful\n")
	fmt.Printf("[UTV] Provided payload hash: %s\n", payloadHash)
	fmt.Printf("[UTV] Emitted payload hash: %s\n", executionHash)

	// Compare the provided payload hash with the emitted hash
	if payloadHash != executionHash {
		fmt.Printf("[UTV] Hash mismatch: provided=%s, emitted=%s\n", payloadHash, executionHash)
		return false, nil
	}

	fmt.Printf("[UTV] ✅ Hash verification successful\n")
	return true, nil
}

// verifySolanaTransaction fetches Solana transaction once and verifies both owner and extracts payload hash
func (k Keeper) verifySolanaTransaction(ctx context.Context, ownerKey, txHash string, chainConfig uetypes.ChainConfig) (bool, string, error) {
	// Create RPC config using the same pattern as existing verification
	rpcCfg := rpc.RpcCallConfig{
		PrivateRPC: utils.GetEnvRPCOverride(chainConfig.Chain),
		PublicRPC:  chainConfig.PublicRpcUrl,
	}

	// Get transaction data (ONCE)
	transaction, err := svmrpc.SVMGetTransactionBySig(ctx, rpcCfg, txHash)
	if err != nil {
		return false, "", fmt.Errorf("failed to get transaction: %w", err)
	}

	// Verify transaction status
	if transaction.Meta.Err != nil {
		return false, "", fmt.Errorf("transaction failed with error: %v", transaction.Meta.Err)
	}

	// Check 1: Verify transaction target (gateway contract)
	gatewayContract := chainConfig.GatewayAddress

	// Check if any instruction calls the gateway contract
	foundGatewayCall := false
	for _, instruction := range transaction.Transaction.Message.Instructions {
		// Verify program ID index is valid
		if instruction.ProgramIDIndex < 0 || instruction.ProgramIDIndex >= len(transaction.Transaction.Message.AccountKeys) {
			return false, "", fmt.Errorf("invalid program ID index: %d", instruction.ProgramIDIndex)
		}

		programID := transaction.Transaction.Message.AccountKeys[instruction.ProgramIDIndex]

		if compareSolanaAddresses(programID, gatewayContract) {
			foundGatewayCall = true
			break
		}
	}

	if !foundGatewayCall {
		return false, "", fmt.Errorf("no instruction found calling the gateway contract %s", gatewayContract)
	}

	// Check 2: Verify owner (transaction signer)
	if len(transaction.Transaction.Message.AccountKeys) == 0 {
		return false, "", fmt.Errorf("no accounts found in transaction")
	}
	sender := transaction.Transaction.Message.AccountKeys[0]

	// Convert Solana address to hex format for comparison
	senderBytes := base58.Decode(sender)
	if senderBytes == nil {
		return false, "", fmt.Errorf("failed to decode Solana address: %s", sender)
	}
	senderHex := fmt.Sprintf("0x%x", senderBytes)

	if senderHex != ownerKey {
		return false, "", fmt.Errorf("transaction sender %s (hex: %s) does not match ownerKey %s", sender, senderHex, ownerKey)
	}

	// Check 3: Extract execution hash from transaction logs using proper event parsing
	if len(transaction.Meta.LogMessages) == 0 {
		return false, "", fmt.Errorf("no log messages found in transaction")
	}

	// Parse logs for FundsAdded event using the same sophisticated pattern as verify_tx_svm.go
	foundEvent := false
	var payloadHash string

	// Get the event discriminator from chain config (same as verify_tx_svm.go)
	var eventDiscriminator []byte
	for _, method := range chainConfig.GatewayMethods {
		if method.Name == uetypes.METHOD.SVM.AddFunds {
			eventDiscriminator, err = hex.DecodeString(method.EventIdentifier)
			if err != nil {
				return false, "", fmt.Errorf("invalid event discriminator in chain config: %w", err)
			}
			break
		}
	}
	if eventDiscriminator == nil {
		return false, "", fmt.Errorf("add_funds method not found in chain config")
	}

	for _, log := range transaction.Meta.LogMessages {
		// Look for "Program data:" logs - this is where Solana events are emitted
		if strings.HasPrefix(log, "Program data: ") {
			encoded := strings.TrimPrefix(log, "Program data: ")

			// Decode the base64 event data
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue // Skip invalid base64
			}

			// Need at least 100 bytes for complete event
			if len(raw) < 100 {
				continue
			}

			// ✅ PROPER: Check event discriminator (same as verify_tx_svm.go)
			fmt.Printf("[SVM] Event actual discriminator: %x\n", raw[:8])
			fmt.Printf("[SVM] Event expected discriminator: %x\n", eventDiscriminator)
			if !bytes.Equal(raw[:8], eventDiscriminator) {
				continue // Skip events that don't match our discriminator
			}

			// Parse the event structure based on actual Solana contract FundsAddedEvent:
			// [0:8]    - event discriminator (8 bytes)
			// [8:40]   - user: Pubkey (32 bytes)
			// [40:48]  - sol_amount: u64 (8 bytes)
			// [48:64]  - usd_equivalent: i128 (16 bytes)
			// [64:68]  - usd_exponent: i32 (4 bytes)
			// [68:100] - transaction_hash: [u8; 32] (32 bytes) <- This is what we want

			// Extract transaction hash from bytes 68-100 (32 bytes)
			hashBytes := raw[68:100]
			payloadHash = fmt.Sprintf("0x%x", hashBytes)

			fmt.Printf("[SVM] Found FundsAddedEvent, extracted transaction_hash: %s\n", payloadHash)
			foundEvent = true
			break
		}
	}

	if !foundEvent {
		return false, "", fmt.Errorf("FundsAdded event not found in transaction logs")
	}

	return true, payloadHash, nil
}

// verifyEVMTransaction fetches EVM transaction once and verifies both owner and extracts payload hash
func (k Keeper) verifyEVMTransaction(ctx context.Context, ownerKey, txHash string, chainConfig uetypes.ChainConfig) (bool, string, error) {
	// Create RPC config using the same pattern as existing verification
	rpcCfg := rpc.RpcCallConfig{
		PrivateRPC: utils.GetEnvRPCOverride(chainConfig.Chain),
		PublicRPC:  chainConfig.PublicRpcUrl,
	}

	// Get transaction receipt to access logs/events (ONCE)
	receipt, err := evmrpc.EVMGetTransactionReceipt(ctx, rpcCfg, txHash)
	if err != nil {
		return false, "", fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	// Also get transaction details for method validation (same as verify_tx_evm.go)
	tx, err := evmrpc.EVMGetTransactionByHash(ctx, rpcCfg, txHash)
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch transaction: %w", err)
	}

	// Check 1: Verify transaction target (gateway contract) - using proper normalization
	gatewayContract := chainConfig.GatewayAddress

	if receipt.To == "" {
		return false, "", fmt.Errorf("no recipient found in EVM transaction")
	}

	// Use proper address normalization (same as verify_tx_evm.go)
	normalizedTo := NormalizeAddress(receipt.To)
	normalizedGateway := NormalizeAddress(gatewayContract)

	if !didSendToGateway(normalizedTo, normalizedGateway) {
		return false, "", fmt.Errorf("transaction is not directed to gateway contract %s, got %s", gatewayContract, receipt.To)
	}

	// Check 2: Verify owner (transaction sender) - using proper normalization
	if receipt.From == "" {
		return false, "", fmt.Errorf("no sender found in EVM transaction")
	}

	normalizedFrom := NormalizeAddress(receipt.From)
	normalizedOwnerKey := NormalizeAddress(ownerKey)

	if normalizedFrom != normalizedOwnerKey {
		return false, "", fmt.Errorf("transaction sender %s does not match ownerKey %s", receipt.From, ownerKey)
	}

	// ✅ PROPER: Check if transaction is calling addFunds method (same as verify_tx_evm.go)
	ok, selector := isCallingAddFunds(tx.Input, chainConfig)
	if !ok {
		return false, "", fmt.Errorf("transaction is not calling addFunds, expected selector %s but got input %s", selector, tx.Input)
	}

	// Check 3: Extract execution hash from transaction logs
	if len(receipt.Logs) == 0 {
		return false, "", fmt.Errorf("no logs found in transaction")
	}

	// ✅ PROPER: Get event topic from chain config (same as verify_tx_evm.go)
	eventTopic := ""
	for _, method := range chainConfig.GatewayMethods {
		if method.Name == uetypes.METHOD.EVM.AddFunds {
			eventTopic = method.EventIdentifier
			break
		}
	}
	if eventTopic == "" {
		return false, "", fmt.Errorf("addFunds method not found in gateway methods")
	}

	// Look for the FundsAdded event in the logs using chain config event topic
	eventTopic = strings.ToLower(eventTopic)

	for _, log := range receipt.Logs {
		logMap, ok := log.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this log has the FundsAdded event signature as the first topic
		if topics, exists := logMap["topics"].([]interface{}); exists && len(topics) >= 3 {
			if firstTopic, ok := topics[0].(string); ok {
				if strings.ToLower(firstTopic) == eventTopic {
					// Found the FundsAdded event
					// The third topic (index 2) contains the transactionHash (execution hash)
					if transactionHashTopic, ok := topics[2].(string); ok {
						// Remove 0x prefix and return the execution hash
						transactionHashTopic = strings.TrimPrefix(transactionHashTopic, "0x")
						if len(transactionHashTopic) == 64 { // 32 bytes = 64 hex chars
							fmt.Printf("[EVM] Found FundsAddedEvent, extracted transaction_hash: 0x%s\n", transactionHashTopic)
							return true, "0x" + transactionHashTopic, nil
						}
					}
				}
			}
		}
	}

	return false, "", fmt.Errorf("FundsAdded event not found in transaction logs")
}
