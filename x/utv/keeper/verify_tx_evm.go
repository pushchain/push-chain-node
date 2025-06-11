package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	evmrpc "github.com/rollchains/pchain/utils/rpc/evm"
	"github.com/rollchains/pchain/x/ue/types"
)

// verifyEVMInteraction verifies user interacted with locker by checking tx sent by ownerKey to locker contract
func (k Keeper) verifyEVMInteraction(ctx context.Context, ownerKey, txHash string, chainConfig types.ChainConfig) error {
	rpcURL := chainConfig.PublicRpcUrl

	tx, err := evmrpc.EVMGetTransactionByHash(ctx, rpcURL, txHash)
	if err != nil {
		return fmt.Errorf("failed to fetch transaction: %w", err)
	}

	// Normalize addresses for comparison
	from := NormalizeAddress(tx.From)
	expectedFrom := NormalizeAddress(ownerKey)
	expectedTo := NormalizeAddress(chainConfig.LockerContractAddress)

	// Check if tx.From matches ownerKey
	if from != expectedFrom {
		return fmt.Errorf("transaction sender %s does not match ownerKey %s", tx.From, expectedFrom)
	}

	// Check if tx.To matches locker contract address
	if !didSendToLocker(tx, expectedTo) {
		return fmt.Errorf("transaction recipient %s is not locker contract %s", tx.To, expectedTo)
	}

	return nil
}

// Verifies and extracts locked amount (used in mint)
func (k Keeper) verifyEVMAndGetFunds(ctx context.Context, ownerKey, txHash string, chainConfig types.ChainConfig) (big.Int, uint32, error) {
	rpcURL := chainConfig.PublicRpcUrl

	// Step 1: Fetch transaction receipt
	receipt, err := evmrpc.EVMGetTransactionReceipt(ctx, rpcURL, txHash)
	if err != nil {
		return *big.NewInt(0), 0, fmt.Errorf("fetch receipt failed: %w", err)
	}

	// Normalize addresses for comparison
	from := NormalizeAddress(receipt.From)
	to := NormalizeAddress(receipt.To)
	expectedFrom := NormalizeAddress(ownerKey)
	expectedTo := NormalizeAddress(chainConfig.LockerContractAddress)

	if from != expectedFrom || to != expectedTo {
		return *big.NewInt(0), 0, fmt.Errorf("tx not sent from %s to locker %s", receipt.From, chainConfig.LockerContractAddress)
	}

	txBlockNum, ok := new(big.Int).SetString(receipt.BlockNumber[2:], 16) // remove "0x"
	if !ok {
		return *big.NewInt(0), 0, fmt.Errorf("invalid block number in receipt: %s", receipt.BlockNumber)
	}

	// Get latest block number
	latestBlock, err := evmrpc.EVMGetBlockByNumber(ctx, rpcURL, "latest", false)
	if err != nil {
		return *big.NewInt(0), 0, fmt.Errorf("fetch latest block failed: %w", err)
	}
	latestBlockNum, ok := new(big.Int).SetString(latestBlock.Number[2:], 16)
	if !ok {
		return *big.NewInt(0), 0, fmt.Errorf("invalid block number in latest block: %s", latestBlock.Number)
	}

	confirmations := new(big.Int).Sub(latestBlockNum, txBlockNum)
	required := big.NewInt(int64(chainConfig.BlockConfirmation))
	if confirmations.Cmp(required) < 0 {
		return *big.NewInt(0), 0, fmt.Errorf("insufficient confirmations: got %s, need %d", confirmations.String(), chainConfig.BlockConfirmation)
	}

	// Step 3: Extract amount from logs
	eventTopic := ""
	for _, method := range chainConfig.GatewayMethods {
		if method.Name == "add_funds" {
			eventTopic = method.EventTopic
			break
		}
	}
	if eventTopic == "" {
		return *big.NewInt(0), fmt.Errorf("add_funds method not found in gateway methods")
	}
	amount, decimals, err := extractAmountFromLogs(receipt.Logs, eventTopic)
	if err != nil {
		return *big.NewInt(0), 0, fmt.Errorf("amount extract failed: %w", err)
	}

	return amount, decimals, nil
}

// didSendToLocker checks if tx.To equals locker contract address
func didSendToLocker(tx *evmrpc.Transaction, lockerAddress string) bool {
	return NormalizeAddress(tx.To) == lockerAddress
}

// NormalizeAddress returns a lowercase hex address without 0x prefix
func NormalizeAddress(addr string) string {
	return strings.ToLower(addr)
}

// extractAmountFromLogs parses logs to extract the locked amount using the given event topic
func extractAmountFromLogs(logs []interface{}, expectedTopic string) (big.Int, uint32, error) {
	expectedTopic = strings.ToLower(expectedTopic)

	for _, rawLog := range logs {
		logMap, ok := rawLog.(map[string]interface{})
		if !ok {
			continue
		}

		// Match the expected event topic
		topics, ok := logMap["topics"].([]interface{})
		if !ok || len(topics) == 0 {
			continue
		}

		topic0, ok := topics[0].(string)
		if !ok || strings.ToLower(topic0) != expectedTopic {
			continue
		}

		// Get data and decode
		dataHex, ok := logMap["data"].(string)
		if !ok || !strings.HasPrefix(dataHex, "0x") {
			continue
		}

		dataBytes, err := hex.DecodeString(dataHex[2:])
		if err != nil || len(dataBytes) < 64 {
			return *big.NewInt(0), 0, err
		}

		// First 32 bytes: amountInUSD
		amount := new(big.Int).SetBytes(dataBytes[:32])

		// Second 32 bytes: decimals (only last byte relevant)
		decimals := uint32(uint8(dataBytes[63]))

		return *amount, decimals, nil
	}

	return *big.NewInt(0), 0, fmt.Errorf("amount not found with expected topic %s", expectedTopic)
}
