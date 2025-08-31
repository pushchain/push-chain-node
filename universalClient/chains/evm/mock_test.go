package evm

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/mock"
)

// mockEthClient is a mock implementation of the Ethereum client for testing
type mockEthClient struct {
	mock.Mock
}

func (m *mockEthClient) BlockNumber(ctx context.Context) (uint64, error) {
	args := m.Called(ctx)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockEthClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	args := m.Called(ctx, q)
	if logs := args.Get(0); logs != nil {
		return logs.([]types.Log), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockEthClient) ChainID(ctx context.Context) (*big.Int, error) {
	args := m.Called(ctx)
	if id := args.Get(0); id != nil {
		return id.(*big.Int), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockEthClient) Close() {
	m.Called()
}

func (m *mockEthClient) TransactionReceipt(ctx context.Context, txHash ethcommon.Hash) (*types.Receipt, error) {
	args := m.Called(ctx, txHash)
	if receipt := args.Get(0); receipt != nil {
		return receipt.(*types.Receipt), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockEthClient) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	args := m.Called(ctx, ch)
	if sub := args.Get(0); sub != nil {
		return sub.(ethereum.Subscription), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockEthClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	args := m.Called(ctx, number)
	if header := args.Get(0); header != nil {
		return header.(*types.Header), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockEthClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := m.Called(ctx, number)
	if block := args.Get(0); block != nil {
		return block.(*types.Block), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockEthClient) TransactionByHash(ctx context.Context, hash ethcommon.Hash) (*types.Transaction, *bool, error) {
	args := m.Called(ctx, hash)
	if tx := args.Get(0); tx != nil {
		if isPending := args.Get(1); isPending != nil {
			pending := isPending.(*bool)
			return tx.(*types.Transaction), pending, args.Error(2)
		}
		return tx.(*types.Transaction), nil, args.Error(2)
	}
	return nil, nil, args.Error(2)
}