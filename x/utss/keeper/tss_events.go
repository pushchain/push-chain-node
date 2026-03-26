package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/utss/types"
)

// updateTssEventStatusByProcessId finds a TSS event matching the given processId and eventType,
// and updates its status to the given newStatus.
func (k Keeper) updateTssEventStatusByProcessId(ctx context.Context, processId uint64, eventType types.TssEventType, newStatus types.TssEventStatus) error {
	var foundId uint64
	var foundEvent types.TssEvent
	found := false

	err := k.TssEvents.Walk(ctx, nil, func(id uint64, event types.TssEvent) (bool, error) {
		if event.ProcessId == processId && event.EventType == eventType {
			foundId = id
			foundEvent = event
			found = true
			return true, nil // stop walking
		}
		return false, nil // continue
	})
	if err != nil {
		return err
	}

	if !found {
		return nil // no matching event found, nothing to update
	}

	foundEvent.Status = newStatus
	return k.TssEvents.Set(ctx, foundId, foundEvent)
}

// completePreviousActiveFinalizedEvent finds any active TSS_EVENT_KEY_FINALIZED event
// and marks it as COMPLETED. This is called before creating a new finalized event.
func (k Keeper) completePreviousActiveFinalizedEvent(ctx context.Context) error {
	var toUpdate []struct {
		id    uint64
		event types.TssEvent
	}

	err := k.TssEvents.Walk(ctx, nil, func(id uint64, event types.TssEvent) (bool, error) {
		if event.EventType == types.TssEventType_TSS_EVENT_KEY_FINALIZED && event.Status == types.TssEventStatus_TSS_EVENT_ACTIVE {
			toUpdate = append(toUpdate, struct {
				id    uint64
				event types.TssEvent
			}{id: id, event: event})
		}
		return false, nil // continue walking to find all
	})
	if err != nil {
		return err
	}

	for _, item := range toUpdate {
		item.event.Status = types.TssEventStatus_TSS_EVENT_COMPLETED
		if err := k.TssEvents.Set(ctx, item.id, item.event); err != nil {
			return err
		}
	}

	return nil
}
