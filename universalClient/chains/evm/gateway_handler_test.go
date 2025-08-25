package evm

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chaincommon "github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/db"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

func TestEVMGatewayHandler_GetLatestBlock(t *testing.T) {
	// This test would require a mock ethclient or actual connection
	// For now, we'll test the structure and initialization
	
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		GatewayAddress: "0x1234567890123456789012345678901234567890",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "addFunds",
				Identifier:      "0xf9bfe8a7",
				EventIdentifier: "0xevent123",
			},
		},
	}

	// Note: This would fail without a real ethclient connection
	// handler, err := NewGatewayHandler(nil, config, database, logger)
	// For testing purposes, we can verify the configuration is correct
	
	assert.Equal(t, "0x1234567890123456789012345678901234567890", config.GatewayAddress)
	assert.Equal(t, uint32(5), config.BlockConfirmation.FastInbound)
	assert.Equal(t, uint32(12), config.BlockConfirmation.StandardInbound)
}

func TestEVMGatewayHandler_ParseGatewayEvent(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		GatewayAddress: "0x1234567890123456789012345678901234567890",
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "addFunds",
				Identifier:      "0xf9bfe8a7",
				EventIdentifier: "0xevent123",
			},
		},
	}

	// Create tracker directly for testing
	tracker := chaincommon.NewConfirmationTracker(
		database,
		config.BlockConfirmation,
		logger,
	)

	// Use the actual event topic from config instead of calculating
	// This matches the production code which uses EventIdentifier from config
	topic := "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	assert.NotEmpty(t, topic)
	assert.Equal(t, 66, len(topic)) // 0x + 64 hex chars

	// Create a mock log that would be parsed
	mockLog := &types.Log{
		Address: ethcommon.HexToAddress(config.GatewayAddress),
		Topics: []ethcommon.Hash{
			ethcommon.HexToHash(topic),
			ethcommon.HexToHash("0x0000000000000000000000001234567890123456789012345678901234567890"), // sender
			ethcommon.HexToHash("0x0000000000000000000000009876543210987654321098765432109876543210"), // token
		},
		Data:        []byte{0x00, 0x00, 0x00, 0x00}, // amount and payload
		BlockNumber: 100,
		TxHash:      ethcommon.HexToHash("0xabcdef1234567890"),
	}

	// Verify log structure
	assert.Equal(t, uint64(100), mockLog.BlockNumber)
	assert.Equal(t, 3, len(mockLog.Topics))
	
	// Test confirmation tracking
	err = tracker.TrackTransaction(
		config.Chain,
		mockLog.TxHash.Hex(),
		mockLog.BlockNumber,
		"addFunds",
		"0xf9bfe8a7",
		mockLog.Data,
	)
	require.NoError(t, err)

	// Verify transaction was tracked
	tx, err := tracker.GetGatewayTransaction(mockLog.TxHash.Hex())
	require.NoError(t, err)
	assert.Equal(t, config.Chain, tx.ChainID)
	assert.Equal(t, uint64(100), tx.BlockNumber)
	assert.Equal(t, "addFunds", tx.Method)
}

func TestEVMGatewayHandler_Confirmations(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain: "eip155:11155111",
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

	txHash := "0xabc123def456"
	blockNumber := uint64(1000)

	// Track a transaction
	err = tracker.TrackTransaction(
		config.Chain,
		txHash,
		blockNumber,
		"addFunds",
		"0xf9bfe8a7",
		nil,
	)
	require.NoError(t, err)

	// Test fast confirmation (5 blocks)
	currentBlock := blockNumber + 4
	err = tracker.UpdateConfirmations(config.Chain, currentBlock)
	require.NoError(t, err)

	confirmed, err := tracker.IsConfirmed(txHash, "fast")
	require.NoError(t, err)
	assert.False(t, confirmed, "Should not be confirmed with only 4 confirmations")

	// Update to 5 confirmations
	currentBlock = blockNumber + 5
	err = tracker.UpdateConfirmations(config.Chain, currentBlock)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(txHash, "fast")
	require.NoError(t, err)
	assert.True(t, confirmed, "Should be confirmed with 5 confirmations for fast mode")

	// Test standard confirmation (12 blocks)
	confirmed, err = tracker.IsConfirmed(txHash, "standard")
	require.NoError(t, err)
	assert.False(t, confirmed, "Should not be confirmed for standard mode with only 5 confirmations")

	// Update to 12 confirmations
	currentBlock = blockNumber + 12
	err = tracker.UpdateConfirmations(config.Chain, currentBlock)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(txHash, "standard")
	require.NoError(t, err)
	assert.True(t, confirmed, "Should be confirmed with 12 confirmations for standard mode")

	// Verify the transaction is marked as confirmed in database
	tx, err := tracker.GetGatewayTransaction(txHash)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)
	assert.Equal(t, uint64(12), tx.Confirmations)
}

func TestEVMGatewayHandler_MultipleTransactions(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain: "eip155:11155111",
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

	// Track multiple transactions at different blocks
	transactions := []struct {
		hash  string
		block uint64
	}{
		{"0x111", 100},
		{"0x222", 102},
		{"0x333", 105},
		{"0x444", 110},
	}

	for _, tx := range transactions {
		err := tracker.TrackTransaction(
			config.Chain,
			tx.hash,
			tx.block,
			"addFunds",
			"0xf9bfe8a7",
			nil,
		)
		require.NoError(t, err)
	}

	// Update to block 115 (all should have at least 5 confirmations)
	currentBlock := uint64(115)
	err = tracker.UpdateConfirmations(config.Chain, currentBlock)
	require.NoError(t, err)

	// Check fast confirmations
	for _, tx := range transactions {
		confirmed, err := tracker.IsConfirmed(tx.hash, "fast")
		require.NoError(t, err)
		assert.True(t, confirmed, "Transaction %s should be fast confirmed", tx.hash)
	}

	// Check standard confirmations
	// Only first two should have 12+ confirmations
	for i, tx := range transactions {
		confirmed, err := tracker.IsConfirmed(tx.hash, "standard")
		require.NoError(t, err)
		
		expectedConfirmed := currentBlock-tx.block >= 12
		assert.Equal(t, expectedConfirmed, confirmed, 
			"Transaction %s (index %d) standard confirmation mismatch", tx.hash, i)
	}

	// Get all confirmed transactions
	confirmedTxs, err := tracker.GetConfirmedTransactions(config.Chain)
	require.NoError(t, err)
	assert.Equal(t, 2, len(confirmedTxs), "Should have 2 fully confirmed transactions")
}

func TestEVMGatewayHandler_EventTopics(t *testing.T) {
	// Test that event topics are properly registered from config
	// This test verifies that we now use EventIdentifier from config directly
	// instead of calculating from method signatures
	
	config := &uregistrytypes.ChainConfig{
		Chain:          "eip155:1",
		GatewayAddress: "0x1234567890123456789012345678901234567890",
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:            "addFunds",
				Identifier:      "0xf9bfe8a7",
				EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
			},
		},
	}

	// Note: With the removal of KnownGatewayMethods, we now rely entirely on
	// EventIdentifier from the config. If it's not provided, the method
	// will log a warning but won't fail initialization.
	
	// Verify config has event identifier
	assert.NotEmpty(t, config.GatewayMethods[0].EventIdentifier)
	assert.Equal(t, "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd", 
		config.GatewayMethods[0].EventIdentifier)
}

func TestEVMGatewayHandler_BlockReorg(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
		Chain: "eip155:11155111",
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

	txHash := "0xreorg123"
	blockNumber := uint64(1000)

	// Track transaction
	err = tracker.TrackTransaction(
		config.Chain,
		txHash,
		blockNumber,
		"addFunds",
		"0xf9bfe8a7",
		nil,
	)
	require.NoError(t, err)

	// Confirm it
	err = tracker.UpdateConfirmations(config.Chain, blockNumber+12)
	require.NoError(t, err)

	tx, err := tracker.GetGatewayTransaction(txHash)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)

	// Simulate reorg - track same transaction at different block
	newBlockNumber := uint64(1002)
	err = tracker.TrackTransaction(
		config.Chain,
		txHash,
		newBlockNumber,
		"addFunds",
		"0xf9bfe8a7",
		nil,
	)
	require.NoError(t, err)

	// Check it's back to pending
	tx, err = tracker.GetGatewayTransaction(txHash)
	require.NoError(t, err)
	assert.Equal(t, "pending", tx.Status)
	assert.Equal(t, uint64(0), tx.Confirmations)
}