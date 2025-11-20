package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/core"
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
		n.mu.RLock()
		_, alreadyProcessing := n.activeEvents[event.EventID]
		n.mu.RUnlock()
		if alreadyProcessing {
			continue
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
	eventCtx, cancel := context.WithTimeout(ctx, n.processingTimeout)
	defer cancel()

	n.mu.Lock()
	n.activeEvents[event.EventID] = cancel
	n.mu.Unlock()

	defer func() {
		n.mu.Lock()
		delete(n.activeEvents, event.EventID)
		n.mu.Unlock()
	}()

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
	isCoordinator := n.isCoordinator(event.BlockNumber, participants)
	if isCoordinator {
		n.logger.Info().Str("event_id", event.EventID).Msg("acting as coordinator")
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusInProgress, "")
	}

	// Parse event data
	var eventData struct {
		KeyID       string `json:"key_id"`
		MessageHash []byte `json:"message_hash,omitempty"`
		ChainPath   []byte `json:"chain_path,omitempty"`
	}
	if len(event.EventData) > 0 {
		if err := json.Unmarshal(event.EventData, &eventData); err != nil {
			n.eventStore.UpdateStatus(event.EventID, eventstore.StatusFailed, fmt.Sprintf("failed to parse event data: %v", err))
			return
		}
	}

	// Pre-register session
	protocolType := tss.ProtocolType(event.ProtocolType)
	if err := n.service.RegisterSessionForEvent(protocolType, event.EventID, event.BlockNumber, participants); err != nil {
		n.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to pre-register session")
	}

	// Execute TSS operation
	threshold := calculateThreshold(len(participants))
	var resultErr error

	switch event.ProtocolType {
	case string(tss.ProtocolKeygen):
		_, resultErr = n.service.RunKeygen(eventCtx, core.KeygenRequest{
			EventID:      event.EventID,
			KeyID:        eventData.KeyID,
			Threshold:    threshold,
			BlockNumber:  event.BlockNumber,
			Participants: participants,
		})
	case string(tss.ProtocolKeyrefresh):
		_, resultErr = n.service.RunKeyrefresh(eventCtx, core.KeyrefreshRequest{
			EventID:      event.EventID,
			KeyID:        eventData.KeyID,
			Threshold:    threshold,
			BlockNumber:  event.BlockNumber,
			Participants: participants,
		})
	case string(tss.ProtocolSign):
		chainPath := eventData.ChainPath
		if len(chainPath) == 0 {
			chainPath = nil
		}
		_, resultErr = n.service.RunSign(eventCtx, core.SignRequest{
			EventID:      event.EventID,
			KeyID:        eventData.KeyID,
			Threshold:    threshold,
			MessageHash:  eventData.MessageHash,
			ChainPath:    chainPath,
			BlockNumber:  event.BlockNumber,
			Participants: participants,
		})
	default:
		resultErr = fmt.Errorf("unknown protocol type: %s", event.ProtocolType)
	}

	if resultErr != nil {
		n.logger.Error().Err(resultErr).Str("event_id", event.EventID).Msg("TSS operation failed")
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusFailed, resultErr.Error())
	} else {
		n.logger.Info().Str("event_id", event.EventID).Msg("TSS operation completed successfully")
		n.eventStore.UpdateStatus(event.EventID, eventstore.StatusSuccess, "")
	}
}

// isCoordinator determines if this node is the coordinator for the given block number.
func (n *Node) isCoordinator(blockNumber uint64, participants []*tss.UniversalValidator) bool {
	if len(participants) == 0 {
		return false
	}
	epoch := blockNumber / n.coordinatorRange
	idx := int(epoch % uint64(len(participants)))
	if idx >= len(participants) {
		return false
	}
	return participants[idx].PartyID() == n.validatorAddress
}
