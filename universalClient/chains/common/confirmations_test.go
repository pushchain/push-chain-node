package common

import (
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
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
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		[]byte("test data"),
	)
	require.NoError(t, err)
	
	// Verify transaction was stored
	tx, err := tracker.GetGatewayTransaction("0x1234567890abcdef")
	require.NoError(t, err)
	// ChainID no longer exists in ChainTransaction
	assert.Equal(t, "0x1234567890abcdef", tx.TxHash)
	assert.Equal(t, uint64(100), tx.BlockNumber)
	assert.Equal(t, "addFunds", tx.Method)
	assert.Equal(t, "confirmation_pending", tx.Status)
	assert.Equal(t, uint64(0), tx.Confirmations)
	
	// Track the same transaction again (should update)
	err = tracker.TrackTransaction(
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
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
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)
	
	// Update confirmations with current block at 105
	err = tracker.UpdateConfirmations(105)
	require.NoError(t, err)
	
	// Check confirmations - still pending since STANDARD requires 12
	tx, err := tracker.GetGatewayTransaction("0x1234567890abcdef")
	require.NoError(t, err)
	assert.Equal(t, uint64(5), tx.Confirmations)
	assert.Equal(t, "confirmation_pending", tx.Status) // Still pending (STANDARD requires 12)
	
	// Update confirmations with current block at 112
	err = tracker.UpdateConfirmations(112)
	require.NoError(t, err)
	
	// Check confirmations - now confirmed
	tx, err = tracker.GetGatewayTransaction("0x1234567890abcdef")
	require.NoError(t, err)
	assert.Equal(t, uint64(12), tx.Confirmations)
	assert.Equal(t, "awaiting_vote", tx.Status) // Now confirmed (standard)
}

func TestIsConfirmed(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Test FAST confirmation type
	err = tracker.TrackTransaction(
		"0xfast",
		100,
		"addFunds",
		"0xf9bfe8a7",
		"FAST",
		nil,
	)
	require.NoError(t, err)
	
	// Initially not confirmed
	confirmed, err := tracker.IsConfirmed("0xfast")
	require.NoError(t, err)
	assert.False(t, confirmed)
	
	// Update to 5 confirmations (fast threshold)
	err = tracker.UpdateConfirmations(105)
	require.NoError(t, err)
	
	// Now confirmed for FAST type (but status is awaiting_vote, not confirmed)
	confirmed, err = tracker.IsConfirmed("0xfast")
	require.NoError(t, err)
	assert.False(t, confirmed) // Status is awaiting_vote, not confirmed
	
	// Test STANDARD confirmation type
	err = tracker.TrackTransaction(
		"0xstandard",
		100,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)
	
	// Not confirmed at 5 confirmations
	confirmed, err = tracker.IsConfirmed("0xstandard")
	require.NoError(t, err)
	assert.False(t, confirmed)
	
	// Update to 12 confirmations (standard threshold)
	err = tracker.UpdateConfirmations(112)
	require.NoError(t, err)
	
	// Now confirmed for STANDARD type (but status is awaiting_vote, not confirmed)
	confirmed, err = tracker.IsConfirmed("0xstandard")
	require.NoError(t, err)
	assert.False(t, confirmed) // Status is awaiting_vote, not confirmed
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
			txHash,
			uint64(100+i),
			"addFunds",
			"0xf9bfe8a7",
			"STANDARD",
			nil,
		)
		require.NoError(t, err)
	}
	
	// Confirm only the first one (needs 12 confirmations with standardInbound)
	err = tracker.UpdateConfirmations(112)
	require.NoError(t, err)
	
	// Mark the third as failed
	err = tracker.MarkTransactionFailed("0x2")
	require.NoError(t, err)
	
	// Get confirmed transactions
	txs, err := tracker.GetConfirmedTransactions("eip155:11155111")
	require.NoError(t, err)
	assert.Len(t, txs, 0) // None are "confirmed" - they become "awaiting_vote" after reaching confirmations
}

func TestMarkTransactionFailed(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Track a transaction
	err = tracker.TrackTransaction(
		"0x1234567890abcdef",
		100,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)
	
	// Mark as failed
	err = tracker.MarkTransactionFailed("0x1234567890abcdef")
	require.NoError(t, err)
	
	// Verify status
	var tx store.ChainTransaction
	err = database.Client().Where("tx_hash = ?", "0x1234567890abcdef").First(&tx).Error
	require.NoError(t, err)
	assert.Equal(t, "failed", tx.Status)
}

func TestIsConfirmedWithReorgedStatus(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()
	
	tracker := NewConfirmationTracker(database, nil, logger)
	
	// Create a transaction and manually set it to reorged status
	tx := &store.ChainTransaction{
		
		TxHash:          "0x1234567890abcdef",
		BlockNumber:     100,
		Method:          "addFunds",
		EventIdentifier: "0xf9bfe8a7",
		Status:          "reorged",
		Confirmations:   0,
	}
	err = database.Client().Create(tx).Error
	require.NoError(t, err)
	
	// Test that reorged transactions are never considered confirmed
	confirmed, err := tracker.IsConfirmed("0x1234567890abcdef")
	require.NoError(t, err)
	assert.False(t, confirmed)
	
	confirmed, err = tracker.IsConfirmed("0x1234567890abcdef")
	require.NoError(t, err)
	assert.False(t, confirmed)
}