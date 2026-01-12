package vote

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// MockTxSigner is a mock implementation of TxSigner
type MockTxSigner struct {
	mock.Mock
}

func (m *MockTxSigner) SignAndBroadcastAuthZTx(
	ctx context.Context,
	msgs []sdk.Msg,
	memo string,
	gasLimit uint64,
	feeAmount sdk.Coins,
) (*sdk.TxResponse, error) {
	args := m.Called(ctx, msgs, memo, gasLimit, feeAmount)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sdk.TxResponse), args.Error(1)
}

func TestNewHandler(t *testing.T) {
	mockSigner := &MockTxSigner{}
	logger := zerolog.Nop()
	granter := "push1test123"

	handler := NewHandler(mockSigner, logger, granter)

	assert.NotNil(t, handler)
	assert.Equal(t, mockSigner, handler.txSigner)
	assert.Equal(t, granter, handler.granter)
}

func TestHandler_validateHandler(t *testing.T) {
	tests := []struct {
		name      string
		txSigner  TxSigner
		granter   string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid handler",
			txSigner:  &MockTxSigner{},
			granter:   "push1test123",
			wantError: false,
		},
		{
			name:      "nil txSigner",
			txSigner:  nil,
			granter:   "push1test123",
			wantError: true,
			errorMsg:  "txSigner is nil",
		},
		{
			name:      "empty granter",
			txSigner:  &MockTxSigner{},
			granter:   "",
			wantError: true,
			errorMsg:  "granter address is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &Handler{
				txSigner: tt.txSigner,
				granter:  tt.granter,
				log:      zerolog.Nop(),
			}

			err := handler.validateHandler()
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHandler_prepareTxParams(t *testing.T) {
	handler := &Handler{
		log: zerolog.Nop(),
	}

	gasLimit, feeAmount, err := handler.prepareTxParams()

	require.NoError(t, err)
	assert.Equal(t, defaultGasLimit, gasLimit)
	assert.NotNil(t, feeAmount)
	assert.Equal(t, "1000000000000000000upc", feeAmount.String())
}

func TestHandler_VoteTssKeyProcess_Success(t *testing.T) {
	mockSigner := &MockTxSigner{}
	logger := zerolog.Nop()
	granter := "push1test123"
	handler := NewHandler(mockSigner, logger, granter)

	tssPubKey := "0x1234567890abcdef"
	keyID := "key-123"
	processId := uint64(42)
	expectedTxHash := "0xabcdef123456"

	// Setup mock response
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{
			TxHash:  expectedTxHash,
			Code:    0,
			GasUsed: 100000,
		}, nil)

	txHash, err := handler.VoteTssKeyProcess(context.Background(), tssPubKey, keyID, processId)

	require.NoError(t, err)
	assert.Equal(t, expectedTxHash, txHash)
	mockSigner.AssertExpectations(t)
}

func TestHandler_VoteTssKeyProcess_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		txSigner  TxSigner
		granter   string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "nil txSigner",
			txSigner:  nil,
			granter:   "push1test123",
			wantError: true,
			errorMsg:  "txSigner is nil",
		},
		{
			name:      "empty granter",
			txSigner:  &MockTxSigner{},
			granter:   "",
			wantError: true,
			errorMsg:  "granter address is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(tt.txSigner, zerolog.Nop(), tt.granter)

			_, err := handler.VoteTssKeyProcess(context.Background(), "0x123", "key-123", 1)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

func TestHandler_VoteTssKeyProcess_BroadcastError(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	broadcastErr := errors.New("network error")
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, broadcastErr)

	_, err := handler.VoteTssKeyProcess(context.Background(), "0x123", "key-123", 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to broadcast TSS vote transaction")
	assert.Contains(t, err.Error(), "network error")
}

func TestHandler_VoteTssKeyProcess_TransactionRejected(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{
			TxHash: "0xrejected",
			Code:   5,
			RawLog: "insufficient funds",
		}, nil)

	_, err := handler.VoteTssKeyProcess(context.Background(), "0x123", "key-123", 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "TSS vote transaction failed with code 5")
	assert.Contains(t, err.Error(), "insufficient funds")
}

func TestHandler_VoteOutbound_Success(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	txID := "0xtx123"
	txHash := "0xexternal123"
	blockHeight := uint64(1000)
	expectedVoteTxHash := "0xvote123"

	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{
			TxHash:  expectedVoteTxHash,
			Code:    0,
			GasUsed: 200000,
		}, nil)

	voteTxHash, err := handler.VoteOutbound(context.Background(), txID, true, txHash, blockHeight, "")

	require.NoError(t, err)
	assert.Equal(t, expectedVoteTxHash, voteTxHash)

	// Verify the message was created correctly
	calls := mockSigner.Calls
	require.Len(t, calls, 1)
	msgs := calls[0].Arguments[1].([]sdk.Msg)
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(*uexecutortypes.MsgVoteOutbound)
	require.True(t, ok)
	assert.Equal(t, txID, msg.TxId)
	assert.True(t, msg.ObservedTx.Success)
	assert.Equal(t, txHash, msg.ObservedTx.TxHash)
	assert.Equal(t, blockHeight, msg.ObservedTx.BlockHeight)
}

func TestHandler_VoteOutbound_Revert(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	txID := "0xtx123"
	reason := "transaction reverted"
	expectedVoteTxHash := "0xvote456"

	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{
			TxHash:  expectedVoteTxHash,
			Code:    0,
			GasUsed: 200000,
		}, nil)

	voteTxHash, err := handler.VoteOutbound(context.Background(), txID, false, "", 0, reason)

	require.NoError(t, err)
	assert.Equal(t, expectedVoteTxHash, voteTxHash)

	// Verify the message was created correctly
	calls := mockSigner.Calls
	require.Len(t, calls, 1)
	msgs := calls[0].Arguments[1].([]sdk.Msg)
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(*uexecutortypes.MsgVoteOutbound)
	require.True(t, ok)
	assert.Equal(t, txID, msg.TxId)
	assert.False(t, msg.ObservedTx.Success)
	assert.Equal(t, reason, msg.ObservedTx.ErrorMsg)
}

func TestHandler_VoteOutbound_RevertWithTxHash(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	txID := "0xtx123"
	txHash := "0xexternal456"
	blockHeight := uint64(2000)
	reason := "transaction reverted on chain"
	expectedVoteTxHash := "0xvote789"

	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{
			TxHash:  expectedVoteTxHash,
			Code:    0,
			GasUsed: 200000,
		}, nil)

	voteTxHash, err := handler.VoteOutbound(context.Background(), txID, false, txHash, blockHeight, reason)

	require.NoError(t, err)
	assert.Equal(t, expectedVoteTxHash, voteTxHash)

	// Verify the message was created correctly
	calls := mockSigner.Calls
	require.Len(t, calls, 1)
	msgs := calls[0].Arguments[1].([]sdk.Msg)
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(*uexecutortypes.MsgVoteOutbound)
	require.True(t, ok)
	assert.False(t, msg.ObservedTx.Success)
	assert.Equal(t, txHash, msg.ObservedTx.TxHash)
	assert.Equal(t, blockHeight, msg.ObservedTx.BlockHeight)
	assert.Equal(t, reason, msg.ObservedTx.ErrorMsg)
}

func TestHandler_VoteOutbound_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		txSigner    TxSigner
		granter     string
		txID        string
		isSuccess   bool
		txHash      string
		blockHeight uint64
		reason      string
		wantError   bool
		errorMsg    string
	}{
		{
			name:        "nil txSigner",
			txSigner:    nil,
			granter:     "push1test123",
			txID:        "0xtx123",
			isSuccess:   true,
			txHash:      "0xhash",
			blockHeight: 1000,
			wantError:   true,
			errorMsg:    "txSigner is nil",
		},
		{
			name:        "empty granter",
			txSigner:    &MockTxSigner{},
			granter:     "",
			txID:        "0xtx123",
			isSuccess:   true,
			txHash:      "0xhash",
			blockHeight: 1000,
			wantError:   true,
			errorMsg:    "granter address is empty",
		},
		{
			name:        "empty txID",
			txSigner:    &MockTxSigner{},
			granter:     "push1test123",
			txID:        "",
			isSuccess:   true,
			txHash:      "0xhash",
			blockHeight: 1000,
			wantError:   true,
			errorMsg:    "txID cannot be empty",
		},
		{
			name:        "success vote - empty txHash",
			txSigner:    &MockTxSigner{},
			granter:     "push1test123",
			txID:        "0xtx123",
			isSuccess:   true,
			txHash:      "",
			blockHeight: 1000,
			wantError:   true,
			errorMsg:    "txHash cannot be empty for success vote",
		},
		{
			name:        "success vote - zero blockHeight",
			txSigner:    &MockTxSigner{},
			granter:     "push1test123",
			txID:        "0xtx123",
			isSuccess:   true,
			txHash:      "0xhash",
			blockHeight: 0,
			wantError:   true,
			errorMsg:    "blockHeight must be > 0 for success vote",
		},
		{
			name:        "revert vote - empty reason",
			txSigner:    &MockTxSigner{},
			granter:     "push1test123",
			txID:        "0xtx123",
			isSuccess:   false,
			txHash:      "",
			blockHeight: 0,
			reason:      "",
			wantError:   true,
			errorMsg:    "reason cannot be empty for revert vote",
		},
		{
			name:        "revert vote - txHash provided but zero blockHeight",
			txSigner:    &MockTxSigner{},
			granter:     "push1test123",
			txID:        "0xtx123",
			isSuccess:   false,
			txHash:      "0xhash",
			blockHeight: 0,
			reason:      "some reason",
			wantError:   true,
			errorMsg:    "blockHeight must be > 0 when txHash is provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(tt.txSigner, zerolog.Nop(), tt.granter)

			_, err := handler.VoteOutbound(context.Background(), tt.txID, tt.isSuccess, tt.txHash, tt.blockHeight, tt.reason)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

func TestHandler_VoteOutbound_BroadcastError(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	broadcastErr := errors.New("broadcast failed")
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, broadcastErr)

	_, err := handler.VoteOutbound(context.Background(), "0xtx123", true, "0xhash", 1000, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to broadcast outbound vote transaction")
	assert.Contains(t, err.Error(), "broadcast failed")
}

func TestHandler_VoteOutbound_TransactionRejected(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{
			TxHash: "0xrejected",
			Code:   10,
			RawLog: "invalid signature",
		}, nil)

	_, err := handler.VoteOutbound(context.Background(), "0xtx123", true, "0xhash", 1000, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "outbound vote transaction failed with code 10")
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestHandler_broadcastVoteTx_ContextTimeout(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msgs := []sdk.Msg{&utsstypes.MsgVoteTssKeyProcess{}}
	logFields := map[string]interface{}{"test": "value"}

	// Mock should not be called because context is cancelled
	// But we'll set it up anyway to verify timeout behavior
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, context.Canceled)

	_, err := handler.broadcastVoteTx(ctx, msgs, "test memo", logFields, "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to broadcast test transaction")
}

func TestHandler_broadcastVoteTx_VerifyParameters(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	msgs := []sdk.Msg{&utsstypes.MsgVoteTssKeyProcess{
		Signer:    "push1test123",
		TssPubkey: "0x123",
		KeyId:     "key-123",
		ProcessId: 1,
	}}
	memo := "test memo"
	logFields := map[string]interface{}{"key_id": "key-123"}

	expectedTxHash := "0xsuccess"
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, msgs, memo, defaultGasLimit, mock.MatchedBy(func(feeAmount sdk.Coins) bool {
		return feeAmount.String() == "1000000000000000000upc"
	})).
		Return(&sdk.TxResponse{
			TxHash: expectedTxHash,
			Code:   0,
		}, nil)

	txHash, err := handler.broadcastVoteTx(context.Background(), msgs, memo, logFields, "test")

	require.NoError(t, err)
	assert.Equal(t, expectedTxHash, txHash)
	mockSigner.AssertExpectations(t)
}

func TestHandler_VoteTssKeyProcess_MessageCreation(t *testing.T) {
	mockSigner := &MockTxSigner{}
	granter := "push1operator123"
	handler := NewHandler(mockSigner, zerolog.Nop(), granter)

	tssPubKey := "0xabcdef123456"
	keyID := "key-456"
	processId := uint64(99)

	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{
			TxHash: "0xhash",
			Code:   0,
		}, nil).
		Run(func(args mock.Arguments) {
			msgs := args.Get(1).([]sdk.Msg)
			require.Len(t, msgs, 1)
			msg, ok := msgs[0].(*utsstypes.MsgVoteTssKeyProcess)
			require.True(t, ok)
			assert.Equal(t, granter, msg.Signer)
			assert.Equal(t, tssPubKey, msg.TssPubkey)
			assert.Equal(t, keyID, msg.KeyId)
			assert.Equal(t, processId, msg.ProcessId)
		})

	_, err := handler.VoteTssKeyProcess(context.Background(), tssPubKey, keyID, processId)
	require.NoError(t, err)
	mockSigner.AssertExpectations(t)
}

func TestHandler_VoteOutbound_MemoFormat(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	txID := "0xtx789"

	// Test success memo
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, "Vote outbound success: 0xtx789", mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{TxHash: "0xhash1", Code: 0}, nil)

	_, err := handler.VoteOutbound(context.Background(), txID, true, "0xhash", 1000, "")
	require.NoError(t, err)

	// Reset mock
	mockSigner.ExpectedCalls = nil
	mockSigner.Calls = nil

	// Test revert memo
	reason := "expired"
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, "Vote outbound revert: 0xtx789 - expired", mock.Anything, mock.Anything).
		Return(&sdk.TxResponse{TxHash: "0xhash2", Code: 0}, nil)

	_, err = handler.VoteOutbound(context.Background(), txID, false, "", 0, reason)
	require.NoError(t, err)
	mockSigner.AssertExpectations(t)
}

func TestHandler_broadcastVoteTx_ContextCancellation(t *testing.T) {
	mockSigner := &MockTxSigner{}
	handler := NewHandler(mockSigner, zerolog.Nop(), "push1test123")

	msgs := []sdk.Msg{&utsstypes.MsgVoteTssKeyProcess{}}
	logFields := map[string]interface{}{}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Mock should return context cancelled error
	mockSigner.On("SignAndBroadcastAuthZTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, context.Canceled)

	_, err := handler.broadcastVoteTx(ctx, msgs, "test", logFields, "test")

	// Should return error due to cancelled context
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to broadcast test transaction")
}
