package rpc

import (
	"context"
	"fmt"

	rpc "github.com/rollchains/pchain/utils/rpc"
)

// EVMGetTransactionByHash fetches tx info
func EVMGetTransactionByHash(ctx context.Context, rpcURL, txHash string) (*Transaction, error) {
	client := rpc.GetClient()

	var result Transaction
	fmt.Println(rpcURL)
	err := client.Call(ctx, rpcURL, "eth_getTransactionByHash", []interface{}{txHash}, &result)
	if err != nil {
		fmt.Println("Error calling eth_getTransactionByHash:", err)
		return nil, fmt.Errorf("eth_getTransactionByHash failed: %w", err)
	}
	return &result, nil
}

// EVMGetTransactionReceipt fetches receipt + logs
func EVMGetTransactionReceipt(ctx context.Context, rpcURL, txHash string) (*TransactionReceipt, error) {
	client := rpc.GetClient()

	var result TransactionReceipt
	err := client.Call(ctx, rpcURL, "eth_getTransactionReceipt", []interface{}{txHash}, &result)
	if err != nil {
		fmt.Println("Error calling eth_getTransactionByHash:", err)
		return nil, fmt.Errorf("eth_getTransactionReceipt failed: %w", err)
	}
	return &result, nil
}

// EVMGetBlockByNumber fetches block details
func EVMGetBlockByNumber(ctx context.Context, rpcURL, blockNumber string, fullTx bool) (*Block, error) {
	client := rpc.GetClient()

	var result Block
	err := client.Call(ctx, rpcURL, "eth_getBlockByNumber", []interface{}{blockNumber, fullTx}, &result)
	if err != nil {
		return nil, fmt.Errorf("eth_getBlockByNumber failed: %w", err)
	}
	return &result, nil
}
