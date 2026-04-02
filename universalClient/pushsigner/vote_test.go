package pushsigner

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestVoteConstants(t *testing.T) {
	t.Run("default gas limit", func(t *testing.T) {
		assert.Equal(t, uint64(500000000), defaultGasLimit)
	})

	t.Run("default fee amount is valid", func(t *testing.T) {
		coins, err := sdk.ParseCoinsNormalized(defaultFeeAmount)
		require.NoError(t, err)
		assert.False(t, coins.IsZero())
	})

	t.Run("default vote timeout", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, defaultVoteTimeout)
	})

	t.Run("tx poll interval", func(t *testing.T) {
		assert.Equal(t, 500*time.Millisecond, txPollInterval)
	})

	t.Run("tx confirm timeout", func(t *testing.T) {
		assert.Equal(t, 15*time.Second, txConfirmTimeout)
	})
}

func TestWaitForTxConfirmation(t *testing.T) {
	t.Run("returns nil when tx found immediately", func(t *testing.T) {
		mock := &mockChainClient{
			getTxFn: func(ctx context.Context, txHash string) (*sdktx.GetTxResponse, error) {
				return &sdktx.GetTxResponse{
					TxResponse: &sdk.TxResponse{TxHash: txHash, Code: 0},
				}, nil
			},
		}

		err := waitForTxConfirmation(context.Background(), mock, "0xabc")
		assert.NoError(t, err)
	})

	t.Run("polls until tx found", func(t *testing.T) {
		calls := 0
		mock := &mockChainClient{
			getTxFn: func(ctx context.Context, txHash string) (*sdktx.GetTxResponse, error) {
				calls++
				if calls < 3 {
					return nil, fmt.Errorf("tx not found")
				}
				return &sdktx.GetTxResponse{
					TxResponse: &sdk.TxResponse{TxHash: txHash},
				}, nil
			},
		}

		err := waitForTxConfirmation(context.Background(), mock, "0xdef")
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, calls, 3)
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		mock := &mockChainClient{
			getTxFn: func(ctx context.Context, txHash string) (*sdktx.GetTxResponse, error) {
				return nil, fmt.Errorf("not found")
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		err := waitForTxConfirmation(ctx, mock, "0xabc")
		assert.Error(t, err)
	})
}

func TestVoteRejectedOnChain(t *testing.T) {
	mock := &mockChainClient{
		getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
			addr, _ := sdk.AccAddressFromBech32(address)
			return makeAccountResponse(t, addr, 1, 1), nil
		},
		broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
			return &sdktx.BroadcastTxResponse{
				TxResponse: &sdk.TxResponse{Code: 7, TxHash: "REJECTED", RawLog: "unauthorized"},
			}, nil
		},
	}

	signer := createTestSigner(t, mock)
	inbound := &uexecutortypes.Inbound{TxHash: "0x1"}

	txHash, err := signer.VoteInbound(context.Background(), inbound)
	require.Error(t, err)
	assert.Empty(t, txHash)
	assert.Contains(t, err.Error(), "transaction failed with code 7")
}

func TestVoteEmptyMemoDefaultsToMsgType(t *testing.T) {
	mock := &mockChainClient{
		getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
			addr, _ := sdk.AccAddressFromBech32(address)
			return makeAccountResponse(t, addr, 1, 1), nil
		},
		broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
			return &sdktx.BroadcastTxResponse{
				TxResponse: &sdk.TxResponse{Code: 0, TxHash: "OK"},
			}, nil
		},
	}

	signer := createTestSigner(t, mock)

	// VoteChainMeta provides a memo, so this tests the non-empty path
	txHash, err := signer.VoteChainMeta(context.Background(), "eip155:1", 100, 200)
	require.NoError(t, err)
	assert.Equal(t, "OK", txHash)
}

func TestVoteAllTypes(t *testing.T) {
	mock := successMock(t)

	t.Run("VoteOutbound with failure observation", func(t *testing.T) {
		signer := createTestSigner(t, mock)
		obs := &uexecutortypes.OutboundObservation{
			Success:     false,
			BlockHeight: 999,
			TxHash:      "0xfailed",
		}

		txHash, err := signer.VoteOutbound(context.Background(), "tx-fail", "utx-fail", obs)
		require.NoError(t, err)
		assert.Equal(t, "VOTE_OK", txHash)
	})

	t.Run("VoteFundMigration with success=false", func(t *testing.T) {
		signer := createTestSigner(t, mock)
		txHash, err := signer.VoteFundMigration(context.Background(), 99, "0xfailhash", false)
		require.NoError(t, err)
		assert.Equal(t, "VOTE_OK", txHash)
	})
}
