package svm

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func TestNewEventListener_Valid(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	methods := []*uregistrytypes.GatewayMethods{
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: "abcdef0123456789",
		},
	}

	el, err := NewEventListener(nil, "GatewayAddr111111111111111111111111111111111", "solana:test", methods, database, 10, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)

	assert.Equal(t, "solana:test", el.chainID)
	assert.Equal(t, "GatewayAddr111111111111111111111111111111111", el.gatewayAddress)
	assert.Equal(t, 10, el.eventPollingSeconds)
	assert.False(t, el.running)
	assert.NotNil(t, el.stopCh)
}

func TestNewEventListener_EmptyGateway(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "", "solana:test", nil, nil, 5, nil, logger)
	assert.Error(t, err)
	assert.Nil(t, el)
	assert.Contains(t, err.Error(), "gateway address not configured")
}

func TestNewEventListener_EmptyChainID(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "", nil, nil, 5, nil, logger)
	assert.Error(t, err)
	assert.Nil(t, el)
	assert.Contains(t, err.Error(), "chain ID not configured")
}

func TestNewEventListener_NilRPCClient(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	assert.Nil(t, el.rpcClient)
}

func TestNewEventListener_NilMethods(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	assert.Empty(t, el.discriminatorToEventType)
}

func TestNewEventListener_DiscriminatorMapping(t *testing.T) {
	logger := zerolog.Nop()

	methods := []*uregistrytypes.GatewayMethods{
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: "AABB0011CCDD2233",
		},
		{
			Name:            EventTypeFinalizeUniversalTx,
			EventIdentifier: "1122334455667788",
		},
		{
			Name:            EventTypeRevertUniversalTx,
			EventIdentifier: "DEADBEEF01234567",
		},
		{
			Name:            "unknown_method", // not a recognized event type
			EventIdentifier: "ffffffffffffffff",
		},
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: "", // empty identifier should be skipped
		},
	}

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", methods, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)

	// Should have 3 entries (unknown_method skipped, empty identifier skipped)
	assert.Len(t, el.discriminatorToEventType, 3)
	assert.Equal(t, EventTypeSendFunds, el.discriminatorToEventType["aabb0011ccdd2233"])
	assert.Equal(t, EventTypeFinalizeUniversalTx, el.discriminatorToEventType["1122334455667788"])
	assert.Equal(t, EventTypeRevertUniversalTx, el.discriminatorToEventType["deadbeef01234567"])
}

func TestNewEventListener_EventStartFrom(t *testing.T) {
	logger := zerolog.Nop()

	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, &startFrom, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	require.NotNil(t, el.eventStartFrom)
	assert.Equal(t, int64(100), *el.eventStartFrom)
}

func TestEventListener_GetPollingInterval(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("positive value returns configured interval", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 15, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, el.getPollingInterval())
	})

	t.Run("zero returns default 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 0, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})

	t.Run("negative returns default 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, -1, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})

	t.Run("one second", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 1, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 1*time.Second, el.getPollingInterval())
	})
}

func TestEventListener_DetermineEventType(t *testing.T) {
	logger := zerolog.Nop()

	// Build a known discriminator: 8 bytes -> hex -> lowercase
	discriminatorBytes := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}
	discriminatorHex := hex.EncodeToString(discriminatorBytes) // "aabbccdd11223344"

	methods := []*uregistrytypes.GatewayMethods{
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: discriminatorHex,
		},
	}

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", methods, nil, 5, nil, logger)
	require.NoError(t, err)

	t.Run("matching discriminator returns event type", func(t *testing.T) {
		payload := append(discriminatorBytes, []byte("extra data here")...)
		encoded := base64.StdEncoding.EncodeToString(payload)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Equal(t, EventTypeSendFunds, eventType)
	})

	t.Run("non-matching discriminator returns empty", func(t *testing.T) {
		otherBytes := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		payload := append(otherBytes, []byte("extra")...)
		encoded := base64.StdEncoding.EncodeToString(payload)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Empty(t, eventType)
	})

	t.Run("no Program data prefix returns empty", func(t *testing.T) {
		eventType := el.determineEventType("Some other log message")
		assert.Empty(t, eventType)
	})

	t.Run("invalid base64 returns empty", func(t *testing.T) {
		eventType := el.determineEventType("Program data: !!!invalid-base64!!!")
		assert.Empty(t, eventType)
	})

	t.Run("payload shorter than 8 bytes returns empty", func(t *testing.T) {
		shortPayload := []byte{0xAA, 0xBB, 0xCC}
		encoded := base64.StdEncoding.EncodeToString(shortPayload)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Empty(t, eventType)
	})

	t.Run("empty log returns empty", func(t *testing.T) {
		eventType := el.determineEventType("")
		assert.Empty(t, eventType)
	})

	t.Run("Program data with empty payload returns empty", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString([]byte{})
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Empty(t, eventType)
	})

	t.Run("exactly 8 bytes matching discriminator", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString(discriminatorBytes)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Equal(t, EventTypeSendFunds, eventType)
	})
}

func TestEventListener_GetStartSlotFromConfig(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("positive eventStartFrom returns that slot", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(5000)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		slot, err := el.getStartSlotFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(5000), slot)
	})

	t.Run("zero eventStartFrom returns 0", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(0)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		slot, err := el.getStartSlotFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(0), slot)
	})

	t.Run("large positive eventStartFrom", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(999999999)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		slot, err := el.getStartSlotFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(999999999), slot)
	})

	t.Run("minus one eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(-1)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		// rpcClient is nil, so calling GetLatestSlot panics
		assert.Panics(t, func() {
			el.getStartSlotFromConfig(context.Background())
		})
	})

	t.Run("nil eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, nil, logger)
		require.NoError(t, err)

		// nil rpcClient, nil eventStartFrom -> falls through to rpcClient.GetLatestSlot which panics
		assert.Panics(t, func() {
			el.getStartSlotFromConfig(context.Background())
		})
	})

	t.Run("negative value less than -1 with nil rpcClient panics", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(-5)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		// -5 is < 0 but not -1, falls through to rpcClient.GetLatestSlot which panics
		assert.Panics(t, func() {
			el.getStartSlotFromConfig(context.Background())
		})
	})
}

func TestEventListener_StopNotRunning(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)

	err = el.Stop()
	assert.NoError(t, err)
}

func TestEventListener_IsRunning_InitiallyFalse(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)

	assert.False(t, el.IsRunning())
}

func TestEventListener_StopTwice(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)

	assert.NoError(t, el.Stop())
	assert.NoError(t, el.Stop())
}

func TestEventListener_StartStop_ContextCancel(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	// Use eventStartFrom to avoid rpcClient calls in getStartSlot
	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 1, &startFrom, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	cancel()

	done := make(chan struct{})
	go func() {
		el.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("event listener did not stop after context cancellation")
	}
}

func TestEventListener_StartStop_StopMethod(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 1, &startFrom, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	done := make(chan struct{})
	go func() {
		stopErr := el.Stop()
		assert.NoError(t, stopErr)
		close(done)
	}()

	select {
	case <-done:
		assert.False(t, el.IsRunning())
	case <-time.After(5 * time.Second):
		t.Fatal("event listener did not stop after Stop() call")
	}
}

func TestEventListener_StartWhileRunning(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 1, &startFrom, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = el.Start(ctx)
	require.NoError(t, err)

	// Starting again while running should return an error
	err = el.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Clean up
	cancel()
	el.wg.Wait()
}
