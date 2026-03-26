package integrationtest

import (
	"strconv"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/utss/keeper"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// TestTssEventCreatedOnProcessInitiation verifies that initiating a TSS key process
// creates a corresponding TssEvent with ACTIVE status.
func TestTssEventCreatedOnProcessInitiation(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	// Find the event for this process
	var foundEvent *utsstypes.TssEvent
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		if event.ProcessId == process.Id && event.EventType == utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED {
			e := event
			foundEvent = &e
			return true, nil
		}
		return false, nil
	})

	require.NotNil(t, foundEvent, "TssEvent should be created on process initiation")
	require.Equal(t, utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED, foundEvent.EventType)
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_ACTIVE, foundEvent.Status)
	require.Equal(t, process.Id, foundEvent.ProcessId)
	require.Equal(t, process.ProcessType.String(), foundEvent.ProcessType)
	require.Equal(t, process.Participants, foundEvent.Participants)
	require.Equal(t, process.ExpiryHeight, foundEvent.ExpiryHeight)
	require.Equal(t, ctx.BlockHeight(), foundEvent.BlockHeight)
}

// TestTssEventCreatedOnKeyFinalization verifies that finalizing a TSS key creates
// a KEY_FINALIZED event and marks the initiated event as COMPLETED.
func TestTssEventCreatedOnKeyFinalization(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	pub := "finalize-pub"
	key := "finalize-key"

	process, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)

	// Vote from all validators to reach quorum and finalize
	for _, v := range validators {
		valAddr, _ := sdk.ValAddressFromBech32(v)
		err := app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key, process.Id)
		require.NoError(t, err)
	}

	// Check for KEY_FINALIZED event
	var finalizedEvent *utsstypes.TssEvent
	var initiatedEvent *utsstypes.TssEvent
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		if event.ProcessId == process.Id {
			e := event
			if event.EventType == utsstypes.TssEventType_TSS_EVENT_KEY_FINALIZED {
				finalizedEvent = &e
			}
			if event.EventType == utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED {
				initiatedEvent = &e
			}
		}
		return false, nil
	})

	require.NotNil(t, finalizedEvent, "KEY_FINALIZED event should be created")
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_ACTIVE, finalizedEvent.Status)
	require.Equal(t, process.Id, finalizedEvent.ProcessId)
	require.Equal(t, key, finalizedEvent.KeyId)
	require.Equal(t, pub, finalizedEvent.TssPubkey)
	require.Equal(t, process.Participants, finalizedEvent.Participants)

	// Initiated event should be COMPLETED
	require.NotNil(t, initiatedEvent, "Initiated event should exist")
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_COMPLETED, initiatedEvent.Status)
}

// TestTssEventMarkedExpiredOnProcessFailure verifies that failing a process
// marks its initiated event as EXPIRED.
func TestTssEventMarkedExpiredOnProcessFailure(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	process, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)

	// Finalize with FAILED status
	err = app.UtssKeeper.FinalizeTssKeyProcess(ctx, process.Id, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_FAILED)
	require.NoError(t, err)

	// Find the initiated event
	var foundEvent *utsstypes.TssEvent
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		if event.ProcessId == process.Id && event.EventType == utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED {
			e := event
			foundEvent = &e
			return true, nil
		}
		return false, nil
	})

	require.NotNil(t, foundEvent)
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_EXPIRED, foundEvent.Status)
}

// TestPreviousFinalizedEventCompletedOnNewFinalization verifies that when a new key
// is finalized, the previous finalized event is marked as COMPLETED.
func TestPreviousFinalizedEventCompletedOnNewFinalization(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	// Process A: initiate and finalize
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	processA, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	for _, v := range validators {
		valAddr, _ := sdk.ValAddressFromBech32(v)
		_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pubA", "keyA", processA.Id)
	}

	// Process B: initiate and finalize
	err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	processB, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	for _, v := range validators {
		valAddr, _ := sdk.ValAddressFromBech32(v)
		_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pubB", "keyB", processB.Id)
	}

	// Check events
	var finalizedA *utsstypes.TssEvent
	var finalizedB *utsstypes.TssEvent
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		if event.EventType == utsstypes.TssEventType_TSS_EVENT_KEY_FINALIZED {
			e := event
			if event.ProcessId == processA.Id {
				finalizedA = &e
			}
			if event.ProcessId == processB.Id {
				finalizedB = &e
			}
		}
		return false, nil
	})

	require.NotNil(t, finalizedA)
	require.NotNil(t, finalizedB)
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_COMPLETED, finalizedA.Status, "Process A's finalized event should be COMPLETED")
	require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_ACTIVE, finalizedB.Status, "Process B's finalized event should be ACTIVE")
}

// TestGetTssEventQuery verifies the GetTssEvent query returns the correct event.
func TestGetTssEventQuery(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	// Find any event ID
	var eventId uint64
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		eventId = id
		return true, nil // take the first
	})

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.GetTssEvent(ctx, &utsstypes.QueryGetTssEventRequest{Id: eventId})
	require.NoError(t, err)
	require.NotNil(t, resp.Event)
	require.Equal(t, eventId, resp.Event.Id)
}

// TestGetTssEventNotFound verifies that querying a non-existent event returns an error.
func TestGetTssEventNotFound(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	querier := keeper.NewQuerier(app.UtssKeeper)
	_, err := querier.GetTssEvent(ctx, &utsstypes.QueryGetTssEventRequest{Id: 99999})
	require.Error(t, err)
}

// TestAllPendingTssEventsQuery verifies that only pending (not yet finalized) events are returned.
func TestAllPendingTssEventsQuery(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	// Process A: initiated — should appear in pending
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	processA, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)

	querier := keeper.NewQuerier(app.UtssKeeper)

	// Before finalization: 1 pending event (process A initiated)
	resp, err := querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 100},
	})
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	require.Equal(t, processA.Id, resp.Events[0].ProcessId)

	// Finalize process A via votes
	for _, v := range validators {
		valAddr, _ := sdk.ValAddressFromBech32(v)
		_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pubA", "keyA", processA.Id)
	}

	// After finalization: 0 pending events (process A removed from pending)
	resp, err = querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 100},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Events)

	// Process B: initiated then failed
	err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	processB, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)

	// Before failure: 1 pending event (process B)
	resp, err = querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 100},
	})
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	require.Equal(t, processB.Id, resp.Events[0].ProcessId)

	// Fail process B
	err = app.UtssKeeper.FinalizeTssKeyProcess(ctx, processB.Id, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_FAILED)
	require.NoError(t, err)

	// After failure: 0 pending events
	resp, err = querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 100},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Events)
}

// TestAllPendingTssEventsOrderedByBlockHeight verifies events come in ascending ID/block height order.
func TestAllPendingTssEventsOrderedByBlockHeight(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	// Create events at different block heights
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 5)
	err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 100},
	})
	require.NoError(t, err)

	// Verify ascending order by ID
	for i := 1; i < len(resp.Events); i++ {
		require.True(t, resp.Events[i].Id > resp.Events[i-1].Id, "Events should be in ascending ID order")
	}
}

// TestAllPendingTssEventsPagination verifies pagination works for active events.
func TestAllPendingTssEventsPagination(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	// Create 5 active events (process initiations)
	for i := 0; i < 5; i++ {
		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)
		ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	}

	querier := keeper.NewQuerier(app.UtssKeeper)

	// Query with limit=2
	resp1, err := querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp1.Events, 2)

	// Query with offset=2, limit=2
	resp2, err := querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Offset: 2, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp2.Events, 2)

	// Verify different events
	require.NotEqual(t, resp1.Events[0].Id, resp2.Events[0].Id)
}

// TestAllTssEventsQuery verifies all events are returned regardless of status.
func TestAllTssEventsQuery(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	// Create an active event
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	process, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)

	// Finalize it (creates completed + active finalized)
	for _, v := range validators {
		valAddr, _ := sdk.ValAddressFromBech32(v)
		_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pub1", "key1", process.Id)
	}

	// Create another and fail it
	err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	process2, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	err = app.UtssKeeper.FinalizeTssKeyProcess(ctx, process2.Id, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_FAILED)
	require.NoError(t, err)

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.AllTssEvents(ctx, &utsstypes.QueryAllTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 100},
	})
	require.NoError(t, err)

	// Should have events of all statuses
	statusSet := make(map[utsstypes.TssEventStatus]bool)
	for _, event := range resp.Events {
		statusSet[event.Status] = true
	}
	// We expect at least ACTIVE and COMPLETED statuses
	require.True(t, len(statusSet) >= 2, "Should have events with multiple statuses")
}

// TestAllTssEventsPagination verifies pagination for all events.
func TestAllTssEventsPagination(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	// Create several events
	for i := 0; i < 5; i++ {
		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)
		ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	}

	querier := keeper.NewQuerier(app.UtssKeeper)

	// First page
	resp1, err := querier.AllTssEvents(ctx, &utsstypes.QueryAllTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 3},
	})
	require.NoError(t, err)
	require.Len(t, resp1.Events, 3)

	// Use next key for second page
	resp2, err := querier.AllTssEvents(ctx, &utsstypes.QueryAllTssEventsRequest{
		Pagination: &query.PageRequest{Key: resp1.Pagination.NextKey, Limit: 3},
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp2.Events)

	// Verify no overlap
	for _, e1 := range resp1.Events {
		for _, e2 := range resp2.Events {
			require.NotEqual(t, e1.Id, e2.Id)
		}
	}
}

// TestTssEventIdAutoIncrement verifies that event IDs are monotonically increasing.
func TestTssEventIdAutoIncrement(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	// Create multiple events
	for i := 0; i < 3; i++ {
		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)
		ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	}

	var ids []uint64
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		ids = append(ids, event.Id)
		return false, nil
	})

	// Verify unique and monotonically increasing
	for i := 1; i < len(ids); i++ {
		require.True(t, ids[i] > ids[i-1], "Event IDs should be monotonically increasing")
	}
}

// TestMultipleProcessTypes verifies events correctly record different process types.
func TestMultipleProcessTypes(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	// KEYGEN
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	process, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	// Finalize so we can start another
	for _, v := range validators {
		valAddr, _ := sdk.ValAddressFromBech32(v)
		_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pub-kg", "key-kg-"+strconv.Itoa(0), process.Id)
	}

	// QUORUM_CHANGE
	err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_QUORUM_CHANGE)
	require.NoError(t, err)

	processTypes := make(map[string]bool)
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		if event.EventType == utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED {
			processTypes[event.ProcessType] = true
		}
		return false, nil
	})

	require.True(t, processTypes[utsstypes.TssProcessType_TSS_PROCESS_KEYGEN.String()], "Should have KEYGEN events")
	require.True(t, processTypes[utsstypes.TssProcessType_TSS_PROCESS_QUORUM_CHANGE.String()], "Should have QUORUM_CHANGE events")
}

// TestNoActiveEventsReturnsEmpty verifies empty list when no active events exist.
func TestNoActiveEventsReturnsEmpty(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	// Create an event and expire it
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)
	process, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	err = app.UtssKeeper.FinalizeTssKeyProcess(ctx, process.Id, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_FAILED)
	require.NoError(t, err)

	// Mark any remaining active events (from setup) as completed
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		if event.Status == utsstypes.TssEventStatus_TSS_EVENT_ACTIVE {
			event.Status = utsstypes.TssEventStatus_TSS_EVENT_COMPLETED
			_ = app.UtssKeeper.TssEvents.Set(ctx, id, event)
		}
		return false, nil
	})

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.AllPendingTssEvents(ctx, &utsstypes.QueryAllPendingTssEventsRequest{
		Pagination: &query.PageRequest{Limit: 100},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Events)
}

// TestConcurrentProcessInitiations verifies two processes initiated in the same block
// get unique IDs and same block height.
func TestConcurrentProcessInitiations(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	blockHeight := ctx.BlockHeight()

	// First initiation
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	// Second initiation in same block (force-expires the first)
	err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_QUORUM_CHANGE)
	require.NoError(t, err)

	var events []utsstypes.TssEvent
	_ = app.UtssKeeper.TssEvents.Walk(ctx, nil, func(id uint64, event utsstypes.TssEvent) (bool, error) {
		if event.EventType == utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED && event.BlockHeight == blockHeight {
			events = append(events, event)
		}
		return false, nil
	})

	require.GreaterOrEqual(t, len(events), 2, "Should have at least 2 events from same block")

	// Verify unique IDs
	idSet := make(map[uint64]bool)
	for _, e := range events {
		require.False(t, idSet[e.Id], "Event IDs must be unique")
		idSet[e.Id] = true
		require.Equal(t, blockHeight, e.BlockHeight, "Both events should have same block height")
	}
}
