package pushwatcher

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

type fakeEventHandler struct {
	handled []string
	err     error
}

func (f *fakeEventHandler) HandleEvent(ctx context.Context, event *store.Event) error {
	f.handled = append(f.handled, event.EventID)
	return f.err
}

func seedEvent(t *testing.T, cs *common.ChainStore, eventID, eventType string) {
	t.Helper()
	stored, err := cs.InsertEventIfNotExists(&store.Event{
		EventID:          eventID,
		Type:             eventType,
		ConfirmationType: store.ConfirmationInstant,
		Status:           store.StatusConfirmed,
		EventData:        []byte("{}"),
	})
	require.NoError(t, err)
	require.True(t, stored)
}

func TestEventProcessor_DispatchesByType(t *testing.T) {
	database := newTestDB(t)
	p, err := NewEventProcessor(database, 0, zerolog.Nop())
	require.NoError(t, err)
	cs := common.NewChainStore(database)

	readHandler := &fakeEventHandler{}
	p.RegisterHandler(store.EventTypeReadRequest, readHandler)

	seedEvent(t, cs, "read-1", store.EventTypeReadRequest)
	seedEvent(t, cs, "tss-1", store.EventTypeKeygen) // no handler registered

	p.processConfirmedEvents(context.Background())

	assert.Equal(t, []string{"read-1"}, readHandler.handled)
}

func TestEventProcessor_HandlerErrorKeepsProcessing(t *testing.T) {
	database := newTestDB(t)
	p, err := NewEventProcessor(database, 0, zerolog.Nop())
	require.NoError(t, err)
	cs := common.NewChainStore(database)

	failing := &fakeEventHandler{err: fmt.Errorf("boom")}
	p.RegisterHandler(store.EventTypeReadRequest, failing)

	seedEvent(t, cs, "read-1", store.EventTypeReadRequest)
	seedEvent(t, cs, "read-2", store.EventTypeReadRequest)

	p.processConfirmedEvents(context.Background())

	// both attempted despite errors, both still CONFIRMED for retry
	assert.Len(t, failing.handled, 2)
	events, err := cs.GetConfirmedEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 2)
}

func TestEventProcessor_NilDatabase(t *testing.T) {
	_, err := NewEventProcessor(nil, 0, zerolog.Nop())
	assert.ErrorIs(t, err, ErrNilDatabase)
}

func TestEventProcessor_StartStop(t *testing.T) {
	p, err := NewEventProcessor(newTestDB(t), 0, zerolog.Nop())
	require.NoError(t, err)

	require.NoError(t, p.Start(context.Background()))
	assert.Equal(t, ErrAlreadyRunning, p.Start(context.Background()))
	require.NoError(t, p.Stop())
	assert.Equal(t, ErrNotRunning, p.Stop())
}
