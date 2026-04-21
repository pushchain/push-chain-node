package evm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventConfirmer(t *testing.T) {
	t.Run("creates event confirmer with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "eip155:1"

		confirmer := NewEventConfirmer(nil, nil, chainID, 5, 5, 12, logger)

		require.NotNil(t, confirmer)
		assert.Equal(t, chainID, confirmer.chainID)
		assert.Equal(t, 5, confirmer.pollIntervalSeconds)
		assert.Equal(t, uint64(5), confirmer.fastConfirmations)
		assert.Equal(t, uint64(12), confirmer.standardConfirmations)
		assert.NotNil(t, confirmer.chainStore)
		assert.NotNil(t, confirmer.stopCh)
	})

	t.Run("creates event confirmer with different confirmation counts", func(t *testing.T) {
		logger := zerolog.Nop()

		testCases := []struct {
			fast     uint64
			standard uint64
		}{
			{1, 6},
			{5, 12},
			{10, 20},
			{0, 1},
		}

		for _, tc := range testCases {
			confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, tc.fast, tc.standard, logger)
			assert.Equal(t, tc.fast, confirmer.fastConfirmations)
			assert.Equal(t, tc.standard, confirmer.standardConfirmations)
		}
	})
}

func TestEventConfirmerGetTxHashFromEventID(t *testing.T) {
	logger := zerolog.Nop()
	confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)

	t.Run("extracts tx hash from standard format", func(t *testing.T) {
		eventID := "0x1234567890abcdef:5"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0x1234567890abcdef", txHash)
	})

	t.Run("extracts tx hash with log index 0", func(t *testing.T) {
		eventID := "0xabc123:0"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0xabc123", txHash)
	})

	t.Run("handles event ID without colon", func(t *testing.T) {
		eventID := "0x1234567890abcdef"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0x1234567890abcdef", txHash)
	})

	t.Run("returns empty string for empty event ID", func(t *testing.T) {
		eventID := ""
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Empty(t, txHash)
	})

	t.Run("handles multiple colons", func(t *testing.T) {
		eventID := "0x123:456:789"
		txHash := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, "0x123", txHash)
	})
}

func TestEventConfirmerGetRequiredConfirmations(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("FAST confirmation type", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(5), confirmations)
	})

	t.Run("STANDARD confirmation type", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(12), confirmations)
	})

	t.Run("unknown type defaults to standard", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("UNKNOWN")
		assert.Equal(t, uint64(12), confirmations)
	})

	t.Run("empty type defaults to standard", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations("")
		assert.Equal(t, uint64(12), confirmations)
	})

	t.Run("uses custom fast confirmations", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 3, 20, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(3), confirmations)
	})

	t.Run("uses custom standard confirmations", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 3, 20, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(20), confirmations)
	})
}

func TestEventConfirmerStop(t *testing.T) {
	t.Run("stop waits for goroutine", func(t *testing.T) {
		logger := zerolog.Nop()
		confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)

		// Should not panic or hang
		confirmer.Stop()
	})
}

func TestEventConfirmerStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		ec := &EventConfirmer{}
		assert.Nil(t, ec.rpcClient)
		assert.Nil(t, ec.chainStore)
		assert.Empty(t, ec.chainID)
		assert.Equal(t, 0, ec.pollIntervalSeconds)
		assert.Equal(t, uint64(0), ec.fastConfirmations)
		assert.Equal(t, uint64(0), ec.standardConfirmations)
		assert.Nil(t, ec.stopCh)
	})
}

// newTestEventConfirmerWithDB creates an EventConfirmer backed by an in-memory database.
func newTestEventConfirmerWithDB(t *testing.T) (*EventConfirmer, *db.DB) {
	t.Helper()
	memDB, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	t.Cleanup(func() { memDB.Close() })
	logger := zerolog.Nop()
	ec := NewEventConfirmer(nil, memDB, "eip155:1", 5, 5, 12, logger)
	return ec, memDB
}

func TestGetTxHashFromEventID_EdgeCases(t *testing.T) {
	logger := zerolog.Nop()
	confirmer := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 12, logger)

	t.Run("colon only returns empty first part", func(t *testing.T) {
		result := confirmer.getTxHashFromEventID(":")
		assert.Equal(t, "", result)
	})

	t.Run("leading colon returns empty first part", func(t *testing.T) {
		result := confirmer.getTxHashFromEventID(":42")
		assert.Equal(t, "", result)
	})

	t.Run("trailing colon returns tx hash", func(t *testing.T) {
		result := confirmer.getTxHashFromEventID("0xabc:")
		assert.Equal(t, "0xabc", result)
	})

	t.Run("full 66-char tx hash with log index", func(t *testing.T) {
		hash := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
		eventID := hash + ":99"
		result := confirmer.getTxHashFromEventID(eventID)
		assert.Equal(t, hash, result)
	})

	t.Run("whitespace-only event ID", func(t *testing.T) {
		result := confirmer.getTxHashFromEventID("   ")
		assert.Equal(t, "   ", result) // no trimming expected
	})
}

func TestProcessPendingEvents_NoPendingEventsInDB(t *testing.T) {
	// Verify the database returns no pending events when empty.
	_, memDB := newTestEventConfirmerWithDB(t)
	cs := common.NewChainStore(memDB)

	pending, err := cs.GetPendingEvents(1000)
	require.NoError(t, err)
	assert.Len(t, pending, 0)
}

func TestProcessPendingEvents_OnlyPendingEventsReturned(t *testing.T) {
	_, memDB := newTestEventConfirmerWithDB(t)
	cs := common.NewChainStore(memDB)

	// Insert one PENDING and one CONFIRMED event.
	pendingEvt := &store.Event{
		EventID:          "0xpending:0",
		BlockHeight:      100,
		Type:             store.EventTypeInbound,
		ConfirmationType: store.ConfirmationStandard,
		Status:           store.StatusPending,
		EventData:        []byte(`{}`),
	}
	confirmedEvt := &store.Event{
		EventID:          "0xconfirmed:1",
		BlockHeight:      90,
		Type:             store.EventTypeInbound,
		ConfirmationType: store.ConfirmationStandard,
		Status:           store.StatusConfirmed,
		EventData:        []byte(`{}`),
	}

	inserted, err := cs.InsertEventIfNotExists(pendingEvt)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = cs.InsertEventIfNotExists(confirmedEvt)
	require.NoError(t, err)
	require.True(t, inserted)

	// GetPendingEvents should only return the PENDING one.
	pending, err := cs.GetPendingEvents(1000)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, "0xpending:0", pending[0].EventID)
}

func TestEventConfirmerStartStop_WithDB(t *testing.T) {
	ec, _ := newTestEventConfirmerWithDB(t)

	ctx, cancel := context.WithCancel(context.Background())

	err := ec.Start(ctx)
	require.NoError(t, err)

	// Let the goroutine spin up briefly.
	time.Sleep(50 * time.Millisecond)

	// Cancel context first, then stop. Should not hang.
	cancel()
	ec.Stop()
}

func TestEventConfirmerStartStop_ViaStopChannel(t *testing.T) {
	ec, _ := newTestEventConfirmerWithDB(t)

	ctx := context.Background()
	err := ec.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Stop via the stopCh (without cancelling context).
	ec.Stop()
}

func TestEventConfirmer_UpdateEventStatus_WithDB(t *testing.T) {
	ec, memDB := newTestEventConfirmerWithDB(t)
	_ = ec // confirmer uses chainStore

	cs := common.NewChainStore(memDB)

	t.Run("update status of pending event to confirmed", func(t *testing.T) {
		event := &store.Event{
			EventID:          "0xabc123:0",
			BlockHeight:      100,
			Type:             store.EventTypeInbound,
			ConfirmationType: store.ConfirmationStandard,
			Status:           store.StatusPending,
			EventData:        []byte(`{"some":"data"}`),
		}
		inserted, err := cs.InsertEventIfNotExists(event)
		require.NoError(t, err)
		require.True(t, inserted)

		rows, err := cs.UpdateEventStatus("0xabc123:0", store.StatusPending, store.StatusConfirmed)
		require.NoError(t, err)
		assert.Equal(t, int64(1), rows)
	})

	t.Run("update status with wrong old status returns 0 rows", func(t *testing.T) {
		// Event is now CONFIRMED, trying to update from PENDING again should affect 0 rows.
		rows, err := cs.UpdateEventStatus("0xabc123:0", store.StatusPending, store.StatusConfirmed)
		require.NoError(t, err)
		assert.Equal(t, int64(0), rows)
	})

	t.Run("update status of non-existent event returns 0 rows", func(t *testing.T) {
		rows, err := cs.UpdateEventStatus("nonexistent:0", store.StatusPending, store.StatusConfirmed)
		require.NoError(t, err)
		assert.Equal(t, int64(0), rows)
	})
}

func TestEventConfirmer_UpdateStatusAndEventData_WithDB(t *testing.T) {
	_, memDB := newTestEventConfirmerWithDB(t)
	cs := common.NewChainStore(memDB)

	outbound := common.OutboundEvent{
		TxID:          "0xtx1",
		UniversalTxID: "0xuni1",
	}
	data, err := json.Marshal(outbound)
	require.NoError(t, err)

	event := &store.Event{
		EventID:          "0xoutbound1:0",
		BlockHeight:      200,
		Type:             store.EventTypeOutbound,
		ConfirmationType: store.ConfirmationFast,
		Status:           store.StatusPending,
		EventData:        data,
	}
	inserted, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)
	require.True(t, inserted)

	// Enrich with gas fee and confirm
	outbound.GasFeeUsed = "123456789"
	updatedData, err := json.Marshal(outbound)
	require.NoError(t, err)

	rows, err := cs.UpdateStatusAndEventData("0xoutbound1:0", store.StatusPending, store.StatusConfirmed, updatedData)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	// Verify the data was updated
	confirmed, err := cs.GetConfirmedEvents(10)
	require.NoError(t, err)
	require.Len(t, confirmed, 1)
	assert.Equal(t, "0xoutbound1:0", confirmed[0].EventID)

	var stored common.OutboundEvent
	require.NoError(t, json.Unmarshal(confirmed[0].EventData, &stored))
	assert.Equal(t, "123456789", stored.GasFeeUsed)
}

func TestEventConfirmer_PendingEventsWithBlockHeightZero(t *testing.T) {
	_, memDB := newTestEventConfirmerWithDB(t)
	cs := common.NewChainStore(memDB)

	// Insert an event with BlockHeight 0 (should be skipped by processPendingEvents)
	event := &store.Event{
		EventID:          "0xzeroblock:0",
		BlockHeight:      0,
		Type:             store.EventTypeInbound,
		ConfirmationType: store.ConfirmationStandard,
		Status:           store.StatusPending,
		EventData:        []byte(`{}`),
	}
	inserted, err := cs.InsertEventIfNotExists(event)
	require.NoError(t, err)
	require.True(t, inserted)

	pending, err := cs.GetPendingEvents(100)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, uint64(0), pending[0].BlockHeight)
}

func TestEventConfirmer_GetRequiredConfirmations_ZeroValues(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("zero fast confirmations returns 0", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 0, 12, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(0), result)
	})

	t.Run("zero standard confirmations returns 0", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 0, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(0), result)
	})

	t.Run("zero standard with unknown type returns 0", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 0, logger)
		result := ec.getRequiredConfirmations("INSTANT")
		assert.Equal(t, uint64(0), result)
	})
}

func TestEventConfirmer_NewWithDatabase(t *testing.T) {
	memDB, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer memDB.Close()

	logger := zerolog.Nop()
	ec := NewEventConfirmer(nil, memDB, "eip155:137", 10, 3, 20, logger)

	require.NotNil(t, ec)
	assert.NotNil(t, ec.chainStore)
	assert.Equal(t, "eip155:137", ec.chainID)
	assert.Equal(t, 10, ec.pollIntervalSeconds)
}

func TestEventConfirmer_GetRequiredConfirmations_LargeValues(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("very large fast confirmations", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 1000000, 12, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(1000000), result)
	})

	t.Run("very large standard confirmations", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 999999, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(999999), result)
	})

	t.Run("fast 1 confirmation", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 1, 12, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(1), result)
	})

	t.Run("standard 1 confirmation", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 1, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(1), result)
	})

	t.Run("unknown type with large standard returns large value", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 5, 500, logger)
		result := ec.getRequiredConfirmations("SUPER_SAFE")
		assert.Equal(t, uint64(500), result)
	})

	t.Run("all confirmation types consistent when same values", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "eip155:1", 5, 10, 10, logger)
		fast := ec.getRequiredConfirmations(store.ConfirmationFast)
		standard := ec.getRequiredConfirmations(store.ConfirmationStandard)
		unknown := ec.getRequiredConfirmations("OTHER")
		assert.Equal(t, uint64(10), fast)
		assert.Equal(t, uint64(10), standard)
		assert.Equal(t, uint64(10), unknown)
	})
}
