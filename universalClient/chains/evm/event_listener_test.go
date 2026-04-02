package evm

import (
	"context"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// helper to create an in-memory DB for tests
func testDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

func testLogger(t *testing.T) zerolog.Logger {
	t.Helper()
	return zerolog.New(zerolog.NewTestWriter(t))
}
func TestNewEventListener_EmptyGatewayAddress(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "", "", "eip155:1", nil, nil, database, 5, nil, logger)
	assert.Nil(t, el)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gateway address not configured")
}

func TestNewEventListener_EmptyChainID(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "", "", nil, nil, database, 5, nil, logger)
	assert.Nil(t, el)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain ID not configured")
}

func TestNewEventListener_ValidCreation(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 10, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)

	assert.Equal(t, "0xGateway", el.gatewayAddress)
	assert.Equal(t, "0xVault", el.vaultAddress)
	assert.Equal(t, "eip155:1", el.chainID)
	assert.Equal(t, 10, el.eventPollingSeconds)
	assert.NotNil(t, el.database)
	assert.NotNil(t, el.chainStore)
	assert.NotNil(t, el.stopCh)
	assert.False(t, el.running)
}

func TestNewEventListener_NilDatabaseAllowed(t *testing.T) {
	// NewEventListener does not validate database being nil; it passes it through
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	assert.Nil(t, el.database)
}
func TestEventListener_IsRunning_DefaultFalse(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.False(t, el.IsRunning())
}

func TestEventListener_StartSetsRunning(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	// Use no topics so the listen goroutine exits early (before hitting nil rpcClient)
	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	// The goroutine will exit quickly because there are no event topics configured.
	time.Sleep(50 * time.Millisecond)

	// Stop should work cleanly
	err = el.Stop()
	assert.NoError(t, err)
	assert.False(t, el.IsRunning())
}

func TestEventListener_DoubleStartReturnsError(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = el.Start(ctx)
	require.NoError(t, err)

	err = el.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// cleanup
	time.Sleep(50 * time.Millisecond)
	el.Stop()
}

func TestEventListener_StopWhenNotRunning(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)

	// Stop on a listener that was never started should be a no-op
	err = el.Stop()
	assert.NoError(t, err)
	assert.False(t, el.IsRunning())
}

func TestEventListener_StartStopStart(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// First start (no topics, so goroutine exits immediately)
	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	time.Sleep(50 * time.Millisecond)
	err = el.Stop()
	require.NoError(t, err)
	assert.False(t, el.IsRunning())

	// Second start after stop should work
	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	time.Sleep(50 * time.Millisecond)
	el.Stop()
}
func TestNewEventListener_TopicMapFromGatewayMethods(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	sendFundsTopicHex := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	executeTxTopicHex := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	revertTxTopicHex := "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, Identifier: "sendFunds()", EventIdentifier: sendFundsTopicHex},
		{Name: EventTypeExecuteUniversalTx, Identifier: "executeUniversalTx()", EventIdentifier: executeTxTopicHex},
		{Name: EventTypeRevertUniversalTx, Identifier: "revertUniversalTx()", EventIdentifier: revertTxTopicHex},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 3)
	assert.Len(t, el.topicToEventType, 3)

	// Verify each topic maps to the correct event type
	assert.Equal(t, EventTypeSendFunds, el.topicToEventType[ethcommon.HexToHash(sendFundsTopicHex)])
	assert.Equal(t, EventTypeExecuteUniversalTx, el.topicToEventType[ethcommon.HexToHash(executeTxTopicHex)])
	assert.Equal(t, EventTypeRevertUniversalTx, el.topicToEventType[ethcommon.HexToHash(revertTxTopicHex)])
}

func TestNewEventListener_TopicMapFromVaultMethods(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	finalizeTxTopicHex := "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	vaultMethods := []*uregistrytypes.VaultMethods{
		{Name: EventTypeFinalizeUniversalTx, Identifier: "finalizeUniversalTx()", EventIdentifier: finalizeTxTopicHex},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, vaultMethods, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 1)
	assert.Equal(t, EventTypeFinalizeUniversalTx, el.topicToEventType[ethcommon.HexToHash(finalizeTxTopicHex)])
}

func TestNewEventListener_TopicMapCombined(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	sendFundsTopicHex := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	finalizeTxTopicHex := "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, Identifier: "sendFunds()", EventIdentifier: sendFundsTopicHex},
	}
	vaultMethods := []*uregistrytypes.VaultMethods{
		{Name: EventTypeFinalizeUniversalTx, Identifier: "finalizeUniversalTx()", EventIdentifier: finalizeTxTopicHex},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, vaultMethods, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 2)
	assert.Len(t, el.topicToEventType, 2)

	assert.Equal(t, EventTypeSendFunds, el.topicToEventType[ethcommon.HexToHash(sendFundsTopicHex)])
	assert.Equal(t, EventTypeFinalizeUniversalTx, el.topicToEventType[ethcommon.HexToHash(finalizeTxTopicHex)])
}

func TestNewEventListener_EmptyEventIdentifierSkipped(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, Identifier: "sendFunds()", EventIdentifier: ""}, // empty => skip
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 0)
	assert.Len(t, el.topicToEventType, 0)
}

func TestNewEventListener_UnknownMethodNameSkipped(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: "unknownMethod", Identifier: "unknown()", EventIdentifier: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
	}
	vaultMethods := []*uregistrytypes.VaultMethods{
		{Name: "unknownVaultMethod", Identifier: "unknown()", EventIdentifier: "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, vaultMethods, database, 5, nil, logger)
	require.NoError(t, err)

	// Unknown names should not be added to topic map
	assert.Len(t, el.eventTopics, 0)
	assert.Len(t, el.topicToEventType, 0)
}

func TestNewEventListener_NoMethodsProducesEmptyTopics(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 0)
	assert.Len(t, el.topicToEventType, 0)
}
func TestEventListener_GetPollingInterval(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	t.Run("Custom interval", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 15, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, el.getPollingInterval())
	})

	t.Run("Zero defaults to 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 0, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})

	t.Run("Negative defaults to 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, -1, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})
}
func TestNewEventListener_EventStartFromStored(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	startBlock := int64(12345)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)
	require.NotNil(t, el.eventStartFrom)
	assert.Equal(t, int64(12345), *el.eventStartFrom)
}

func TestNewEventListener_EventStartFromNil(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)
	assert.Nil(t, el.eventStartFrom)
}
func TestEventListener_GetStartBlockFromConfig(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	t.Run("positive eventStartFrom returns that block", func(t *testing.T) {
		startBlock := int64(5000)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		block, err := el.getStartBlockFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(5000), block)
	})

	t.Run("zero eventStartFrom returns 0", func(t *testing.T) {
		startBlock := int64(0)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		block, err := el.getStartBlockFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(0), block)
	})

	t.Run("large positive eventStartFrom", func(t *testing.T) {
		startBlock := int64(999999999)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		block, err := el.getStartBlockFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(999999999), block)
	})

	t.Run("minus one eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		startBlock := int64(-1)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		// rpcClient is nil, so calling GetLatestBlock panics
		assert.Panics(t, func() {
			el.getStartBlockFromConfig(context.Background())
		})
	})

	t.Run("nil eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
		require.NoError(t, err)

		// nil rpcClient, nil eventStartFrom -> falls through to rpcClient.GetLatestBlock which panics on nil
		assert.Panics(t, func() {
			el.getStartBlockFromConfig(context.Background())
		})
	})

	t.Run("negative value less than -1 with nil rpcClient panics", func(t *testing.T) {
		startBlock := int64(-5)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		// -5 is < 0 but not -1, and not >= 0, so falls through to rpcClient.GetLatestBlock
		assert.Panics(t, func() {
			el.getStartBlockFromConfig(context.Background())
		})
	})
}

func TestEventListener_ContextCancellationStopsGoroutine(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	// Use no topics so the goroutine exits at the "no event topics" warning
	// before trying to use nil rpcClient. We can still verify context flow.
	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	// Cancel context
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Stop to clean up
	el.Stop()
	assert.False(t, el.IsRunning())
}
