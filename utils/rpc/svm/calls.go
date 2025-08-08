package rpc

import (
	"context"
	"fmt"

	rpc "github.com/pushchain/push-chain-node/utils/rpc"
)

// GetTransaction fetches transaction details using getTransaction RPC method
func SVMGetTransactionBySig(ctx context.Context, cfg rpc.RpcCallConfig, txHash string) (*Transaction, error) {
	client := rpc.GetClient()

	var result Transaction
	params := []interface{}{
		txHash,
		map[string]interface{}{
			"commitment":                     "confirmed",
			"maxSupportedTransactionVersion": 0,
			"encoding":                       "json",
		},
	}

	var err error
	if cfg.PrivateRPC != "" {
		err = client.CallWithFallback(ctx, cfg.PrivateRPC, cfg.PublicRPC, "getTransaction", params, &result)
	} else {
		err = client.Call(ctx, cfg.PublicRPC, "getTransaction", params, &result)
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
		err = client.Call(ctx, cfg.PublicRPC, "getSlot", nil, &result)
	}

	if err != nil {
		return 0, fmt.Errorf("getSlot failed: %w", err)
	}
	return uint64(result), nil
}
