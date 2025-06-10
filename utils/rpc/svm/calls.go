package rpc

import (
	"context"
	"fmt"

	baserpc "github.com/rollchains/pchain/utils/rpc"
)

// GetTransaction fetches transaction details using getTransaction RPC method
func SolGetTransactionBySig(ctx context.Context, rpcURL, txHash string) (*Transaction, error) {
	var result Transaction
	err := baserpc.GetClient().Call(ctx, rpcURL, "getTransaction", []interface{}{txHash}, &result)
	if err != nil {
		return nil, fmt.Errorf("getTransaction failed: %w", err)
	}
	return &result, nil
}

// GetSlot fetches current slot using getSlot RPC method
func SolGetCurrentSlot(ctx context.Context, rpcURL string) (uint64, error) {
	var result Slot
	err := baserpc.GetClient().Call(ctx, rpcURL, "getSlot", nil, &result)
	if err != nil {
		return 0, fmt.Errorf("getSlot failed: %w", err)
	}
	return uint64(result), nil
}
