package svm

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chaincommon "github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/db"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

func TestSolanaGatewayHandler_SlotConfirmations(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		VmType:         uregistrytypes.VmType_SVM,
		GatewayAddress: "11111111111111111111111111111112",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "add_funds",
				Identifier:      "84ed4c39500ab38a",
				EventIdentifier: "funds_added",
			},
		},
	}

	// Test configuration
	assert.Equal(t, "11111111111111111111111111111112", config.GatewayAddress)
	assert.Equal(t, uint32(5), config.BlockConfirmation.FastInbound)
	assert.Equal(t, uint32(12), config.BlockConfirmation.StandardInbound)

	// Create confirmation tracker
	tracker := chaincommon.NewConfirmationTracker(
		database,
		config.BlockConfirmation,
		logger,
	)

	// Test tracking Solana transaction
	txSignature := "5wHu1qwD7q5ifaN5nwdcDqNFo53GJqa2tkAMzRcMJFRKQPpMi5kXyzFe2HjSEJQeFRBKNtEe6qEKUfJedMa9pLXa"
	slotNumber := uint64(150000000)

	err = tracker.TrackTransaction(
		txSignature,
		slotNumber,
		"add_funds",
		"84ed4c39500ab38a",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)

	// Verify transaction was tracked
	tx, err := tracker.GetGatewayTransaction(txSignature)
	require.NoError(t, err)
	// ChainID no longer exists in ChainTransaction
	assert.Equal(t, txSignature, tx.TxHash)
	assert.Equal(t, slotNumber, tx.BlockNumber) // In Solana, we use slot number as block number
	assert.Equal(t, "add_funds", tx.Method)
	assert.Equal(t, "pending", tx.Status)
}

func TestSolanaGatewayHandler_ConfirmationLevels(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
	}

	tracker := chaincommon.NewConfirmationTracker(
		database,
		config.BlockConfirmation,
		logger,
	)

	// Test FAST confirmation type (5 blocks)
	fastTxSig := "fastTxSignature123"
	fastStartSlot := uint64(150000000)

	err = tracker.TrackTransaction(
		fastTxSig,
		fastStartSlot,
		"add_funds",
		"84ed4c39500ab38a",
		"FAST",
		nil,
	)
	require.NoError(t, err)

	// Check not confirmed with 4 slots
	currentSlot := fastStartSlot + 4
	err = tracker.UpdateConfirmations(currentSlot)
	require.NoError(t, err)

	confirmed, err := tracker.IsConfirmed(fastTxSig)
	require.NoError(t, err)
	assert.False(t, confirmed, "FAST transaction should not be confirmed with 4 slots")

	// Check confirmed with 5 slots
	currentSlot = fastStartSlot + 5
	err = tracker.UpdateConfirmations(currentSlot)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(fastTxSig)
	require.NoError(t, err)
	assert.True(t, confirmed, "FAST transaction should be confirmed with 5 slots")

	tx, err := tracker.GetGatewayTransaction(fastTxSig)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)
	assert.Equal(t, uint64(5), tx.Confirmations)

	// Test STANDARD confirmation type (12 blocks)
	standardTxSig := "standardTxSignature456"
	standardStartSlot := uint64(150001000)

	err = tracker.TrackTransaction(
		standardTxSig,
		standardStartSlot,
		"add_funds",
		"84ed4c39500ab38a",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)

	// Check not confirmed with 11 slots
	currentSlot = standardStartSlot + 11
	err = tracker.UpdateConfirmations(currentSlot)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(standardTxSig)
	require.NoError(t, err)
	assert.False(t, confirmed, "STANDARD transaction should not be confirmed with 11 slots")

	// Check confirmed with 12 slots
	currentSlot = standardStartSlot + 12
	err = tracker.UpdateConfirmations(currentSlot)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(standardTxSig)
	require.NoError(t, err)
	assert.True(t, confirmed, "STANDARD transaction should be confirmed with 12 slots")

	tx, err = tracker.GetGatewayTransaction(standardTxSig)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)
	assert.Equal(t, uint64(12), tx.Confirmations)
}

func TestSolanaGatewayHandler_MultipleTransactions(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
	}

	tracker := chaincommon.NewConfirmationTracker(
		database,
		config.BlockConfirmation,
		logger,
	)

	// Track multiple Solana transactions at different slots with mixed confirmation types
	transactions := []struct {
		signature        string
		slot             uint64
		confirmationType string
	}{
		{"sig1_5wHu1qwD7q5ifaN5nwdcDqNFo53GJqa7tkAMzRc", 150000000, "STANDARD"}, // Will have 15 confirmations
		{"sig2_6xIu2rwE8r6jgbO6oxedErOGp64HKrb8ulBNaSD", 150000002, "STANDARD"}, // Will have 13 confirmations
		{"sig3_7yJv3sxF9s7khcP7pyfeF5PHq75ILsc9vmCObTE", 150000005, "FAST"},     // Will have 10 confirmations
		{"sig4_8zKw4tyG0t8lidQ8qzgfG6QIr86JMtd0wnDPcUF", 150000010, "FAST"},     // Will have 5 confirmations
	}

	for _, tx := range transactions {
		err := tracker.TrackTransaction(
			tx.signature,
			tx.slot,
			"add_funds",
			"84ed4c39500ab38a",
			tx.confirmationType,
			nil,
		)
		require.NoError(t, err)
	}

	// Update to slot 150000015
	currentSlot := uint64(150000015)
	err = tracker.UpdateConfirmations(currentSlot)
	require.NoError(t, err)

	// Check confirmations based on type
	expectedResults := []struct {
		signature        string
		confirmations    uint64
		shouldBeConfirmed bool
	}{
		{"sig1_5wHu1qwD7q5ifaN5nwdcDqNFo53GJqa7tkAMzRc", 15, true},  // STANDARD with 15 > 12
		{"sig2_6xIu2rwE8r6jgbO6oxedErOGp64HKrb8ulBNaSD", 13, true},  // STANDARD with 13 > 12
		{"sig3_7yJv3sxF9s7khcP7pyfeF5PHq75ILsc9vmCObTE", 10, true},  // FAST with 10 > 5
		{"sig4_8zKw4tyG0t8lidQ8qzgfG6QIr86JMtd0wnDPcUF", 5, true},   // FAST with 5 = 5
	}

	for i, expected := range expectedResults {
		tx, err := tracker.GetGatewayTransaction(expected.signature)
		require.NoError(t, err)
		assert.Equal(t, expected.confirmations, tx.Confirmations, 
			"Transaction %s (index %d) confirmations mismatch", expected.signature, i)

		confirmed, err := tracker.IsConfirmed(expected.signature)
		require.NoError(t, err)
		assert.Equal(t, expected.shouldBeConfirmed, confirmed, 
			"Transaction %s (index %d) confirmation status mismatch", expected.signature, i)
	}

	// Get all confirmed transactions
	confirmedTxs, err := tracker.GetConfirmedTransactions(config.Chain)
	require.NoError(t, err)
	assert.Equal(t, 4, len(confirmedTxs), "Should have 4 confirmed transactions (2 STANDARD, 2 FAST)")
}

func TestSolanaGatewayHandler_Methods(t *testing.T) {
	// Test that Solana methods are configured properly
	// With the removal of KnownGatewayMethods, we now rely on config
	
	config := &uregistrytypes.ChainConfig{
		Chain:          "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		GatewayAddress: "DZMjJ7hhAB2wmAuRX4sMbYYqDBhFBPzJJ2cWsbTnwQaT",
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "add_funds",
				Identifier:      "84ed4c39500ab38a",
				EventIdentifier: "funds_added",
			},
		},
	}

	// Verify the Solana method configuration
	assert.Len(t, config.GatewayMethods, 1)
	assert.Equal(t, "add_funds", config.GatewayMethods[0].Name)
	assert.Equal(t, "84ed4c39500ab38a", config.GatewayMethods[0].Identifier)
	assert.Equal(t, "funds_added", config.GatewayMethods[0].EventIdentifier)
	
	// Note: Solana uses EventIdentifier differently than EVM
	// It's used for log message pattern matching, not hash-based topics
}

func TestSolanaGatewayHandler_SlotReorg(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
	}

	tracker := chaincommon.NewConfirmationTracker(
		database,
		config.BlockConfirmation,
		logger,
	)

	txSignature := "reorg_5wHu1qwD7q5ifaN5nwdcDqNFo53GJqa7tkAMzRc"
	slotNumber := uint64(150000000)

	// Track transaction
	err = tracker.TrackTransaction(
		txSignature,
		slotNumber,
		"add_funds",
		"84ed4c39500ab38a",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)

	// Finalize it
	err = tracker.UpdateConfirmations(slotNumber+12)
	require.NoError(t, err)

	tx, err := tracker.GetGatewayTransaction(txSignature)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)

	// Simulate reorg - same transaction at different slot
	newSlotNumber := uint64(150000002)
	err = tracker.TrackTransaction(
		txSignature,
		newSlotNumber,
		"add_funds",
		"84ed4c39500ab38a",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)

	// Check it's back to pending
	tx, err = tracker.GetGatewayTransaction(txSignature)
	require.NoError(t, err)
	assert.Equal(t, "pending", tx.Status)
	assert.Equal(t, uint64(0), tx.Confirmations)
}

func TestCrossChainConfirmations(t *testing.T) {
	// Test that EVM and Solana chains use confirmation types correctly
	// Both chains use the same requirements: FAST=5, STANDARD=12
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	// EVM config
	evmConfig := &uregistrytypes.ChainConfig{
		Chain: "eip155:11155111",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
	}

	// Solana config with same requirements (as per business logic)
	solanaConfig := &uregistrytypes.ChainConfig{
		Chain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
	}

	// Create separate trackers
	evmTracker := chaincommon.NewConfirmationTracker(database, evmConfig.BlockConfirmation, logger)
	solanaTracker := chaincommon.NewConfirmationTracker(database, solanaConfig.BlockConfirmation, logger)

	// Track EVM transaction with FAST type
	evmFastTxHash := "0xevmfast123"
	err = evmTracker.TrackTransaction(evmFastTxHash, 1000, "addFunds", "0xf9bfe8a7", "FAST", nil)
	require.NoError(t, err)

	// Track EVM transaction with STANDARD type
	evmStandardTxHash := "0xevmstandard456"
	err = evmTracker.TrackTransaction(evmStandardTxHash, 1000, "addFunds", "0xf9bfe8a7", "STANDARD", nil)
	require.NoError(t, err)

	// Track Solana transaction with FAST type
	solanaFastTxSig := "solanafast789"
	err = solanaTracker.TrackTransaction(solanaFastTxSig, 2000, "add_funds", "84ed4c39500ab38a", "FAST", nil)
	require.NoError(t, err)

	// Track Solana transaction with STANDARD type
	solanaStandardTxSig := "solanastandard012"
	err = solanaTracker.TrackTransaction(solanaStandardTxSig, 2000, "add_funds", "84ed4c39500ab38a", "STANDARD", nil)
	require.NoError(t, err)

	// Test FAST confirmations (5 blocks for both chains)
	// Update EVM to 5 confirmations
	err = evmTracker.UpdateConfirmations(1005)
	require.NoError(t, err)
	
	confirmed, err := evmTracker.IsConfirmed(evmFastTxHash)
	require.NoError(t, err)
	assert.True(t, confirmed, "EVM FAST should be confirmed with 5 blocks")

	confirmed, err = evmTracker.IsConfirmed(evmStandardTxHash)
	require.NoError(t, err)
	assert.False(t, confirmed, "EVM STANDARD should not be confirmed with 5 blocks")

	// Update Solana to 5 confirmations
	err = solanaTracker.UpdateConfirmations(2005)
	require.NoError(t, err)
	
	confirmed, err = solanaTracker.IsConfirmed(solanaFastTxSig)
	require.NoError(t, err)
	assert.True(t, confirmed, "Solana FAST should be confirmed with 5 slots")

	confirmed, err = solanaTracker.IsConfirmed(solanaStandardTxSig)
	require.NoError(t, err)
	assert.False(t, confirmed, "Solana STANDARD should not be confirmed with 5 slots")

	// Test STANDARD confirmations (12 blocks for both chains)
	// Update EVM to 12 confirmations
	err = evmTracker.UpdateConfirmations(1012)
	require.NoError(t, err)
	
	confirmed, err = evmTracker.IsConfirmed(evmStandardTxHash)
	require.NoError(t, err)
	assert.True(t, confirmed, "EVM STANDARD should be confirmed with 12 blocks")

	// Update Solana to 12 confirmations
	err = solanaTracker.UpdateConfirmations(2012)
	require.NoError(t, err)
	
	confirmed, err = solanaTracker.IsConfirmed(solanaStandardTxSig)
	require.NoError(t, err)
	assert.True(t, confirmed, "Solana STANDARD should be confirmed with 12 slots")

	// Verify all transactions are confirmed
	tx, err := evmTracker.GetGatewayTransaction(evmFastTxHash)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)

	tx, err = evmTracker.GetGatewayTransaction(evmStandardTxHash)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)

	tx, err = solanaTracker.GetGatewayTransaction(solanaFastTxSig)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)

	tx, err = solanaTracker.GetGatewayTransaction(solanaStandardTxSig)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)
}