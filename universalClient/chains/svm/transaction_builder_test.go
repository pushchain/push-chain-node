package svm

import (
	"context"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock RPC client for transaction builder tests
type mockTxBuilderClient struct {
	mock.Mock
}

func (m *mockTxBuilderClient) GetRecentBlockhash(ctx context.Context, commitment rpc.CommitmentType) (*rpc.GetRecentBlockhashResult, error) {
	args := m.Called(ctx, commitment)
	if result := args.Get(0); result != nil {
		return result.(*rpc.GetRecentBlockhashResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockTxBuilderClient) GetMinimumBalanceForRentExemption(ctx context.Context, dataSize uint64, commitment rpc.CommitmentType) (uint64, error) {
	args := m.Called(ctx, dataSize, commitment)
	return args.Get(0).(uint64), args.Error(1)
}

func TestNewTransactionBuilder(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	client := &Client{}
	
	builder := NewTransactionBuilder(client, gatewayAddr, logger)
	
	assert.NotNil(t, builder)
	assert.Equal(t, client, builder.parentClient)
	assert.Equal(t, gatewayAddr, builder.gatewayAddr)
}
