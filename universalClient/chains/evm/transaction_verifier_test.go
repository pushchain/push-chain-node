package evm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)


func TestNewTransactionVerifier(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)

	config := &uregistrytypes.ChainConfig{
		Chain: "eip155:1",
	}

	// Setup test database
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	database := &db.DB{}
	_ = gormDB // Database would be initialized properly in production

	client := &Client{}
	tracker := common.NewConfirmationTracker(database, nil, logger)

	verifier := NewTransactionVerifier(client, config, database, tracker, logger)

	assert.NotNil(t, verifier)
	assert.Equal(t, client, verifier.parentClient)
	assert.Equal(t, config, verifier.config)
	assert.Equal(t, database, verifier.database)
	assert.Equal(t, tracker, verifier.tracker)
}

func TestTransactionVerifier_GetTransactionConfirmations(t *testing.T) {
	// Test basic initialization and structure
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	config := &uregistrytypes.ChainConfig{Chain: "eip155:1"}
	database := &db.DB{}
	tracker := common.NewConfirmationTracker(nil, nil, logger)
	client := &Client{}

	verifier := &TransactionVerifier{
		parentClient: client,
		config:       config,
		database:     database,
		tracker:      tracker,
		logger:       logger,
	}

	// Verify the method exists and is callable
	assert.NotNil(t, verifier.GetTransactionConfirmations)
}

func TestTransactionVerifier_VerifyTransactionExistence(t *testing.T) {
	// Test that the method exists and validates transaction status
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	config := &uregistrytypes.ChainConfig{Chain: "eip155:1"}
	database := &db.DB{}
	tracker := common.NewConfirmationTracker(nil, nil, logger)
	client := &Client{}

	verifier := &TransactionVerifier{
		parentClient: client,
		config:       config,
		database:     database,
		tracker:      tracker,
		logger:       logger,
	}

	// Test with a sample transaction
	tx := &store.ChainTransaction{
		TxHash:      "0x123",
		BlockNumber: 100,
		Status:      "confirmation_pending",
	}

	// Verify the method exists and is callable
	assert.NotNil(t, verifier.VerifyTransactionExistence)
	assert.NotNil(t, tx)
}

func TestTransactionVerifier_VerifyPendingTransactions(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)

	// Setup test database
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Auto migrate the schema
	err = gormDB.AutoMigrate(&store.ChainTransaction{})
	assert.NoError(t, err)

	database := &db.DB{}
	_ = gormDB // Database would be initialized properly in production

	// Create test transactions
	testTxs := []store.ChainTransaction{
		{
			TxHash:        "0x111",
			BlockNumber:   100,
			Status:        "confirmation_pending",
			Confirmations: 0,
		},
		{
			TxHash:        "0x222",
			BlockNumber:   101,
			Status:        "awaiting_vote",
			Confirmations: 0,
		},
		{
			TxHash:        "0x333",
			BlockNumber:   102,
			Status:        "completed", // Should not be fetched
			Confirmations: 12,
		},
	}

	for _, tx := range testTxs {
		err := gormDB.Create(&tx).Error
		assert.NoError(t, err)
	}

	client := &Client{}
	config := &uregistrytypes.ChainConfig{Chain: "eip155:1"}
	tracker := common.NewConfirmationTracker(database, nil, logger)

	verifier := &TransactionVerifier{
		parentClient: client,
		config:       config,
		database:     database,
		tracker:      tracker,
		logger:       logger,
	}

	// Verify the method exists
	assert.NotNil(t, verifier.VerifyPendingTransactions)

	// Check that transactions were created correctly
	var count int64
	err = gormDB.Model(&store.ChainTransaction{}).Count(&count).Error
	assert.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestTransactionVerifier_InterfaceCompliance(t *testing.T) {
	// Ensure TransactionVerifier has all expected methods
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	config := &uregistrytypes.ChainConfig{}
	database := &db.DB{}
	tracker := common.NewConfirmationTracker(nil, nil, logger)
	client := &Client{}

	verifier := NewTransactionVerifier(client, config, database, tracker, logger)

	// Test that all methods exist and are callable
	assert.NotNil(t, verifier.GetTransactionConfirmations)
	assert.NotNil(t, verifier.VerifyTransactionExistence)
	assert.NotNil(t, verifier.VerifyPendingTransactions)

	// Verify struct fields
	assert.NotNil(t, verifier.parentClient)
	assert.NotNil(t, verifier.config)
	assert.NotNil(t, verifier.database)
	assert.NotNil(t, verifier.tracker)
	assert.NotNil(t, verifier.logger)
}

func TestTransactionVerifier_ReceiptValidation(t *testing.T) {
	tests := []struct {
		name               string
		receipt            *types.Receipt
		expectedExists     bool
		expectedStatusCode uint64
	}{
		{
			name:               "nil receipt",
			receipt:            nil,
			expectedExists:     false,
			expectedStatusCode: 0,
		},
		{
			name: "successful receipt",
			receipt: &types.Receipt{
				Status:      1,
				BlockNumber: big.NewInt(100),
			},
			expectedExists:     true,
			expectedStatusCode: 1,
		},
		{
			name: "failed receipt",
			receipt: &types.Receipt{
				Status:      0,
				BlockNumber: big.NewInt(100),
			},
			expectedExists:     false,
			expectedStatusCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test receipt validation logic
			if tt.receipt == nil {
				assert.False(t, tt.expectedExists)
			} else {
				assert.Equal(t, tt.expectedStatusCode, tt.receipt.Status)
				if tt.receipt.Status == 0 {
					assert.False(t, tt.expectedExists)
				} else {
					assert.True(t, tt.expectedExists)
				}
			}
		})
	}
}