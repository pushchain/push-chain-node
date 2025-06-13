package rpc

import (
	"context"
	"fmt"

	rpc "github.com/rollchains/pchain/utils/rpc"
)

// GetTransaction fetches transaction details using getTransaction RPC method
func SVMGetTransactionBySig(ctx context.Context, cfg rpc.RpcCallConfig, txHash string) (*Transaction, error) {
	client := rpc.GetClient()

	var result Transaction
	var err error
	if cfg.PrivateRPC != "" {
		err = client.CallWithFallback(ctx, cfg.PrivateRPC, cfg.PublicRPC, "getTransaction", []interface{}{txHash}, &result)
	} else {
		fmt.Println("Error calling getTransaction:", err)
		err = client.Call(ctx, cfg.PublicRPC, "getTransaction", []interface{}{txHash}, &result)
	}

	if err != nil {
		return nil, fmt.Errorf("getTransaction failed: %w", err)
	}
	return &result, nil
}

// GetSlot fetches current slot using getSlot RPC method
func SVMGetCurrentSlot(ctx context.Context, cfg rpc.RpcCallConfig) (uint64, error) {
	client := rpc.GetClient()

	var result Slot
	var err error
	if cfg.PrivateRPC != "" {
		err = client.CallWithFallback(ctx, cfg.PrivateRPC, cfg.PublicRPC, "getSlot", nil, &result)
	} else {
		fmt.Println("Error calling getSlot:", err)
		err = client.Call(ctx, cfg.PublicRPC, "getSlot", nil, &result)
	}

	if err != nil {
		return 0, fmt.Errorf("getSlot failed: %w", err)
	}
	return uint64(result), nil
}
