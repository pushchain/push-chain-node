package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// MockTxSigner is a mock for the TxSignerInterface
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

// MockUniversalValidatorKeys is a mock for keys.UniversalValidatorKeys
type MockUniversalValidatorKeys struct {
	Address string
}

func (m *MockUniversalValidatorKeys) GetAddress() (sdk.AccAddress, error) {
	return sdk.AccAddress([]byte(m.Address)), nil
}

func (m *MockUniversalValidatorKeys) GetPrivateKey(password string) (cryptotypes.PrivKey, error) {
	return nil, nil
}

func (m *MockUniversalValidatorKeys) GetHotkeyPassword() string {
	return ""
}

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *db.DB {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	
	// Auto-migrate the schema
	err = gormDB.AutoMigrate(&store.ChainTransaction{})
	require.NoError(t, err)
	
	testDB := &db.DB{}
	// Use the test helper method
	testDB.SetupDBForTesting(gormDB)
	
	return testDB
}

func TestNewVoteHandler(t *testing.T) {
	mockSigner := &MockTxSigner{}
	testDB := setupTestDB(t)
	log := zerolog.Nop()
	testKeys := &MockUniversalValidatorKeys{
		Address: "cosmos1test",
	}
	granter := "cosmos1granter"
	
	vh := NewVoteHandler(mockSigner, testDB, log, testKeys, granter)
	
	assert.NotNil(t, vh)
	assert.Equal(t, mockSigner, vh.txSigner)
	assert.Equal(t, testDB, vh.db)
	assert.Equal(t, granter, vh.granter)
}

func TestVoteHandler_VoteAndConfirm(t *testing.T) {
	tests := []struct {
		name          string
		tx            *store.ChainTransaction
		setupMock     func(*MockTxSigner)
		expectedError bool
		errorMsg      string
	}{
		{
			name: "successful vote and confirm",
			tx: &store.ChainTransaction{
				TxHash:        "0x123",
				BlockNumber:   100,
				Method:        "addFunds",
				Status:        "pending",
				Confirmations: 10,
				Data:          json.RawMessage(`{"sender":"0xabc","amount":"1000"}`),
			},
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx", 
					mock.Anything, 
					mock.Anything, 
					mock.Anything, 
					uint64(500000), 
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:    0,
					TxHash:  "cosmos_tx_123",
					GasUsed: 200000,
				}, nil)
			},
			expectedError: false,
		},
		{
			name: "vote transaction fails with non-zero code",
			tx: &store.ChainTransaction{
				TxHash:        "0x456",
				BlockNumber:   200,
				Method:        "add_funds",
				Status:        "pending",
				Confirmations: 15,
				Data:          json.RawMessage(`{}`),
			},
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx", 
					mock.Anything, 
					mock.Anything, 
					mock.Anything, 
					uint64(500000), 
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:   1,
					RawLog: "insufficient funds",
				}, nil)
			},
			expectedError: true,
			errorMsg:      "vote transaction failed with code",
		},
		{
			name: "broadcast error",
			tx: &store.ChainTransaction{
				TxHash:        "0x789",
				BlockNumber:   300,
				Method:        "addFunds",
				Status:        "pending",
				Confirmations: 20,
			},
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx", 
					mock.Anything, 
					mock.Anything, 
					mock.Anything, 
					uint64(500000), 
					mock.Anything,
				).Return(nil, errors.New("network error"))
			},
			expectedError: true,
			errorMsg:      "failed to broadcast vote transaction",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockSigner := &MockTxSigner{}
			testDB := setupTestDB(t)
			log := zerolog.Nop()
			
			// Save initial transaction
			err := testDB.Client().Create(tt.tx).Error
			require.NoError(t, err)
			
			vh := NewVoteHandler(mockSigner, testDB, log, &MockUniversalValidatorKeys{}, "cosmos1granter")
			
			// Setup mock expectations
			tt.setupMock(mockSigner)
			
			// Execute
			err = vh.VoteAndConfirm(context.Background(), tt.tx)
			
			// Assert
			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				
				// Verify transaction status was updated
				var updatedTx store.ChainTransaction
				err = testDB.Client().Where("tx_hash = ?", tt.tx.TxHash).First(&updatedTx).Error
				assert.NoError(t, err)
				assert.Equal(t, "confirmed", updatedTx.Status)
				assert.True(t, updatedTx.UpdatedAt.After(time.Now().Add(-time.Minute)))
			}
			
			mockSigner.AssertExpectations(t)
		})
	}
}

func TestVoteHandler_constructInbound(t *testing.T) {
	vh := &VoteHandler{log: zerolog.Nop()}
	
	tests := []struct {
		name     string
		tx       *store.ChainTransaction
		expected *uetypes.Inbound
	}{
		{
			name: "complete data for EVM transaction",
			tx: &store.ChainTransaction{
				TxHash: "0xabc123",
				Method: "addFunds",
				Data: json.RawMessage(`{
					"source_chain": "eip155:1",
					"sender": "0x111",
					"recipient": "0x222",
					"amount": "1000000",
					"asset_address": "0x333",
					"log_index": "5",
					"tx_type": "FEE_ABSTRACTION"
				}`),
			},
			expected: &uetypes.Inbound{
				SourceChain: "eip155:1",
				TxHash:      "0xabc123",
				Sender:      "0x111",
				Recipient:   "0x222",
				Amount:      "1000000",
				AssetAddr:   "0x333",
				LogIndex:    "5",
				TxType:      uetypes.InboundTxType_FEE_ABSTRACTION,
			},
		},
		{
			name: "minimal data with defaults",
			tx: &store.ChainTransaction{
				TxHash: "0xdef456",
				Method: "add_funds",
				Data:   json.RawMessage(`{}`),
			},
			expected: &uetypes.Inbound{
				SourceChain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
				TxHash:      "0xdef456",
				Sender:      "0x0000000000000000000000000000000000000000",
				Recipient:   "0x0000000000000000000000000000000000000000",
				Amount:      "0",
				AssetAddr:   "0x0000000000000000000000000000000000000000",
				LogIndex:    "0",
				TxType:      uetypes.InboundTxType_SYNTHETIC,
			},
		},
		{
			name: "nil data with method-based chain inference",
			tx: &store.ChainTransaction{
				TxHash: "0x789",
				Method: "addFunds",
				Data:   nil,
			},
			expected: &uetypes.Inbound{
				SourceChain: "eip155:11155111",
				TxHash:      "0x789",
				Sender:      "0x0000000000000000000000000000000000000000",
				Recipient:   "0x0000000000000000000000000000000000000000",
				Amount:      "0",
				AssetAddr:   "0x0000000000000000000000000000000000000000",
				LogIndex:    "0",
				TxType:      uetypes.InboundTxType_SYNTHETIC,
			},
		},
		{
			name: "chain_id fallback",
			tx: &store.ChainTransaction{
				TxHash: "0xchain",
				Method: "unknown",
				Data: json.RawMessage(`{
					"chain_id": "eip155:42161"
				}`),
			},
			expected: &uetypes.Inbound{
				SourceChain: "eip155:42161",
				TxHash:      "0xchain",
				Sender:      "0x0000000000000000000000000000000000000000",
				Recipient:   "0x0000000000000000000000000000000000000000",
				Amount:      "0",
				AssetAddr:   "0x0000000000000000000000000000000000000000",
				LogIndex:    "0",
				TxType:      uetypes.InboundTxType_SYNTHETIC,
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound, err := vh.constructInbound(tt.tx)
			
			assert.NoError(t, err)
			assert.Equal(t, tt.expected.SourceChain, inbound.SourceChain)
			assert.Equal(t, tt.expected.TxHash, inbound.TxHash)
			assert.Equal(t, tt.expected.Sender, inbound.Sender)
			assert.Equal(t, tt.expected.Recipient, inbound.Recipient)
			assert.Equal(t, tt.expected.Amount, inbound.Amount)
			assert.Equal(t, tt.expected.AssetAddr, inbound.AssetAddr)
			assert.Equal(t, tt.expected.LogIndex, inbound.LogIndex)
			assert.Equal(t, tt.expected.TxType, inbound.TxType)
		})
	}
}

func TestVoteHandler_executeVote(t *testing.T) {
	tests := []struct {
		name          string
		inbound       *uetypes.Inbound
		setupMock     func(*MockTxSigner)
		expectedError bool
		errorMsg      string
	}{
		{
			name: "successful execution",
			inbound: &uetypes.Inbound{
				SourceChain: "eip155:1",
				TxHash:      "0x123",
				Sender:      "0xsender",
				Recipient:   "0xrecipient",
				Amount:      "1000",
			},
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx", 
					mock.Anything,
					mock.MatchedBy(func(msgs []sdk.Msg) bool {
						return len(msgs) == 1 && msgs[0].(*uetypes.MsgVoteInbound) != nil
					}),
					mock.MatchedBy(func(memo string) bool {
						return memo == "Vote on inbound tx 0x123"
					}),
					uint64(500000),
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:    0,
					TxHash:  "cosmos_tx",
					GasUsed: 150000,
				}, nil)
			},
			expectedError: false,
		},
		{
			name: "broadcast failure",
			inbound: &uetypes.Inbound{
				TxHash: "0x456",
			},
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx", 
					mock.Anything,
					mock.Anything,
					mock.Anything,
					uint64(500000),
					mock.Anything,
				).Return(nil, errors.New("connection timeout"))
			},
			expectedError: true,
			errorMsg:      "failed to broadcast vote transaction",
		},
		{
			name: "transaction rejected",
			inbound: &uetypes.Inbound{
				TxHash: "0x789",
			},
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx", 
					mock.Anything,
					mock.Anything,
					mock.Anything,
					uint64(500000),
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:   5,
					RawLog: "unauthorized",
				}, nil)
			},
			expectedError: true,
			errorMsg:      "vote transaction failed with code 5",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSigner := &MockTxSigner{}
			vh := &VoteHandler{
				txSigner: mockSigner,
				granter:  "cosmos1granter",
				log:      zerolog.Nop(),
			}
			
			tt.setupMock(mockSigner)
			
			err := vh.executeVote(context.Background(), tt.inbound)
			
			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
			
			mockSigner.AssertExpectations(t)
		})
	}
}

func TestVoteHandler_GetPendingTransactions(t *testing.T) {
	testDB := setupTestDB(t)
	vh := &VoteHandler{
		db:  testDB,
		log: zerolog.Nop(),
	}
	
	// Create test transactions
	txs := []store.ChainTransaction{
		{
			TxHash:        "0x1",
			Status:        "confirmation_pending",
			Confirmations: 10,
		},
		{
			TxHash:        "0x2",
			Status:        "confirmation_pending",
			Confirmations: 5,
		},
		{
			TxHash:        "0x3",
			Status:        "confirmed",
			Confirmations: 15,
		},
		{
			TxHash:        "0x4",
			Status:        "awaiting_vote",
			Confirmations: 20,
		},
	}
	
	for _, tx := range txs {
		err := testDB.Client().Create(&tx).Error
		require.NoError(t, err)
	}
	
	// Test with minConfirmations = 10
	pendingTxs, err := vh.GetPendingTransactions(10)
	assert.NoError(t, err)
	assert.Len(t, pendingTxs, 2)
	
	// Verify correct transactions were returned
	txHashes := make([]string, len(pendingTxs))
	for i, tx := range pendingTxs {
		txHashes[i] = tx.TxHash
	}
	assert.Contains(t, txHashes, "0x1")
	assert.Contains(t, txHashes, "0x4")
	assert.NotContains(t, txHashes, "0x2") // Not enough confirmations
	assert.NotContains(t, txHashes, "0x3") // Already confirmed
	
	// Test with minConfirmations = 5
	pendingTxs, err = vh.GetPendingTransactions(5)
	assert.NoError(t, err)
	assert.Len(t, pendingTxs, 3)
	
	// Test with minConfirmations = 25 (no results)
	pendingTxs, err = vh.GetPendingTransactions(25)
	assert.NoError(t, err)
	assert.Len(t, pendingTxs, 0)
}

// Ensure MockTxSigner implements TxSignerInterface
var _ TxSignerInterface = (*MockTxSigner)(nil)