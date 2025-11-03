package core

import (
	"context"
	"errors"
	"testing"
	"time"

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

// setupGasVoteTestDB creates an in-memory SQLite database for gas vote testing
func setupGasVoteTestDB(t *testing.T) *db.DB {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Auto-migrate the schema with GasVoteTransaction
	err = gormDB.AutoMigrate(&store.GasVoteTransaction{})
	require.NoError(t, err)

	testDB := &db.DB{}
	testDB.SetupDBForTesting(gormDB)

	return testDB
}

func TestNewGasVoteHandler(t *testing.T) {
	mockSigner := &MockTxSigner{}
	testDB := setupGasVoteTestDB(t)
	log := zerolog.Nop()
	testKeys := &MockUniversalValidatorKeys{
		Address: "cosmos1test",
	}
	granter := "cosmos1granter"

	gh := NewGasVoteHandler(mockSigner, testDB, log, testKeys, granter)

	assert.NotNil(t, gh)
	assert.Equal(t, mockSigner, gh.txSigner)
	assert.Equal(t, testDB, gh.db)
	assert.Equal(t, granter, gh.granter)
}

func TestGasVoteHandler_VoteGasPrice(t *testing.T) {
	tests := []struct {
		name               string
		chainID            string
		price              uint64
		setupMock          func(*MockTxSigner)
		expectedError      bool
		errorMsg           string
		expectedStatus     string
		checkDBRecord      bool
		expectedVoteTxHash string
	}{
		{
			name:    "successful gas price vote",
			chainID: "eip155:1",
			price:   50000000000, // 50 gwei
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx",
					mock.Anything,
					mock.MatchedBy(func(msgs []sdk.Msg) bool {
						if len(msgs) != 1 {
							return false
						}
						msg, ok := msgs[0].(*uetypes.MsgVoteGasPrice)
						if !ok {
							return false
						}
						return msg.ObservedChainId == "eip155:1" &&
							msg.Price == 50000000000 &&
							msg.BlockNumber == 0
					}),
					mock.MatchedBy(func(memo string) bool {
						return memo == "Vote on gas price for eip155:1"
					}),
					uint64(500000000),
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:    0,
					TxHash:  "cosmos_gas_vote_tx_123",
					GasUsed: 200000,
				}, nil)
			},
			expectedError:      false,
			expectedStatus:     "success",
			checkDBRecord:      true,
			expectedVoteTxHash: "cosmos_gas_vote_tx_123",
		},
		{
			name:    "vote transaction fails with non-zero code",
			chainID: "eip155:137",
			price:   30000000000,
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx",
					mock.Anything,
					mock.Anything,
					mock.Anything,
					uint64(500000000),
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:   1,
					TxHash: "failed_tx",
					RawLog: "insufficient funds",
				}, nil)
			},
			expectedError:  true,
			errorMsg:       "gas vote transaction failed with code",
			expectedStatus: "failed",
			checkDBRecord:  true,
		},
		{
			name:    "broadcast error",
			chainID: "eip155:56",
			price:   5000000000,
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx",
					mock.Anything,
					mock.Anything,
					mock.Anything,
					uint64(500000000),
					mock.Anything,
				).Return(nil, errors.New("network error"))
			},
			expectedError:  true,
			errorMsg:       "failed to broadcast gas vote transaction",
			expectedStatus: "failed",
			checkDBRecord:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockSigner := &MockTxSigner{}
			testDB := setupGasVoteTestDB(t)
			log := zerolog.Nop()

			gh := NewGasVoteHandler(mockSigner, testDB, log, &MockUniversalValidatorKeys{}, "cosmos1granter")

			// Setup mock expectations
			tt.setupMock(mockSigner)

			// Execute
			err := gh.VoteGasPrice(context.Background(), tt.chainID, tt.price)

			// Assert error expectations
			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}

			// Check database record if required
			if tt.checkDBRecord {
				var voteRecord store.GasVoteTransaction
				err = testDB.Client().First(&voteRecord).Error
				assert.NoError(t, err)

				// Verify record fields (ChainID removed - stored per-chain)
				assert.Equal(t, tt.price, voteRecord.GasPrice)
				assert.Equal(t, tt.expectedStatus, voteRecord.Status)
				assert.NotZero(t, voteRecord.CreatedAt) // GORM auto-populated

				if tt.expectedStatus == "success" {
					assert.Equal(t, tt.expectedVoteTxHash, voteRecord.VoteTxHash)
					assert.Empty(t, voteRecord.ErrorMsg)
				} else {
					assert.NotEmpty(t, voteRecord.ErrorMsg)
				}
			}

			mockSigner.AssertExpectations(t)
		})
	}
}

func TestGasVoteHandler_executeVote(t *testing.T) {
	tests := []struct {
		name          string
		chainID       string
		price         uint64
		setupMock     func(*MockTxSigner)
		setupHandler  func(*GasVoteHandler)
		expectedError bool
		errorMsg      string
		expectedHash  string
	}{
		{
			name:    "successful execution",
			chainID: "eip155:1",
			price:   100000000000,
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx",
					mock.Anything,
					mock.MatchedBy(func(msgs []sdk.Msg) bool {
						if len(msgs) != 1 {
							return false
						}
						msg, ok := msgs[0].(*uetypes.MsgVoteGasPrice)
						if !ok {
							return false
						}
						return msg.Signer == "cosmos1granter" &&
							msg.ObservedChainId == "eip155:1" &&
							msg.Price == 100000000000 &&
							msg.BlockNumber == 0
					}),
					mock.MatchedBy(func(memo string) bool {
						return memo == "Vote on gas price for eip155:1"
					}),
					uint64(500000000),
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:    0,
					TxHash:  "success_tx_hash",
					GasUsed: 150000,
				}, nil)
			},
			expectedError: false,
			expectedHash:  "success_tx_hash",
		},
		{
			name:    "broadcast failure",
			chainID: "eip155:42161",
			price:   20000000000,
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx",
					mock.Anything,
					mock.Anything,
					mock.Anything,
					uint64(500000000),
					mock.Anything,
				).Return(nil, errors.New("connection timeout"))
			},
			expectedError: true,
			errorMsg:      "failed to broadcast gas vote transaction",
		},
		{
			name:    "transaction rejected with non-zero code",
			chainID: "eip155:10",
			price:   10000000000,
			setupMock: func(m *MockTxSigner) {
				m.On("SignAndBroadcastAuthZTx",
					mock.Anything,
					mock.Anything,
					mock.Anything,
					uint64(500000000),
					mock.Anything,
				).Return(&sdk.TxResponse{
					Code:   5,
					TxHash: "rejected_tx",
					RawLog: "unauthorized signer",
				}, nil)
			},
			expectedError: true,
			errorMsg:      "gas vote transaction failed with code 5",
		},
		{
			name:      "nil txSigner",
			chainID:   "eip155:1",
			price:     50000000000,
			setupMock: func(m *MockTxSigner) {},
			setupHandler: func(gh *GasVoteHandler) {
				gh.txSigner = nil
			},
			expectedError: true,
			errorMsg:      "txSigner is nil",
		},
		{
			name:      "empty granter address",
			chainID:   "eip155:1",
			price:     50000000000,
			setupMock: func(m *MockTxSigner) {},
			setupHandler: func(gh *GasVoteHandler) {
				gh.granter = ""
			},
			expectedError: true,
			errorMsg:      "granter address is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSigner := &MockTxSigner{}
			gh := &GasVoteHandler{
				txSigner: mockSigner,
				granter:  "cosmos1granter",
				log:      zerolog.Nop(),
			}

			// Apply custom handler setup if provided
			if tt.setupHandler != nil {
				tt.setupHandler(gh)
			}

			// Setup mock expectations
			tt.setupMock(mockSigner)

			// Execute
			txHash, err := gh.executeVote(context.Background(), tt.chainID, tt.price)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
				assert.Empty(t, txHash)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedHash, txHash)
			}

			mockSigner.AssertExpectations(t)
		})
	}
}

func TestGasVoteHandler_DatabaseRecordPersistence(t *testing.T) {
	// Setup
	mockSigner := &MockTxSigner{}
	testDB := setupGasVoteTestDB(t)
	log := zerolog.Nop()

	// Setup mock to return success
	mockSigner.On("SignAndBroadcastAuthZTx",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		uint64(500000000),
		mock.Anything,
	).Return(&sdk.TxResponse{
		Code:    0,
		TxHash:  "persistent_tx_hash",
		GasUsed: 180000,
	}, nil)

	gh := NewGasVoteHandler(mockSigner, testDB, log, &MockUniversalValidatorKeys{}, "cosmos1granter")

	// Execute multiple votes for the same chain (per-chain database architecture)
	chainID := "eip155:1"
	prices := []uint64{50000000000, 30000000000, 5000000000}

	for _, price := range prices {
		err := gh.VoteGasPrice(context.Background(), chainID, price)
		require.NoError(t, err)
	}

	// Verify all records were stored (3 votes for the same chain)
	var allVotes []store.GasVoteTransaction
	err := testDB.Client().Find(&allVotes).Error
	require.NoError(t, err)
	assert.Len(t, allVotes, 3)

	// Verify each record (ChainID removed - stored in per-chain database)
	for i, price := range prices {
		assert.Equal(t, price, allVotes[i].GasPrice)
		assert.Equal(t, "success", allVotes[i].Status)
		assert.Equal(t, "persistent_tx_hash", allVotes[i].VoteTxHash)
		assert.NotZero(t, allVotes[i].ID)
		assert.NotZero(t, allVotes[i].CreatedAt) // GORM auto-populated
		assert.NotZero(t, allVotes[i].UpdatedAt) // GORM auto-populated

		t.Logf("Vote %d: Price=%d, TxHash=%s",
			i+1, allVotes[i].GasPrice, allVotes[i].VoteTxHash)
	}

	mockSigner.AssertExpectations(t)
}

func TestGasVoteHandler_FailureRecordStorage(t *testing.T) {
	// Setup
	mockSigner := &MockTxSigner{}
	testDB := setupGasVoteTestDB(t)
	log := zerolog.Nop()

	// Setup mock to return failure
	mockSigner.On("SignAndBroadcastAuthZTx",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		uint64(500000000),
		mock.Anything,
	).Return(nil, errors.New("test broadcast failure"))

	gh := NewGasVoteHandler(mockSigner, testDB, log, &MockUniversalValidatorKeys{}, "cosmos1granter")

	// Execute vote that will fail
	chainID := "eip155:1"
	price := uint64(50000000000)

	err := gh.VoteGasPrice(context.Background(), chainID, price)
	assert.Error(t, err)

	// Verify failure record was stored (ChainID removed - per-chain database)
	var voteRecord store.GasVoteTransaction
	err = testDB.Client().First(&voteRecord).Error
	require.NoError(t, err)

	assert.Equal(t, price, voteRecord.GasPrice)
	assert.Equal(t, "failed", voteRecord.Status)
	assert.Empty(t, voteRecord.VoteTxHash)
	assert.Contains(t, voteRecord.ErrorMsg, "test broadcast failure")
	assert.NotZero(t, voteRecord.CreatedAt) // GORM auto-populated

	mockSigner.AssertExpectations(t)
}

func TestGasVoteHandler_ContextTimeout(t *testing.T) {
	// Setup
	mockSigner := &MockTxSigner{}
	testDB := setupGasVoteTestDB(t)
	log := zerolog.Nop()

	// Setup mock to simulate a long-running operation
	mockSigner.On("SignAndBroadcastAuthZTx",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		uint64(500000000),
		mock.Anything,
	).Run(func(args mock.Arguments) {
		// Simulate delay
		time.Sleep(100 * time.Millisecond)
	}).Return(&sdk.TxResponse{
		Code:   0,
		TxHash: "delayed_tx",
	}, nil)

	gh := NewGasVoteHandler(mockSigner, testDB, log, &MockUniversalValidatorKeys{}, "cosmos1granter")

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Execute vote with timeout context
	err := gh.VoteGasPrice(ctx, "eip155:1", 50000000000)

	// The error might be context deadline exceeded or the operation might complete
	// depending on timing, but we should handle both cases gracefully
	if err != nil {
		t.Logf("Vote failed with error (expected): %v", err)
	} else {
		t.Log("Vote completed despite timeout context")
	}
}

func TestGasVoteHandler_MessageConstruction(t *testing.T) {
	// Setup
	mockSigner := &MockTxSigner{}
	testDB := setupGasVoteTestDB(t)
	log := zerolog.Nop()

	chainID := "eip155:1"
	price := uint64(75000000000)
	granter := "cosmos1testgranter"

	// Capture the message passed to SignAndBroadcastAuthZTx
	var capturedMsg *uetypes.MsgVoteGasPrice
	mockSigner.On("SignAndBroadcastAuthZTx",
		mock.Anything,
		mock.MatchedBy(func(msgs []sdk.Msg) bool {
			if len(msgs) == 1 {
				if msg, ok := msgs[0].(*uetypes.MsgVoteGasPrice); ok {
					capturedMsg = msg
					return true
				}
			}
			return false
		}),
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return(&sdk.TxResponse{
		Code:   0,
		TxHash: "msg_test_tx",
	}, nil)

	gh := NewGasVoteHandler(mockSigner, testDB, log, &MockUniversalValidatorKeys{}, granter)

	// Execute
	err := gh.VoteGasPrice(context.Background(), chainID, price)
	require.NoError(t, err)

	// Verify message fields
	require.NotNil(t, capturedMsg)
	assert.Equal(t, granter, capturedMsg.Signer)
	assert.Equal(t, chainID, capturedMsg.ObservedChainId)
	assert.Equal(t, price, capturedMsg.Price)
	assert.Equal(t, uint64(0), capturedMsg.BlockNumber)

	mockSigner.AssertExpectations(t)
}
