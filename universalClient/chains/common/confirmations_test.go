package common

import (
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/store"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

func TestNewConfirmationTracker(t *testing.T) {
	logger := zerolog.Nop()
	
	// Test with nil config (uses defaults)
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	assert.NotNil(t, tracker)
	assert.Equal(t, uint64(5), tracker.fastInbound)
	assert.Equal(t, uint64(12), tracker.standardInbound)
	
	// Test with custom config
	config := &uregistrytypes.BlockConfirmation{
		FastInbound:     10,
		StandardInbound: 20,
	}
	tracker = NewConfirmationTracker(database, config, logger)
	assert.NotNil(t, tracker)
	assert.Equal(t, uint64(10), tracker.fastInbound)
	assert.Equal(t, uint64(20), tracker.standardInbound)
}

func TestTrackTransaction(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Track a new transaction
	err = tracker.TrackTransaction(
		"eip155:11155111",
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		[]byte("test data"),
	)
	require.NoError(t, err)
	
	// Verify transaction was stored
	tx, err := tracker.GetGatewayTransaction("0x1234567890abcdef")
	require.NoError(t, err)
	assert.Equal(t, "eip155:11155111", tx.ChainID)
	assert.Equal(t, "0x1234567890abcdef", tx.TxHash)
	assert.Equal(t, uint64(100), tx.BlockNumber)
	assert.Equal(t, "addFunds", tx.Method)
	assert.Equal(t, "pending", tx.Status)
	assert.Equal(t, uint64(0), tx.Confirmations)
	
	// Track the same transaction again (should update)
	err = tracker.TrackTransaction(
		"eip155:11155111",
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		[]byte("updated data"),
	)
	require.NoError(t, err)
}

func TestUpdateConfirmations(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Track a transaction at block 100
	err = tracker.TrackTransaction(
		"eip155:11155111",
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		nil,
	)
	require.NoError(t, err)
	
	// Update confirmations with current block at 105
	err = tracker.UpdateConfirmations("eip155:11155111", 105)
	require.NoError(t, err)
	
	// Check confirmations
	tx, err := tracker.GetGatewayTransaction("0x1234567890abcdef")
	require.NoError(t, err)
	assert.Equal(t, uint64(5), tx.Confirmations)
	assert.Equal(t, "pending", tx.Status) // Still pending (needs 12 for standard)
	
	// Update confirmations with current block at 112
	err = tracker.UpdateConfirmations("eip155:11155111", 112)
	require.NoError(t, err)
	
	// Check confirmations
	tx, err = tracker.GetGatewayTransaction("0x1234567890abcdef")
	require.NoError(t, err)
	assert.Equal(t, uint64(12), tx.Confirmations)
	assert.Equal(t, "confirmed", tx.Status) // Now confirmed
}

func TestIsConfirmed(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Track a transaction
	err = tracker.TrackTransaction(
		"eip155:11155111",
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		nil,
	)
	require.NoError(t, err)
	
	// Initially not confirmed
	confirmed, err := tracker.IsConfirmed("0x1234567890abcdef", "fast")
	require.NoError(t, err)
	assert.False(t, confirmed)
	
	// Update to 5 confirmations
	err = tracker.UpdateConfirmations("eip155:11155111", 105)
	require.NoError(t, err)
	
	// Now confirmed for fast mode
	confirmed, err = tracker.IsConfirmed("0x1234567890abcdef", "fast")
	require.NoError(t, err)
	assert.True(t, confirmed)
	
	// But not confirmed for standard mode
	confirmed, err = tracker.IsConfirmed("0x1234567890abcdef", "standard")
	require.NoError(t, err)
	assert.False(t, confirmed)
	
	// Update to 12 confirmations
	err = tracker.UpdateConfirmations("eip155:11155111", 112)
	require.NoError(t, err)
	
	// Now confirmed for both modes
	confirmed, err = tracker.IsConfirmed("0x1234567890abcdef", "standard")
	require.NoError(t, err)
	assert.True(t, confirmed)
}

func TestGetConfirmedTransactions(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Track multiple transactions
	for i := 0; i < 3; i++ {
		txHash := fmt.Sprintf("0x%d", i)
		err = tracker.TrackTransaction(
			"eip155:11155111",
			txHash,
			uint64(100+i),
			"addFunds",
			"0xf9bfe8a7",
			nil,
		)
		require.NoError(t, err)
	}
	
	// Confirm only the first one (needs 12 confirmations with standardInbound)
	err = tracker.UpdateConfirmations("eip155:11155111", 112)
	require.NoError(t, err)
	
	// Mark the third as failed
	err = tracker.MarkTransactionFailed("0x2")
	require.NoError(t, err)
	
	// Get confirmed transactions
	txs, err := tracker.GetConfirmedTransactions("eip155:11155111")
	require.NoError(t, err)
	assert.Len(t, txs, 1) // Only first one should be confirmed (block 100, confirmations = 112-100 = 12)
}

func TestMarkTransactionFailed(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Track a transaction
	err = tracker.TrackTransaction(
		"eip155:11155111",
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		nil,
	)
	require.NoError(t, err)
	
	// Mark as failed
	err = tracker.MarkTransactionFailed("0x1234567890abcdef")
	require.NoError(t, err)
	
	// Verify status
	var tx store.GatewayTransaction
	err = database.Client().Where("tx_hash = ?", "0x1234567890abcdef").First(&tx).Error
	require.NoError(t, err)
	assert.Equal(t, "failed", tx.Status)
}