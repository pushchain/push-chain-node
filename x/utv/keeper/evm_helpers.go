// evm_helpers.go
// EVM-specific helper functions used in inbound transaction verification.
package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/rollchains/pchain/utils/rpc"
	evmrpc "github.com/rollchains/pchain/utils/rpc/evm"
	uetypes "github.com/rollchains/pchain/x/ue/types"
	utvtypes "github.com/rollchains/pchain/x/utv/types"
)

// isValidEVMGateway checks if tx.To equals gateway address
func isValidEVMGateway(toAddress, gatewayAddress string) bool {
	return NormalizeEVMAddress(toAddress) == NormalizeEVMAddress(gatewayAddress)
}

// isValidEVMOwner checks if tx.From equals owner address
func isValidEVMOwner(actualOwner, expectedOwner string) bool {
	return NormalizeEVMAddress(actualOwner) == NormalizeEVMAddress(expectedOwner)
}

// isEVMTxCallingAddFunds checks if the txInput is of a addFunds fn
func isEVMTxCallingAddFunds(txInput string, chainConfig uetypes.ChainConfig) (bool, string) {
	for _, method := range chainConfig.GatewayMethods {
		if method.Name == uetypes.METHOD.EVM.AddFunds {
			selector := method.Identifier
			if strings.HasPrefix(txInput, selector) {
				return true, selector
			}
			return false, selector
		}
	}
	return false, ""
}

// NormalizeEVMAddress returns a lowercase hex address without 0x prefix
func NormalizeEVMAddress(addr string) string {
	return strings.ToLower(addr)
}

// @notice Parses EVM logs to extract amount, decimals and payloadHash from the `FundsAdded` event.
// @dev This function scans logs for a specific event topic and decodes the `amountInUSD`, `payloadHash` fields.
// @param logs The array of raw EVM logs from the transaction receipt.
// @param expectedTopic The hash of the `FundsAdded` event signature (topic[0]) to match.
// @return amount The extracted amount in USD as a big.Int.
// @return decimals The number of decimals used for the USD amount.
// @return error An error if the event was not found or decoding failed.
//
// Emits:
// event FundsAdded(
//
//	address indexed user,
//	bytes32 indexed payloadHash,
//	AmountInUSD AmountInUSD
//
// );
func ParseEVMFundsAddedEventLogs(logs []interface{}, expectedTopic string) (*utvtypes.EVMFundsAddedEventData, error) {
	expectedTopic = strings.ToLower(expectedTopic)

	for _, rawLog := range logs {
		logMap, ok := rawLog.(map[string]interface{})
		if !ok {
			continue
		}

		// Match the expected event topic
		topics, ok := logMap["topics"].([]interface{})
		if !ok || len(topics) < 3 {
			continue
		}

		topic0, ok := topics[0].(string)
		if !ok || strings.ToLower(topic0) != expectedTopic {
			continue
		}

		// topic[2] is payloadHash (indexed bytes32)
		payloadHashHex, ok := topics[2].(string)
		if !ok {
			return nil, fmt.Errorf("invalid payloadHash format")
		}

		// Get data and decode
		dataHex, ok := logMap["data"].(string)
		if !ok || !strings.HasPrefix(dataHex, "0x") {
			continue
		}

		dataBytes, err := hex.DecodeString(dataHex[2:])
		if err != nil || len(dataBytes) < 64 {
			return nil, fmt.Errorf("error decoding log data: %w", err)
		}

		// First 32 bytes: amountInUSD
		amount := new(big.Int).SetBytes(dataBytes[:32])

		// Second 32 bytes: decimals (last byte)
		decimals := uint32(uint8(dataBytes[63]))

		return &utvtypes.EVMFundsAddedEventData{
			AmountInUSD: amount,
			Decimals:    decimals,
			PayloadHash: payloadHashHex,
		}, nil
	}

	return nil, fmt.Errorf("amount not found with expected topic %s", expectedTopic)
}

// Checks if a given evm tx hash has enough confirmations
func CheckEVMBlockConfirmations(
	ctx context.Context,
	txHash string,
	rpcCfg rpc.RpcCallConfig,
	requiredConfirmations uint64,
) error {
	// Fetch transaction receipt
	receipt, err := evmrpc.EVMGetTransactionReceipt(ctx, rpcCfg, txHash)
	if err != nil {
		return fmt.Errorf("fetch receipt failed: %w", err)
	}

	txBlockNum, ok := new(big.Int).SetString(receipt.BlockNumber[2:], 16) // remove "0x"
	if !ok {
		return fmt.Errorf("invalid block number in receipt: %s", receipt.BlockNumber)
	}

	latestBlock, err := evmrpc.EVMGetBlockByNumber(ctx, rpcCfg, "latest", false)
	if err != nil {
		return fmt.Errorf("failed to fetch latest block: %w", err)
	}

	latestBlockNum, ok := new(big.Int).SetString(latestBlock.Number[2:], 16)
	if !ok {
		return fmt.Errorf("invalid latest block number: %s", latestBlock.Number)
	}

	confirmations := new(big.Int).Sub(latestBlockNum, txBlockNum)
	required := big.NewInt(int64(requiredConfirmations))

	if confirmations.Cmp(required) < 0 {
		return fmt.Errorf("insufficient confirmations: got %s, need %d", confirmations.String(), requiredConfirmations)
	}

	return nil
}
