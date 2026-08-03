package common

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// OutboundObservationEventProcessor handles OUTBOUND events: it builds the
// outbound observation from the stored event and votes it on Push chain.
type OutboundObservationEventProcessor struct {
	signer     VoteSigner
	chainStore *ChainStore
	logger     zerolog.Logger
}

// NewOutboundObservationEventProcessor creates the handler for OUTBOUND events.
func NewOutboundObservationEventProcessor(
	signer VoteSigner,
	database *db.DB,
	logger zerolog.Logger,
) *OutboundObservationEventProcessor {
	return &OutboundObservationEventProcessor{
		signer:     signer,
		chainStore: NewChainStore(database),
		logger:     logger.With().Str("component", "outbound_observation_event_processor").Logger(),
	}
}

// HandleEvent implements EventHandler for OUTBOUND events.
func (p *OutboundObservationEventProcessor) HandleEvent(ctx context.Context, event *store.Event) error {
	p.logger.Debug().
		Str("event_id", event.EventID).
		Msg("processing outbound event")

	// Parse outbound event data once
	outboundData, err := p.parseOutboundEventData(event)
	if err != nil {
		return fmt.Errorf("failed to parse outbound event data: %w", err)
	}

	// Build observation from parsed data
	observation, err := p.buildOutboundObservation(event, outboundData)
	if err != nil {
		return fmt.Errorf("failed to build outbound observation: %w", err)
	}

	// Vote on outbound
	voteTxHash, err := p.signer.VoteOutbound(ctx, outboundData.TxID, outboundData.UniversalTxID, observation)
	if err != nil {
		return fmt.Errorf("failed to vote on outbound: %w", err)
	}

	return markEventCompleted(p.chainStore, p.logger, event, voteTxHash)
}

// parseOutboundEventData unmarshals event data into an OutboundEvent struct
func (p *OutboundObservationEventProcessor) parseOutboundEventData(event *store.Event) (*OutboundEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}

	if len(event.EventData) == 0 {
		return nil, fmt.Errorf("event data is empty")
	}

	var eventData OutboundEvent
	if err := json.Unmarshal(event.EventData, &eventData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	if eventData.TxID == "" {
		return nil, fmt.Errorf("tx_id not found in event data")
	}

	if eventData.UniversalTxID == "" {
		return nil, fmt.Errorf("universal_tx_id not found in event data")
	}

	return &eventData, nil
}

// buildOutboundObservation builds an OutboundObservation from event metadata and parsed outbound data
func (p *OutboundObservationEventProcessor) buildOutboundObservation(event *store.Event, outboundData *OutboundEvent) (*uexecutortypes.OutboundObservation, error) {
	gasFeeUsed := "0"
	if outboundData.GasFeeUsed != "" {
		gasFeeUsed = outboundData.GasFeeUsed
	}

	observation := &uexecutortypes.OutboundObservation{
		Success:            true,
		BlockHeight:        event.BlockHeight,
		TxHash:             eventTxHash(event.EventID),
		ErrorMsg:           "",
		GasFeeUsed:         gasFeeUsed,
		Pc20WrapperAddress: outboundData.Pc20WrapperAddress,
	}

	return observation, nil
}
