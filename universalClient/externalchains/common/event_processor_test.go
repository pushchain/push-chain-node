package common

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ucdb "github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

type fakeVoteSigner struct {
	inboundVotes  int
	outboundVotes int
	txHash        string
	err           error
}

func (f *fakeVoteSigner) VoteInbound(ctx context.Context, inbound *uexecutortypes.Inbound) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.inboundVotes++
	return f.txHash, nil
}

func (f *fakeVoteSigner) VoteOutbound(ctx context.Context, txID string, utxID string, observation *uexecutortypes.OutboundObservation) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.outboundVotes++
	return f.txHash, nil
}

type fakeEventHandler struct {
	handled []string
	err     error
}

func (f *fakeEventHandler) HandleEvent(ctx context.Context, event *store.Event) error {
	f.handled = append(f.handled, event.EventID)
	return f.err
}

func newTestDB(t *testing.T) *ucdb.DB {
	t.Helper()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func seedConfirmedEvent(t *testing.T, database *ucdb.DB, eventID, eventType string, eventData []byte) {
	t.Helper()
	result := database.Client().Create(&store.Event{
		EventID:          eventID,
		Type:             eventType,
		ConfirmationType: store.ConfirmationInstant,
		Status:           store.StatusConfirmed,
		EventData:        eventData,
	})
	require.NoError(t, result.Error)
}

func TestNewEventProcessor(t *testing.T) {
	processor := NewEventProcessor(nil, "eip155:1", zerolog.Nop())

	require.NotNil(t, processor)
	assert.Equal(t, "eip155:1", processor.chainID)
	assert.False(t, processor.running)
	assert.NotNil(t, processor.stopCh)
	assert.NotNil(t, processor.chainStore)
	assert.Empty(t, processor.handlers)
}

func TestEventProcessor_DispatchesByType(t *testing.T) {
	database := newTestDB(t)
	ep := NewEventProcessor(database, "eip155:1", zerolog.Nop())

	inboundHandler := &fakeEventHandler{}
	ep.RegisterHandler(store.EventTypeInbound, inboundHandler)

	seedConfirmedEvent(t, database, "0xin:0", store.EventTypeInbound, []byte("{}"))
	seedConfirmedEvent(t, database, "0xout:0", store.EventTypeOutbound, []byte("{}")) // no handler registered

	require.NoError(t, ep.processConfirmedEvents(context.Background()))

	assert.Equal(t, []string{"0xin:0"}, inboundHandler.handled)
}

func TestEventProcessor_HandlerErrorKeepsProcessing(t *testing.T) {
	database := newTestDB(t)
	ep := NewEventProcessor(database, "eip155:1", zerolog.Nop())

	failing := &fakeEventHandler{err: fmt.Errorf("boom")}
	ep.RegisterHandler(store.EventTypeInbound, failing)

	seedConfirmedEvent(t, database, "0xin:0", store.EventTypeInbound, []byte("{}"))
	seedConfirmedEvent(t, database, "0xin:1", store.EventTypeInbound, []byte("{}"))

	require.NoError(t, ep.processConfirmedEvents(context.Background()))

	// both attempted despite errors, both still CONFIRMED for retry
	assert.Len(t, failing.handled, 2)
	events, err := NewChainStore(database).GetConfirmedEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 2)
}

func TestEventProcessor_PendingEventsIgnored(t *testing.T) {
	database := newTestDB(t)
	ep := NewEventProcessor(database, "eip155:1", zerolog.Nop())

	handler := &fakeEventHandler{}
	ep.RegisterHandler(store.EventTypeInbound, handler)

	result := database.Client().Create(&store.Event{
		EventID:          "0xpending:0",
		Type:             store.EventTypeInbound,
		ConfirmationType: store.ConfirmationInstant,
		Status:           store.StatusPending,
		EventData:        []byte("{}"),
	})
	require.NoError(t, result.Error)

	require.NoError(t, ep.processConfirmedEvents(context.Background()))

	assert.Empty(t, handler.handled)
}

func TestEventProcessor_NilDatabaseErrors(t *testing.T) {
	ep := NewEventProcessor(nil, "eip155:1", zerolog.Nop())
	ep.RegisterHandler(store.EventTypeInbound, &fakeEventHandler{})

	err := ep.processConfirmedEvents(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get confirmed events")
}

func TestEventProcessor_Lifecycle(t *testing.T) {
	database := newTestDB(t)
	ep := NewEventProcessor(database, "eip155:1", zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// initial state
	assert.False(t, ep.IsRunning())

	// start
	require.NoError(t, ep.Start(ctx))
	assert.True(t, ep.IsRunning())

	// double start rejected
	err := ep.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// stop, idempotent
	require.NoError(t, ep.Stop())
	assert.False(t, ep.IsRunning())
	require.NoError(t, ep.Stop())

	// restart works
	require.NoError(t, ep.Start(ctx))
	assert.True(t, ep.IsRunning())
	require.NoError(t, ep.Stop())
}

func TestEventProcessor_StopViaContextCancel(t *testing.T) {
	database := newTestDB(t)
	ep := NewEventProcessor(database, "eip155:1", zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, ep.Start(ctx))
	assert.True(t, ep.IsRunning())

	cancel()

	done := make(chan struct{})
	go func() {
		_ = ep.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("processLoop did not exit after context cancellation")
	}
	assert.False(t, ep.IsRunning())
}

func TestBase58ToHex(t *testing.T) {
	t.Run("empty string returns 0x", func(t *testing.T) {
		result, err := base58ToHex("")
		require.NoError(t, err)
		assert.Equal(t, "0x", result)
	})

	t.Run("already hex returns as is", func(t *testing.T) {
		input := "0xabcdef1234567890"
		result, err := base58ToHex(input)
		require.NoError(t, err)
		assert.Equal(t, input, result)
	})

	t.Run("valid base58 converts to hex", func(t *testing.T) {
		result, err := base58ToHex("2VfUX")
		require.NoError(t, err)
		assert.True(t, len(result) > 2)
		assert.Equal(t, "0x", result[:2])
	})

	t.Run("invalid base58 returns error", func(t *testing.T) {
		// Base58 doesn't include 0, O, I, l
		_, err := base58ToHex("0OIl")
		require.Error(t, err)
	})
}

func TestEventTxHash(t *testing.T) {
	t.Run("hex event id", func(t *testing.T) {
		assert.Equal(t, "0xabc123", eventTxHash("0xabc123:5"))
	})

	t.Run("base58 event id converts", func(t *testing.T) {
		got := eventTxHash("2VfUX:0")
		assert.Equal(t, "0x", got[:2])
	})

	t.Run("invalid base58 falls back to raw value", func(t *testing.T) {
		assert.Equal(t, "0OIl", eventTxHash("0OIl:0"))
	})
}

func TestMarkEventCompleted(t *testing.T) {
	database := newTestDB(t)
	cs := NewChainStore(database)
	seedConfirmedEvent(t, database, "0xdone:0", store.EventTypeInbound, []byte("{}"))

	event := &store.Event{EventID: "0xdone:0", Type: store.EventTypeInbound}
	require.NoError(t, markEventCompleted(cs, zerolog.Nop(), event, "0xvote"))

	rows, err := cs.UpdateEventStatus("0xdone:0", store.StatusCompleted, store.StatusCompleted)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	// already completed: no-op, no error
	require.NoError(t, markEventCompleted(cs, zerolog.Nop(), event, "0xvote2"))
}

func TestEncodeUint256Result(t *testing.T) {
	out, err := EncodeUint256Result(big.NewInt(1_000_000))
	require.NoError(t, err)
	require.Len(t, out, 32)
	assert.Equal(t, big.NewInt(1_000_000), new(big.Int).SetBytes(out))

	out, err = EncodeUint256Result(nil)
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 32), out)

	_, err = EncodeUint256Result(big.NewInt(-1))
	assert.Error(t, err)
}
