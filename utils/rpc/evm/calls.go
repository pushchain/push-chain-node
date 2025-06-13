package rpc

import (
	"context"
	"fmt"

	rpc "github.com/rollchains/pchain/utils/rpc"
)

// EVMGetTransactionByHash fetches tx info
func EVMGetTransactionByHash(ctx context.Context, cfg rpc.RpcCallConfig, txHash string) (*Transaction, error) {
	client := rpc.GetClient()

	var result Transaction
	var err error
	if cfg.PrivateRPC != "" {
		err = client.CallWithFallback(ctx, cfg.PrivateRPC, cfg.PublicRPC, "eth_getTransactionByHash", []interface{}{txHash}, &result)
	} else {
		fmt.Println("Error calling eth_getTransactionByHash:", err)
		err = client.Call(ctx, cfg.PublicRPC, "eth_getTransactionByHash", []interface{}{txHash}, &result)
	}

	if err != nil {
		return nil, fmt.Errorf("eth_getTransactionByHash failed: %w", err)
	}
	return &result, nil
}

// EVMGetTransactionReceipt fetches receipt + logs
func EVMGetTransactionReceipt(ctx context.Context, cfg rpc.RpcCallConfig, txHash string) (*TransactionReceipt, error) {
	client := rpc.GetClient()

	var result TransactionReceipt
	var err error
	if cfg.PrivateRPC != "" {
		err = client.CallWithFallback(ctx, cfg.PrivateRPC, cfg.PublicRPC, "eth_getTransactionReceipt", []interface{}{txHash}, &result)
	} else {
		fmt.Println("Error calling eth_getTransactionReceipt:", err)
		err = client.Call(ctx, cfg.PublicRPC, "eth_getTransactionReceipt", []interface{}{txHash}, &result)
	}

	if err != nil {
		fmt.Println("Error calling eth_getTransactionByHash:", err)
		return nil, fmt.Errorf("eth_getTransactionReceipt failed: %w", err)
	}
	return &result, nil
}

// EVMGetBlockByNumber fetches block details
func EVMGetBlockByNumber(ctx context.Context, cfg rpc.RpcCallConfig, blockNumber string, fullTx bool) (*Block, error) {
	client := rpc.GetClient()

	var result Block
	var err error
	if cfg.PrivateRPC != "" {
		err = client.CallWithFallback(ctx, cfg.PrivateRPC, cfg.PublicRPC, "eth_getBlockByNumber", []interface{}{blockNumber, fullTx}, &result)
	} else {
		fmt.Println("Error calling eth_getBlockByNumber:", err)
		err = client.Call(ctx, cfg.PublicRPC, "eth_getBlockByNumber", []interface{}{blockNumber, fullTx}, &result)
	}

	if err != nil {
		return nil, fmt.Errorf("eth_getBlockByNumber failed: %w", err)
	}
	return &result, nil
}
