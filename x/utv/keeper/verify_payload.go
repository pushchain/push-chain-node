package keeper

import (
	"context"
	"fmt"
	"strings"

	"encoding/base64"

	"github.com/decred/base58"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rollchains/pchain/utils/rpc"
	evmrpc "github.com/rollchains/pchain/utils/rpc/evm"
	svmrpc "github.com/rollchains/pchain/utils/rpc/svm"
	uetypes "github.com/rollchains/pchain/x/ue/types"
)

// VerifyTxHashWithPayload verifies a transaction hash against a provided payload hash using Universal Account ID
func (k Keeper) VerifyTxHashWithPayload(ctx context.Context, universalAccountId uetypes.UniversalAccountId, payloadHash, txHash string) (bool, error) {
	fmt.Printf("[UTV] 🔍 Starting verification process\n")
	fmt.Printf("[UTV] Universal Account ID: %+v\n", universalAccountId)
	fmt.Printf("[UTV] Payload hash: %s\n", payloadHash)
	fmt.Printf("[UTV] Transaction hash: %s\n", txHash)

	// Extract owner and chain from Universal Account ID
	ownerKey := universalAccountId.Owner
	chain := fmt.Sprintf("%s:%s", universalAccountId.ChainNamespace, universalAccountId.ChainId)

	// Get chain configuration
	chainConfig, err := k.getChainConfig(chain)
	if err != nil {
		return false, fmt.Errorf("failed to get chain config: %v", err)
	}

	// Verify owner and extract execution hash from transaction
	var executionHash string
	var isOwnerVerified bool
	if strings.Contains(chain, "solana") {
		verified, hash, err := k.verifySolanaTransaction(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return false, fmt.Errorf("solana verification failed: %v", err)
		}
		isOwnerVerified = verified
		executionHash = hash
	} else if strings.Contains(chain, "eip155") {
		verified, hash, err := k.verifyEVMTransaction(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return false, fmt.Errorf("ethereum verification failed: %v", err)
		}
		isOwnerVerified = verified
		executionHash = hash
	} else {
		return false, fmt.Errorf("unsupported chain: %s", chain)
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
func (k Keeper) verifySolanaTransaction(ctx context.Context, ownerKey, txHash string, chainConfig *ChainConfig) (bool, string, error) {
	// Create RPC config from chain config
	rpcConfig := makeRpcConfig(chainConfig)

	// Get transaction data (ONCE)
	transaction, err := svmrpc.SVMGetTransactionBySig(ctx, rpcConfig, txHash)
	if err != nil {
		return false, "", fmt.Errorf("failed to get transaction: %w", err)
	}

	// Check 1: Verify transaction target (gateway contract)
	gatewayContract := chainConfig.GatewayContract

	// Check if any instruction calls the gateway contract
	foundGatewayCall := false
	for _, instruction := range transaction.Transaction.Message.Instructions {
		// Verify program ID index is valid
		if instruction.ProgramIDIndex < 0 || instruction.ProgramIDIndex >= len(transaction.Transaction.Message.AccountKeys) {
			return false, "", fmt.Errorf("invalid program ID index: %d", instruction.ProgramIDIndex)
		}

		programID := transaction.Transaction.Message.AccountKeys[instruction.ProgramIDIndex]

		if strings.EqualFold(programID, gatewayContract) {
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

	for _, log := range transaction.Meta.LogMessages {
		// Look for "Program data:" logs - this is where Solana events are emitted
		if strings.HasPrefix(log, "Program data: ") {
			encoded := strings.TrimPrefix(log, "Program data: ")

			// Decode the base64 event data
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue // Skip invalid base64
			}

			// Need at least 8 bytes for event discriminator
			if len(raw) < 8 {
				continue
			}

			// Parse the event structure based on actual Solana contract FundsAddedEvent:
			// [0:8]    - event discriminator (8 bytes)
			// [8:40]   - user: Pubkey (32 bytes)
			// [40:48]  - sol_amount: u64 (8 bytes)
			// [48:64]  - usd_equivalent: i128 (16 bytes)
			// [64:68]  - usd_exponent: i32 (4 bytes)
			// [68:100] - transaction_hash: [u8; 32] (32 bytes) <- This is what we want

			// Ensure we have enough data for the complete event (8 + 32 + 8 + 16 + 4 + 32 = 100 bytes)
			if len(raw) >= 100 {
				// Extract transaction hash from bytes 68-100 (32 bytes)
				hashBytes := raw[68:100]
				payloadHash = fmt.Sprintf("0x%x", hashBytes)

				fmt.Printf("[SVM] Found FundsAddedEvent, extracted transaction_hash: %s\n", payloadHash)
				foundEvent = true
				break
			}
		}
	}

	if !foundEvent {
		return false, "", fmt.Errorf("FundsAdded event not found in transaction logs")
	}

	return true, payloadHash, nil
}

// verifyEVMTransaction fetches EVM transaction once and verifies both owner and extracts payload hash
func (k Keeper) verifyEVMTransaction(ctx context.Context, ownerKey, txHash string, chainConfig *ChainConfig) (bool, string, error) {
	// Create RPC config from chain config
	rpcConfig := makeRpcConfig(chainConfig)

	// Get transaction receipt to access logs/events (ONCE)
	receipt, err := evmrpc.EVMGetTransactionReceipt(ctx, rpcConfig, txHash)
	if err != nil {
		return false, "", fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	// Check 1: Verify transaction target (gateway contract)
	gatewayContract := chainConfig.GatewayContract

	if receipt.To == "" {
		return false, "", fmt.Errorf("no recipient found in EVM transaction")
	}

	// Use proper address normalization
	normalizedTo := NormalizeAddress(receipt.To)
	normalizedGateway := NormalizeAddress(gatewayContract)

	if normalizedTo != normalizedGateway {
		return false, "", fmt.Errorf("transaction is not directed to gateway contract %s, got %s", gatewayContract, receipt.To)
	}

	// Check 2: Verify owner (transaction sender)
	if receipt.From == "" {
		return false, "", fmt.Errorf("no sender found in EVM transaction")
	}

	// Use proper address normalization
	normalizedFrom := NormalizeAddress(receipt.From)
	normalizedOwnerKey := NormalizeAddress(ownerKey)

	if normalizedFrom != normalizedOwnerKey {
		return false, "", fmt.Errorf("transaction sender %s does not match ownerKey %s", receipt.From, ownerKey)
	}

	// Check 3: Extract execution hash from transaction logs
	if len(receipt.Logs) == 0 {
		return false, "", fmt.Errorf("no logs found in transaction")
	}

	// Look for the FundsAdded event in the logs
	fundsAddedEventSignature := crypto.Keccak256Hash([]byte("FundsAdded(address,bytes32,(uint256,uint8))")).Hex()

	for _, log := range receipt.Logs {
		logMap, ok := log.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this log has the FundsAdded event signature as the first topic
		if topics, exists := logMap["topics"].([]interface{}); exists && len(topics) >= 3 {
			if firstTopic, ok := topics[0].(string); ok {
				if firstTopic == fundsAddedEventSignature {
					// Found the FundsAdded event
					// The third topic (index 2) contains the transactionHash (execution hash)
					if transactionHashTopic, ok := topics[2].(string); ok {
						// Remove 0x prefix and return the execution hash
						transactionHashTopic = strings.TrimPrefix(transactionHashTopic, "0x")
						if len(transactionHashTopic) == 64 { // 32 bytes = 64 hex chars
							return true, "0x" + transactionHashTopic, nil
						}
					}
				}
			}
		}
	}

	return false, "", fmt.Errorf("FundsAdded event not found in transaction logs")
}

// ChainConfig holds configuration for a specific chain
type ChainConfig struct {
	RpcEndpoint     string
	GatewayContract string
}

// getChainConfig returns the chain configuration for the given chain
func (k Keeper) getChainConfig(chain string) (*ChainConfig, error) {
	if strings.Contains(chain, "solana") {
		return &ChainConfig{
			RpcEndpoint:     "https://api.devnet.solana.com",
			GatewayContract: "3zrWaMknHTRQpZSxY4BvQxw9TStSXiHcmcp3NMPTFkke",
		}, nil
	} else if strings.Contains(chain, "eip155") {
		return &ChainConfig{
			RpcEndpoint:     "https://eth-sepolia.public.blastapi.io",
			GatewayContract: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		}, nil
	} else {
		return nil, fmt.Errorf("unsupported chain: %s", chain)
	}
}

// makeRpcConfig creates an RPC config from chain config
func makeRpcConfig(chainConfig *ChainConfig) rpc.RpcCallConfig {
	return rpc.RpcCallConfig{
		PublicRPC: chainConfig.RpcEndpoint,
	}
}
