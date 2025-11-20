package node

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// pollLoop polls the database for pending events and processes them.
func (n *Node) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(n.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-n.stopCh:
			return
		case <-ticker.C:
			if err := n.processPendingEvents(ctx); err != nil {
				n.logger.Error().Err(err).Msg("error processing pending events")
			}
		}
	}
}

// processPendingEvents queries the database for pending events and starts processing them.
func (n *Node) processPendingEvents(ctx context.Context) error {
	currentBlock, err := n.dataProvider.GetLatestBlockNum(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get latest block number")
	}

	// Get pending events from event store (with 10 block confirmation)
	events, err := n.eventStore.GetPendingEvents(currentBlock, 10)
	if err != nil {
		return errors.Wrap(err, "failed to get pending events")
	}

	for _, event := range events {
		// Check if event is already in progress (using database status)
		existingEvent, err := n.eventStore.GetEvent(event.EventID)
		if err == nil && existingEvent != nil && existingEvent.Status == eventstore.StatusInProgress {
			continue // Already being processed
		}

		n.processingWg.Add(1)
		go func(evt store.TSSEvent) {
			defer n.processingWg.Done()
			n.processEvent(ctx, evt)
		}(event)
	}

	return nil
}

// processEvent processes a single TSS event.
func (n *Node) processEvent(ctx context.Context, event store.TSSEvent) {
	// Mark event as in progress
	n.eventStore.UpdateStatus(event.EventID, eventstore.StatusInProgress, "")

	eventCtx, cancel := context.WithTimeout(ctx, n.processingTimeout)
	defer cancel()

	n.logger.Info().
		Str("event_id", event.EventID).
		Str("protocol", event.ProtocolType).
		Uint64("block_number", event.BlockNumber).
		Msg("processing TSS event")

	// Get participants
	allValidators, err := n.dataProvider.GetUniversalValidators(eventCtx)
	if err != nil {
		n.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to get validators")
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusFailed, err.Error())
		return
	}

	var participants []*tss.UniversalValidator
	for _, v := range allValidators {
		if v.Status == tss.UVStatusActive {
			participants = append(participants, v)
		}
	}

	if len(participants) == 0 {
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusFailed, "no active participants")
		return
	}

	// Check if we're a participant
	var isParticipant bool
	for _, p := range participants {
		if p.PartyID() == n.validatorAddress {
			isParticipant = true
			break
		}
	}
	if !isParticipant {
		return
	}

	// Check if we're coordinator
	isCoordinator := isCoordinator(event.BlockNumber, n.coordinatorRange, n.validatorAddress, participants)
	if isCoordinator {
		n.logger.Info().Str("event_id", event.EventID).Msg("acting as coordinator")
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusInProgress, "")
	}

	// Execute TSS operation based on protocol type
	var resultErr error

	switch event.ProtocolType {
	case string(tss.ProtocolKeygen):
		_, resultErr = n.executeKeygen(eventCtx, KeygenRequest{
			EventID:       event.EventID,
			BlockNumber:   event.BlockNumber,
			Participants:  participants,
			IsCoordinator: isCoordinator,
		})
	case string(tss.ProtocolKeyrefresh):
		// TODO: Implement keyrefresh
		resultErr = errors.New("keyrefresh not yet implemented")
	case string(tss.ProtocolSign):
		// TODO: Implement sign
		resultErr = errors.New("sign not yet implemented")
	default:
		resultErr = errors.Errorf("unknown protocol type: %s", event.ProtocolType)
	}

	if resultErr != nil {
		n.logger.Error().Err(resultErr).Str("event_id", event.EventID).Msg("TSS operation failed")
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusFailed, resultErr.Error())
	} else {
		n.logger.Info().Str("event_id", event.EventID).Msg("TSS operation completed successfully")
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusSuccess, "")
	}
}
