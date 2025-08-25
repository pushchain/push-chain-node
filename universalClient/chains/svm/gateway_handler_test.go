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
		config.Chain,
		txSignature,
		slotNumber,
		"add_funds",
		"84ed4c39500ab38a",
		nil,
	)
	require.NoError(t, err)

	// Verify transaction was tracked
	tx, err := tracker.GetGatewayTransaction(txSignature)
	require.NoError(t, err)
	assert.Equal(t, config.Chain, tx.ChainID)
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

	// Solana uses slots instead of blocks
	txSignature := "testSignature123"
	startSlot := uint64(150000000)

	// Track transaction
	err = tracker.TrackTransaction(
		config.Chain,
		txSignature,
		startSlot,
		"add_funds",
		"84ed4c39500ab38a",
		nil,
	)
	require.NoError(t, err)

	// Test Solana confirmation levels
	// In Solana: processed=1, confirmed=5, finalized=12 (mapped values)
	
	// Simulate "processed" level (1 confirmation)
	currentSlot := startSlot + 1
	err = tracker.UpdateConfirmations(config.Chain, currentSlot)
	require.NoError(t, err)

	tx, err := tracker.GetGatewayTransaction(txSignature)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), tx.Confirmations)
	
	// Not yet "confirmed" in Solana terms (needs 5)
	confirmed, err := tracker.IsConfirmed(txSignature, "fast")
	require.NoError(t, err)
	assert.False(t, confirmed)

	// Simulate "confirmed" level (5 confirmations)
	currentSlot = startSlot + 5
	err = tracker.UpdateConfirmations(config.Chain, currentSlot)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(txSignature, "fast")
	require.NoError(t, err)
	assert.True(t, confirmed, "Should be confirmed at Solana 'confirmed' level")

	// But not "finalized" yet
	confirmed, err = tracker.IsConfirmed(txSignature, "standard")
	require.NoError(t, err)
	assert.False(t, confirmed, "Should not be finalized yet")

	// Simulate "finalized" level (12 confirmations)
	currentSlot = startSlot + 12
	err = tracker.UpdateConfirmations(config.Chain, currentSlot)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(txSignature, "standard")
	require.NoError(t, err)
	assert.True(t, confirmed, "Should be finalized")

	// Verify status in database
	tx, err = tracker.GetGatewayTransaction(txSignature)
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

	// Track multiple Solana transactions at different slots
	transactions := []struct {
		signature string
		slot      uint64
	}{
		{"sig1_5wHu1qwD7q5ifaN5nwdcDqNFo53GJqa7tkAMzRc", 150000000},
		{"sig2_6xIu2rwE8r6jgbO6oxedErOGp64HKrb8ulBNaSD", 150000002},
		{"sig3_7yJv3sxF9s7khcP7pyfeF5PHq75ILsc9vmCObTE", 150000005},
		{"sig4_8zKw4tyG0t8lidQ8qzgfG6QIr86JMtd0wnDPcUF", 150000010},
	}

	for _, tx := range transactions {
		err := tracker.TrackTransaction(
			config.Chain,
			tx.signature,
			tx.slot,
			"add_funds",
			"84ed4c39500ab38a",
			nil,
		)
		require.NoError(t, err)
	}

	// Update to slot 150000015 (all should have at least 5 confirmations)
	currentSlot := uint64(150000015)
	err = tracker.UpdateConfirmations(config.Chain, currentSlot)
	require.NoError(t, err)

	// Check fast confirmations (Solana "confirmed" level)
	for _, tx := range transactions {
		confirmed, err := tracker.IsConfirmed(tx.signature, "fast")
		require.NoError(t, err)
		assert.True(t, confirmed, "Transaction %s should be fast confirmed", tx.signature)
	}

	// Check standard confirmations (Solana "finalized" level)
	for i, tx := range transactions {
		confirmed, err := tracker.IsConfirmed(tx.signature, "standard")
		require.NoError(t, err)
		
		expectedConfirmed := currentSlot-tx.slot >= 12
		assert.Equal(t, expectedConfirmed, confirmed, 
			"Transaction %s (index %d) standard confirmation mismatch", tx.signature, i)
	}

	// Get all confirmed transactions
	confirmedTxs, err := tracker.GetConfirmedTransactions(config.Chain)
	require.NoError(t, err)
	assert.Equal(t, 2, len(confirmedTxs), "Should have 2 fully finalized transactions")
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
		config.Chain,
		txSignature,
		slotNumber,
		"add_funds",
		"84ed4c39500ab38a",
		nil,
	)
	require.NoError(t, err)

	// Finalize it
	err = tracker.UpdateConfirmations(config.Chain, slotNumber+12)
	require.NoError(t, err)

	tx, err := tracker.GetGatewayTransaction(txSignature)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)

	// Simulate reorg - same transaction at different slot
	newSlotNumber := uint64(150000002)
	err = tracker.TrackTransaction(
		config.Chain,
		txSignature,
		newSlotNumber,
		"add_funds",
		"84ed4c39500ab38a",
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
	// Test that EVM and Solana chains can have different confirmation requirements
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

	// Solana config with different requirements
	solanaConfig := &uregistrytypes.ChainConfig{
		Chain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     3,  // Different from EVM
			StandardInbound: 10, // Different from EVM
		},
	}

	// Create separate trackers
	evmTracker := chaincommon.NewConfirmationTracker(database, evmConfig.BlockConfirmation, logger)
	solanaTracker := chaincommon.NewConfirmationTracker(database, solanaConfig.BlockConfirmation, logger)

	// Track EVM transaction
	evmTxHash := "0xevm123"
	err = evmTracker.TrackTransaction(evmConfig.Chain, evmTxHash, 1000, "addFunds", "0xf9bfe8a7", nil)
	require.NoError(t, err)

	// Track Solana transaction
	solanaTxSig := "solana456"
	err = solanaTracker.TrackTransaction(solanaConfig.Chain, solanaTxSig, 2000, "add_funds", "84ed4c39500ab38a", nil)
	require.NoError(t, err)

	// Update confirmations for EVM (needs 5 for fast)
	err = evmTracker.UpdateConfirmations(evmConfig.Chain, 1004)
	require.NoError(t, err)
	
	confirmed, err := evmTracker.IsConfirmed(evmTxHash, "fast")
	require.NoError(t, err)
	assert.False(t, confirmed, "EVM should not be fast confirmed with 4 confirmations")

	// Update confirmations for Solana (needs only 3 for fast)
	err = solanaTracker.UpdateConfirmations(solanaConfig.Chain, 2003)
	require.NoError(t, err)
	
	confirmed, err = solanaTracker.IsConfirmed(solanaTxSig, "fast")
	require.NoError(t, err)
	assert.True(t, confirmed, "Solana should be fast confirmed with 3 confirmations")

	// Update EVM to 5 confirmations
	err = evmTracker.UpdateConfirmations(evmConfig.Chain, 1005)
	require.NoError(t, err)
	
	confirmed, err = evmTracker.IsConfirmed(evmTxHash, "fast")
	require.NoError(t, err)
	assert.True(t, confirmed, "EVM should now be fast confirmed with 5 confirmations")

	// Check standard confirmations
	err = evmTracker.UpdateConfirmations(evmConfig.Chain, 1012)
	require.NoError(t, err)
	
	confirmed, err = evmTracker.IsConfirmed(evmTxHash, "standard")
	require.NoError(t, err)
	assert.True(t, confirmed, "EVM should be standard confirmed with 12 confirmations")

	err = solanaTracker.UpdateConfirmations(solanaConfig.Chain, 2010)
	require.NoError(t, err)
	
	confirmed, err = solanaTracker.IsConfirmed(solanaTxSig, "standard")
	require.NoError(t, err)
	assert.True(t, confirmed, "Solana should be standard confirmed with 10 confirmations")
}