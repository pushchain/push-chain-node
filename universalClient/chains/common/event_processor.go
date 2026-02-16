package common

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mr-tron/base58"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// EventProcessor processes events from the chain's database and votes on them
type EventProcessor struct {
	signer     *pushsigner.Signer
	chainStore *ChainStore
	logger     zerolog.Logger
	chainID    string
	running    bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(
	signer *pushsigner.Signer,
	database *db.DB,
	chainID string,
	logger zerolog.Logger,
) *EventProcessor {
	return &EventProcessor{
		signer:     signer,
		chainStore: NewChainStore(database),
		chainID:    chainID,
		logger:     logger.With().Str("component", "event_processor").Str("chain", chainID).Logger(),
		stopCh:     make(chan struct{}),
	}
}

// Start begins processing events
func (ep *EventProcessor) Start(ctx context.Context) error {
	if ep.running {
		return fmt.Errorf("event processor is already running")
	}

	ep.running = true
	ep.stopCh = make(chan struct{})

	ep.wg.Add(1)
	go ep.processLoop(ctx)

	ep.logger.Info().Msg("event processor started")
	return nil
}

// Stop gracefully stops the event processor
func (ep *EventProcessor) Stop() error {
	if !ep.running {
		return nil
	}

	ep.logger.Info().Msg("stopping event processor")
	close(ep.stopCh)
	ep.running = false

	ep.wg.Wait()
	ep.logger.Info().Msg("event processor stopped")
	return nil
}

// IsRunning returns whether the processor is currently running
func (ep *EventProcessor) IsRunning() bool {
	return ep.running
}

// processLoop is the main event processing loop
func (ep *EventProcessor) processLoop(ctx context.Context) {
	defer ep.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			ep.logger.Info().Msg("context cancelled, stopping event processor")
			return
		case <-ep.stopCh:
			ep.logger.Info().Msg("stop signal received, stopping event processor")
			return
		case <-ticker.C:
			// Fetch 1000 CONFIRMED events and process them
			if err := ep.processConfirmedEvents(ctx); err != nil {
				ep.logger.Error().Err(err).Msg("failed to process confirmed events")
			}
		}
	}
}

// processConfirmedEvents processes confirmed events (both inbound and outbound)
func (ep *EventProcessor) processConfirmedEvents(ctx context.Context) error {
	events, err := ep.chainStore.GetConfirmedEvents(1000)
	if err != nil {
		return fmt.Errorf("failed to get confirmed events: %w", err)
	}

	for _, event := range events {
		if event.Type == EventTypeInbound {
			if err := ep.processInboundEvent(ctx, &event); err != nil {
				ep.logger.Error().
					Err(err).
					Str("event_id", event.EventID).
					Msg("failed to vote on inbound event")
				continue
			}
		} else if event.Type == EventTypeOutbound {
			if err := ep.processOutboundEvent(ctx, &event); err != nil {
				ep.logger.Error().
					Err(err).
					Str("event_id", event.EventID).
					Msg("failed to vote on outbound event")
				continue
			}
		}
	}

	return nil
}

// processOutboundEvent processes an outbound event by voting on it
func (ep *EventProcessor) processOutboundEvent(ctx context.Context, event *store.Event) error {
	// Extract observation from event data
	observation, err := ep.extractOutboundObservation(event)
	if err != nil {
		return fmt.Errorf("failed to extract outbound observation: %w", err)
	}

	// Extract txID and universalTxID from event data
	txID, utxID, err := ep.extractOutboundIDs(event)
	if err != nil {
		return fmt.Errorf("failed to extract outbound IDs: %w", err)
	}

	// Vote on outbound
	voteTxHash, err := ep.signer.VoteOutbound(ctx, txID, utxID, observation)
	if err != nil {
		return fmt.Errorf("failed to vote on outbound: %w", err)
	}

	// Update vote_tx_hash first
	if err := ep.chainStore.UpdateVoteTxHash(event.EventID, voteTxHash); err != nil {
		ep.logger.Error().
			Err(err).
			Str("event_id", event.EventID).
			Msg("failed to update vote_tx_hash")
	}

	// Update event status to COMPLETED using chain_store
	rowsAffected, err := ep.chainStore.UpdateEventStatus(event.EventID, "CONFIRMED", "COMPLETED")
	if err != nil {
		return fmt.Errorf("failed to update event status: %w", err)
	}

	if rowsAffected == 0 {
		ep.logger.Warn().
			Str("event_id", event.EventID).
			Msg("event status was already changed - possibly processed by another worker")
		return nil
	}

	ep.logger.Info().
		Str("event_id", event.EventID).
		Str("tx_id", txID).
		Str("utx_id", utxID).
		Str("vote_tx_hash", voteTxHash).
		Msg("voted on outbound event")

	return nil
}

// processInboundEvent processes an inbound event by voting on it and confirming it
func (ep *EventProcessor) processInboundEvent(ctx context.Context, event *store.Event) error {
	ep.logger.Info().
		Str("event_id", event.EventID).
		Uint32("id", uint32(event.ID)).
		Uint64("block", event.BlockHeight).
		Str("current_status", event.Status).
		Msg("processing inbound event")

	// Extract inbound data from event
	inbound, err := ep.constructInbound(event)
	if err != nil {
		return fmt.Errorf("failed to construct inbound: %w", err)
	}

	// Execute vote on blockchain
	voteTxHash, err := ep.signer.VoteInbound(ctx, inbound)
	if err != nil {
		ep.logger.Error().
			Str("event_id", event.EventID).
			Err(err).
			Msg("failed to vote on event - keeping status for retry")
		return err
	}

	// Update event status using chain_store
	rowsAffected, err := ep.chainStore.UpdateEventStatus(event.EventID, "CONFIRMED", "COMPLETED")
	if err != nil {
		return fmt.Errorf("failed to update event status after successful vote: %w", err)
	}

	if rowsAffected == 0 {
		ep.logger.Warn().
			Str("event_id", event.EventID).
			Msg("event status was already changed - possibly processed by another worker")
		return nil
	}

	// Update vote_tx_hash
	if err := ep.chainStore.UpdateVoteTxHash(event.EventID, voteTxHash); err != nil {
		ep.logger.Error().
			Err(err).
			Str("event_id", event.EventID).
			Msg("failed to update vote_tx_hash")
	}

	ep.logger.Info().
		Str("event_id", event.EventID).
		Str("vote_tx_hash", voteTxHash).
		Msg("inbound event processed and confirmed successfully")

	return nil
}

// constructInbound creates an Inbound message from event data
func (ep *EventProcessor) constructInbound(event *store.Event) (*uexecutortypes.Inbound, error) {
	var eventData UniversalTx

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

	// Extract txHash from EventID (format: "txHash:logIndex")
	txHash := ""
	parts := strings.Split(event.EventID, ":")
	if len(parts) > 0 {
		txHash = parts[0]
	}

	// Convert txHash to hex format if it's in base58
	txHashHex, err := ep.base58ToHex(txHash)
	if err != nil {
		ep.logger.Warn().
			Str("tx_hash", txHash).
			Err(err).
			Msg("failed to convert txHash to hex, using original value")
		txHashHex = txHash
	}

	inboundMsg := &uexecutortypes.Inbound{
		SourceChain: eventData.SourceChain,
		TxHash:      txHashHex,
		Sender:      eventData.Sender,
		Amount:      eventData.Amount,
		AssetAddr:   eventData.Token,
		LogIndex:    strconv.FormatUint(uint64(eventData.LogIndex), 10),
		TxType:      txType,
	}

	if txType == uexecutortypes.TxType_FUNDS_AND_PAYLOAD || txType == uexecutortypes.TxType_GAS_AND_PAYLOAD {
		inboundMsg.UniversalPayload = &eventData.Payload
	}

	// Set recipient for transactions that involve funds
	if txType == uexecutortypes.TxType_FUNDS || txType == uexecutortypes.TxType_GAS {
		inboundMsg.Recipient = eventData.Recipient
	}

	// Check if VerificationData is 0x and replace with TxHash
	if inboundMsg.UniversalPayload != nil && inboundMsg.UniversalPayload.VType == uexecutortypes.VerificationType_universalTxVerification {
		inboundMsg.VerificationData = txHashHex
	} else {
		inboundMsg.VerificationData = eventData.VerificationData
	}

	return inboundMsg, nil
}

// base58ToHex converts a base58 encoded string to hex format (0x...)
func (ep *EventProcessor) base58ToHex(base58Str string) (string, error) {
	if base58Str == "" {
		return "0x", nil
	}

	// Check if it's already in hex format
	if strings.HasPrefix(base58Str, "0x") {
		return base58Str, nil
	}

	// Decode base58 to bytes
	decoded, err := base58.Decode(base58Str)
	if err != nil {
		return "", fmt.Errorf("failed to decode base58: %w", err)
	}

	// Convert to hex with 0x prefix
	return "0x" + hex.EncodeToString(decoded), nil
}

// extractOutboundIDs extracts both txID and universalTxID from an outbound Event's event data
func (ep *EventProcessor) extractOutboundIDs(event *store.Event) (txID string, utxID string, err error) {
	if event == nil {
		return "", "", fmt.Errorf("event is nil")
	}

	if len(event.EventData) == 0 {
		return "", "", fmt.Errorf("event data is empty")
	}

	// Parse event data JSON to extract tx_id and universal_tx_id
	var eventData OutboundEvent
	if err := json.Unmarshal(event.EventData, &eventData); err != nil {
		return "", "", fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	if eventData.TxID == "" {
		return "", "", fmt.Errorf("tx_id not found in event data")
	}

	if eventData.UniversalTxID == "" {
		return "", "", fmt.Errorf("universal_tx_id not found in event data")
	}

	return eventData.TxID, eventData.UniversalTxID, nil
}

// extractOutboundObservation extracts an OutboundObservation from event data
func (ep *EventProcessor) extractOutboundObservation(event *store.Event) (*uexecutortypes.OutboundObservation, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}

	// Extract txHash from EventID (format: "txHash:logIndex" or "signature:logIndex")
	txHash := ""
	parts := strings.Split(event.EventID, ":")
	if len(parts) > 0 {
		txHash = parts[0]
	}

	// Convert txHash to hex format if it's in base58
	txHashHex, err := ep.base58ToHex(txHash)
	if err != nil {
		ep.logger.Warn().
			Str("tx_hash", txHash).
			Err(err).
			Msg("failed to convert txHash to hex, using original value")
		txHashHex = txHash
	}

	// Since the event is confirmed, success is always true
	// Parse event data to extract error_msg if available
	var errorMsg string = ""

	if len(event.EventData) > 0 {
		var eventData map[string]interface{}
		if err := json.Unmarshal(event.EventData, &eventData); err == nil {
			// Check for error_msg field
			if errorMsgVal, ok := eventData["error_msg"].(string); ok {
				errorMsg = errorMsgVal
			}
		}
	}

	observation := &uexecutortypes.OutboundObservation{
		Success:     true, // Since event is confirmed, success is always true
		BlockHeight: event.BlockHeight,
		TxHash:      txHashHex,
		ErrorMsg:    errorMsg,
	}

	return observation, nil
}

