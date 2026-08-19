package common

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// InboundObservation is the inbound observation payload stored for INBOUND events
type InboundObservation struct {
	SourceChain         string `json:"sourceChain"`
	LogIndex            uint   `json:"logIndex"`
	Sender              string `json:"sender"`
	Recipient           string `json:"recipient"`
	Token               string `json:"bridgeToken"`
	Amount              string `json:"bridgeAmount"`         // uint256 as decimal string
	RawPayload          string `json:"rawPayload,omitempty"` // hex-encoded raw payload bytes from source chain
	VerificationData    string `json:"verificationData"`
	RevertFundRecipient string `json:"revertFundRecipient,omitempty"`
	TxType              uint   `json:"txType"`  // enum backing uint as decimal string
	FromCEA             bool   `json:"fromCEA"` // true if inbound is initiated by a CEA
}

// InboundObservationEventProcessor handles INBOUND events: it builds the
// inbound observation from the stored event and votes it on Push chain.
type InboundObservationEventProcessor struct {
	signer     VoteSigner
	chainStore *ChainStore
	logger     zerolog.Logger
}

// NewInboundObservationEventProcessor creates the handler for INBOUND events.
func NewInboundObservationEventProcessor(
	signer VoteSigner,
	database *db.DB,
	logger zerolog.Logger,
) *InboundObservationEventProcessor {
	return &InboundObservationEventProcessor{
		signer:     signer,
		chainStore: NewChainStore(database),
		logger:     logger.With().Str("component", "inbound_observation_event_processor").Logger(),
	}
}

// HandleEvent implements EventHandler for INBOUND events.
func (p *InboundObservationEventProcessor) HandleEvent(ctx context.Context, event *store.Event) error {
	p.logger.Debug().
		Str("event_id", event.EventID).
		Msg("processing inbound event")

	// Extract inbound data from event
	inbound, err := p.buildInboundObservation(event)
	if err != nil {
		return fmt.Errorf("failed to build inbound observation: %w", err)
	}

	// Execute vote on blockchain
	voteTxHash, err := p.signer.VoteInbound(ctx, inbound)
	if err != nil {
		return fmt.Errorf("failed to vote on inbound - keeping status for retry: %w", err)
	}

	return markEventCompleted(p.chainStore, p.logger, event, voteTxHash)
}

// buildInboundObservation builds an Inbound observation from event data
func (p *InboundObservationEventProcessor) buildInboundObservation(event *store.Event) (*uexecutortypes.Inbound, error) {
	var eventData InboundObservation

	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}

	if event.EventData == nil {
		return nil, fmt.Errorf("event data is missing for event_id: %s", event.EventID)
	}

	if err := json.Unmarshal(event.EventData, &eventData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	// Map txType from eventData to proper enum value
	txType := uexecutortypes.TxType_UNSPECIFIED_TX
	switch eventData.TxType {
	case 0:
		txType = uexecutortypes.TxType_GAS
	case 1:
		txType = uexecutortypes.TxType_GAS_AND_PAYLOAD
	case 2:
		txType = uexecutortypes.TxType_FUNDS
	case 3:
		txType = uexecutortypes.TxType_FUNDS_AND_PAYLOAD
	default:
		txType = uexecutortypes.TxType_UNSPECIFIED_TX
	}

	txHashHex := eventTxHash(event.EventID)

	inboundMsg := &uexecutortypes.Inbound{
		SourceChain: eventData.SourceChain,
		TxHash:      txHashHex,
		Sender:      eventData.Sender,
		Recipient:   eventData.Recipient,
		Amount:      eventData.Amount,
		AssetAddr:   eventData.Token,
		LogIndex:    strconv.FormatUint(uint64(eventData.LogIndex), 10),
		TxType:      txType,
		IsCEA:       eventData.FromCEA,
		RawPayload:  eventData.RawPayload,
	}

	// Set revert instructions if revert fund recipient is present
	if eventData.RevertFundRecipient != "" {
		inboundMsg.RevertInstructions = &uexecutortypes.RevertInstructions{
			FundRecipient: eventData.RevertFundRecipient,
		}
	}

	// Use event's VerificationData if present, otherwise fall back to txHash
	if eventData.VerificationData == "" || eventData.VerificationData == "0x" {
		inboundMsg.VerificationData = txHashHex
	} else {
		inboundMsg.VerificationData = eventData.VerificationData
	}

	return inboundMsg, nil
}
