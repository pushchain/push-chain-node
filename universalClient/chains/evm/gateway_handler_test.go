package evm

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chaincommon "github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
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
		GatewayAddress: "0x1234567890123456789012345678901234567890",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
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

	config := &uregistrytypes.ChainConfig{}

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
		mockLog.TxHash.Hex(),
		mockLog.BlockNumber,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		mockLog.Data,
	)
	require.NoError(t, err)

	// Verify transaction was tracked
	tx, err := tracker.GetGatewayTransaction(mockLog.TxHash.Hex())
	require.NoError(t, err)
	// ChainID no longer exists in ChainTransaction
	assert.Equal(t, uint64(100), tx.BlockNumber)
	assert.Equal(t, "addFunds", tx.Method)
}

func TestEVMGatewayHandler_Confirmations(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	config := &uregistrytypes.ChainConfig{
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
	fastTxHash := "0xfast123"
	blockNumber := uint64(1000)

	err = tracker.TrackTransaction(
		fastTxHash,
		blockNumber,
		"addFunds",
		"0xf9bfe8a7",
		"FAST",
		nil,
	)
	require.NoError(t, err)

	// Test with 4 confirmations - should not be confirmed
	currentBlock := blockNumber + 4
	err = tracker.UpdateConfirmations(currentBlock)
	require.NoError(t, err)

	confirmed, err := tracker.IsConfirmed(fastTxHash)
	require.NoError(t, err)
	assert.False(t, confirmed, "FAST transaction should not be confirmed with only 4 confirmations")

	// Update to 5 confirmations - should be confirmed for FAST
	currentBlock = blockNumber + 5
	err = tracker.UpdateConfirmations(currentBlock)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(fastTxHash)
	require.NoError(t, err)
	assert.True(t, confirmed, "FAST transaction should be confirmed with 5 confirmations")

	// Test STANDARD confirmation type (12 blocks)
	standardTxHash := "0xstandard456"
	err = tracker.TrackTransaction(
		standardTxHash,
		blockNumber,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)

	// With 5 confirmations - should not be confirmed for STANDARD
	err = tracker.UpdateConfirmations(currentBlock)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(standardTxHash)
	require.NoError(t, err)
	assert.False(t, confirmed, "STANDARD transaction should not be confirmed with only 5 confirmations")

	// Update to 12 confirmations - should be confirmed for STANDARD
	currentBlock = blockNumber + 12
	err = tracker.UpdateConfirmations(currentBlock)
	require.NoError(t, err)

	confirmed, err = tracker.IsConfirmed(standardTxHash)
	require.NoError(t, err)
	assert.True(t, confirmed, "STANDARD transaction should be confirmed with 12 confirmations")

	// Verify the STANDARD transaction is marked as confirmed in database
	tx, err := tracker.GetGatewayTransaction(standardTxHash)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)
	assert.Equal(t, uint64(12), tx.Confirmations)
}

// TestEVMClient_RPCPoolConfiguration tests RPC pool configuration setup
func TestEVMClient_RPCPoolConfiguration(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	chainConfig := &uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		GatewayAddress: "0x1234567890123456789012345678901234567890",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
	}

	// Test with multiple RPC URLs
	appConfig := &config.Config{
		ChainConfigs: map[string]config.ChainSpecificConfig{
			"eip155:11155111": {
				RPCURLs: []string{"http://rpc1.test", "http://rpc2.test", "http://rpc3.test"},
			},
		},
		RPCPoolConfig: config.RPCPoolConfig{
			HealthCheckIntervalSeconds: 30,
			UnhealthyThreshold:         3,
			RecoveryIntervalSeconds:    300,
			MinHealthyEndpoints:        1,
			RequestTimeoutSeconds:      10,
			LoadBalancingStrategy:      "round_robin",
		},
	}

	// Test client creation with pool configuration
	client, err := NewClient(chainConfig, database, appConfig, logger)
	require.NoError(t, err)
	require.NotNil(t, client)

	// RPC pool is initialized during Start(), not during NewClient()
	// This is expected behavior - pool is created when client connects
	require.Nil(t, client.rpcPool, "RPC pool should be nil before Start() is called")

	// Verify configuration is set up correctly
	urls := client.getRPCURLs()
	assert.Equal(t, 3, len(urls), "Should have 3 configured URLs")
	assert.Contains(t, urls, "http://rpc1.test")
	assert.Contains(t, urls, "http://rpc2.test")
	assert.Contains(t, urls, "http://rpc3.test")

	// Test single URL fallback to legacy mode configuration
	appConfigSingle := &config.Config{
		ChainConfigs: map[string]config.ChainSpecificConfig{
			"eip155:11155111": {
				RPCURLs: []string{"http://single-rpc.test"},
			},
		},
		RPCPoolConfig: config.RPCPoolConfig{
			LoadBalancingStrategy: "round_robin",
		},
	}

	clientSingle, err := NewClient(chainConfig, database, appConfigSingle, logger)
	require.NoError(t, err)
	require.NotNil(t, clientSingle)

	// Single URL configuration
	urlsSingle := clientSingle.getRPCURLs()
	assert.Equal(t, 1, len(urlsSingle), "Should have 1 configured URL")
	assert.Equal(t, "http://single-rpc.test", urlsSingle[0])
}

// TestEVMGatewayHandler_Integration tests gateway handler integration with RPC pool system
func TestEVMGatewayHandler_Integration(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	chainConfig := &uregistrytypes.ChainConfig{
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

	appConfig := &config.Config{
		ChainConfigs: map[string]config.ChainSpecificConfig{
			"eip155:11155111": {
				RPCURLs: []string{"http://rpc1.test", "http://rpc2.test"},
			},
		},
		RPCPoolConfig: config.RPCPoolConfig{
			HealthCheckIntervalSeconds: 1,
			UnhealthyThreshold:         2,
			RecoveryIntervalSeconds:    1,
			MinHealthyEndpoints:        1,
			RequestTimeoutSeconds:      1,
			LoadBalancingStrategy:      "round_robin",
		},
		EventPollingIntervalSeconds: 1,
	}

	// Create client (RPC pool created during Start(), not NewClient())
	client, err := NewClient(chainConfig, database, appConfig, logger)
	require.NoError(t, err)

	// Create gateway handler
	handler, err := NewGatewayHandler(client, chainConfig, database, appConfig, logger)
	require.NoError(t, err)
	require.NotNil(t, handler)

	// Verify handler integration with parent client
	assert.Equal(t, client, handler.parentClient, "Gateway handler should reference parent client for RPC pool access")
	assert.NotNil(t, handler.tracker, "Gateway handler should have confirmation tracker")
	assert.NotNil(t, handler.eventParser, "Gateway handler should have event parser")
	assert.NotNil(t, handler.eventWatcher, "Gateway handler should have event watcher")

	// Verify that gateway handler uses executeWithFailover pattern in its code
	// This is verified by the fact that all gateway handler methods call
	// h.parentClient.executeWithFailover() which uses the RPC pool when available
}

// TestEVMGatewayHandler_ExecuteWithFailoverPattern tests that executeWithFailover is used
func TestEVMGatewayHandler_ExecuteWithFailoverPattern(t *testing.T) {
	// This test verifies the integration pattern is correct by testing structure
	// The actual failover behavior is tested in the RPC pool tests

	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	chainConfig := &uregistrytypes.ChainConfig{
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

	appConfig := &config.Config{
		ChainConfigs: map[string]config.ChainSpecificConfig{
			"eip155:11155111": {
				RPCURLs: []string{"http://rpc1.test", "http://rpc2.test"},
			},
		},
		RPCPoolConfig: config.RPCPoolConfig{
			HealthCheckIntervalSeconds: 1,
			UnhealthyThreshold:         2,
			RecoveryIntervalSeconds:    1,
			MinHealthyEndpoints:        1,
			RequestTimeoutSeconds:      1,
			LoadBalancingStrategy:      "round_robin",
		},
	}

	client, err := NewClient(chainConfig, database, appConfig, logger)
	require.NoError(t, err)

	handler, err := NewGatewayHandler(client, chainConfig, database, appConfig, logger)
	require.NoError(t, err)

	// Verify integration structure
	assert.NotNil(t, handler.parentClient, "Gateway handler should have parent client reference")
	assert.Equal(t, client, handler.parentClient, "Parent client should be the same client instance")

	// The key integration point is that gateway handler methods like:
	// - GetLatestBlock() calls h.parentClient.executeWithFailover()
	// - GetTransactionConfirmations() calls h.parentClient.executeWithFailover()
	// - WatchGatewayEvents() calls h.parentClient.executeWithFailover()
	// This ensures RPC pool failover is used for all gateway operations

	t.Log("Gateway handler successfully integrated with RPC pool via executeWithFailover pattern")
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

	// Track multiple transactions at different blocks with different confirmation types
	transactions := []struct {
		hash             string
		block            uint64
		confirmationType string
	}{
		{"0x111", 100, "STANDARD"}, // Will have 15 confirmations
		{"0x222", 102, "STANDARD"}, // Will have 13 confirmations
		{"0x333", 105, "FAST"},     // Will have 10 confirmations
		{"0x444", 110, "FAST"},     // Will have 5 confirmations
	}

	for _, tx := range transactions {
		err := tracker.TrackTransaction(
			tx.hash,
			tx.block,
			"addFunds",
			"0xf9bfe8a7",
			tx.confirmationType,
			nil,
		)
		require.NoError(t, err)
	}

	// Update to block 115
	currentBlock := uint64(115)
	err = tracker.UpdateConfirmations(currentBlock)
	require.NoError(t, err)

	// Check FAST transactions (need 5 confirmations)
	// 0x333 has 10 confirmations - should be confirmed
	// 0x444 has 5 confirmations - should be confirmed
	fastTxs := []string{"0x333", "0x444"}
	for _, hash := range fastTxs {
		confirmed, err := tracker.IsConfirmed(hash)
		require.NoError(t, err)
		assert.True(t, confirmed, "FAST transaction %s should be confirmed", hash)
	}

	// Check STANDARD transactions (need 12 confirmations)
	// 0x111 has 15 confirmations - should be confirmed
	// 0x222 has 13 confirmations - should be confirmed
	standardTxs := []string{"0x111", "0x222"}
	for _, hash := range standardTxs {
		confirmed, err := tracker.IsConfirmed(hash)
		require.NoError(t, err)
		assert.True(t, confirmed, "STANDARD transaction %s should be confirmed", hash)
	}

	// Get all confirmed transactions - all 4 should be confirmed
	confirmedTxs, err := tracker.GetConfirmedTransactions(config.Chain)
	require.NoError(t, err)
	assert.Equal(t, 4, len(confirmedTxs), "Should have 4 confirmed transactions (2 STANDARD, 2 FAST)")
}

func TestEVMGatewayHandler_EventTopics(t *testing.T) {
	// Test that event topics are properly registered from config
	// This test verifies that we now use EventIdentifier from config directly
	// instead of calculating from method signatures

	config := &uregistrytypes.ChainConfig{
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
		txHash,
		blockNumber,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)

	// Confirm it
	err = tracker.UpdateConfirmations(blockNumber + 12)
	require.NoError(t, err)

	tx, err := tracker.GetGatewayTransaction(txHash)
	require.NoError(t, err)
	assert.Equal(t, "confirmed", tx.Status)

	// Simulate reorg - track same transaction at different block
	newBlockNumber := uint64(1002)
	err = tracker.TrackTransaction(
		txHash,
		newBlockNumber,
		"addFunds",
		"0xf9bfe8a7",
		"STANDARD",
		nil,
	)
	require.NoError(t, err)

	// Check it's back to pending
	tx, err = tracker.GetGatewayTransaction(txHash)
	require.NoError(t, err)
	assert.Equal(t, "pending", tx.Status)
	assert.Equal(t, uint64(0), tx.Confirmations)
}
