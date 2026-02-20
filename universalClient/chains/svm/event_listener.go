package svm

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// EventListener listens for gateway events on SVM chains and stores them in the database
type EventListener struct {
	// Core dependencies
	rpcClient  *RPCClient
	chainStore *common.ChainStore
	database   *db.DB

	// Configuration
	gatewayAddress           string
	chainID                  string
	discriminatorToEventType map[string]string
	eventPollingSeconds      int
	eventStartFrom           *int64

	// State
	logger  zerolog.Logger
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewEventListener creates a new SVM event listener
func NewEventListener(
	rpcClient *RPCClient,
	gatewayAddress string,
	chainID string,
	gatewayMethods []*uregistrytypes.GatewayMethods,
	database *db.DB,
	eventPollingSeconds int,
	eventStartFrom *int64,
	logger zerolog.Logger,
) (*EventListener, error) {
	if gatewayAddress == "" {
		return nil, fmt.Errorf("gateway address not configured")
	}

	if chainID == "" {
		return nil, fmt.Errorf("chain ID not configured")
	}

	// Build discriminator to event type mapping
	discriminatorToEventType := make(map[string]string)
	for _, method := range gatewayMethods {
		if method.EventIdentifier == "" {
			continue
		}
		switch method.Name {
		case EventTypeSendFunds,
			EventTypeExecuteUniversalTx,
			EventTypeRevertUniversalTx:
			discriminator := strings.ToLower(method.EventIdentifier)
			discriminatorToEventType[discriminator] = method.Name
		}
	}

	return &EventListener{
		rpcClient:                rpcClient,
		chainStore:               common.NewChainStore(database),
		database:                 database,
		gatewayAddress:           gatewayAddress,
		chainID:                  chainID,
		discriminatorToEventType: discriminatorToEventType,
		eventPollingSeconds:      eventPollingSeconds,
		eventStartFrom:           eventStartFrom,
		logger:                   logger.With().Str("component", "svm_event_listener").Str("chain", chainID).Logger(),
		stopCh:                   make(chan struct{}),
	}, nil
}

// Start begins listening for gateway events
func (el *EventListener) Start(ctx context.Context) error {
	if el.running {
		return fmt.Errorf("event listener is already running")
	}

	el.running = true
	el.stopCh = make(chan struct{})

	el.wg.Add(1)
	go el.listen(ctx)

	el.logger.Info().Msg("SVM event listener started")
	return nil
}

// Stop gracefully stops the event listener
func (el *EventListener) Stop() error {
	if !el.running {
		return nil
	}

	el.logger.Info().Msg("stopping SVM event listener")
	close(el.stopCh)
	el.running = false

	el.wg.Wait()
	el.logger.Info().Msg("SVM event listener stopped")
	return nil
}

// IsRunning returns whether the listener is currently running
func (el *EventListener) IsRunning() bool {
	return el.running
}

// listen is the main event listening loop
func (el *EventListener) listen(ctx context.Context) {
	defer el.wg.Done()

	// Get polling interval from config
	pollInterval := el.getPollingInterval()

	// Get starting slot
	fromSlot, err := el.getStartSlot(ctx)
	if err != nil {
		el.logger.Error().Err(err).Msg("failed to get start slot")
		return
	}

	el.logger.Info().
		Uint64("from_slot", fromSlot).
		Dur("poll_interval", pollInterval).
		Msg("starting event watching")

	currentSlot := fromSlot
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			el.logger.Info().Msg("context cancelled, stopping event listener")
			return
		case <-el.stopCh:
			el.logger.Info().Msg("stop signal received, stopping event listener")
			return
		case <-ticker.C:
			if err := el.processNewSlots(ctx, &currentSlot); err != nil {
				el.logger.Error().Err(err).Msg("failed to process new slots")
				// Continue processing on error
			}
		}
	}
}

// processNewSlots processes new slots since last processed slot
func (el *EventListener) processNewSlots(
	ctx context.Context,
	currentSlot *uint64,
) error {
	// Get latest slot
	latestSlot, err := el.rpcClient.GetLatestSlot(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest slot: %w", err)
	}

	// Skip if no new slots
	if *currentSlot >= latestSlot {
		return nil
	}

	// Process slots in range
	if err := el.processSlotRange(ctx, *currentSlot, latestSlot); err != nil {
		return fmt.Errorf("failed to process slot range: %w", err)
	}

	// Update last processed slot in database
	if err := el.updateLastProcessedSlot(latestSlot); err != nil {
		el.logger.Error().Err(err).Msg("failed to update last processed slot")
		// Don't return error - continue processing
	}

	// Move to next slot
	*currentSlot = latestSlot + 1
	return nil
}

// processSlotRange processes events in a range of slots
func (el *EventListener) processSlotRange(
	ctx context.Context,
	fromSlot, toSlot uint64,
) error {
	// Parse gateway address
	gatewayAddr, err := solana.PublicKeyFromBase58(el.gatewayAddress)
	if err != nil {
		return fmt.Errorf("invalid gateway address: %w", err)
	}

	// Get signatures for the gateway program
	signatures, err := el.rpcClient.GetSignaturesForAddress(ctx, gatewayAddr)
	if err != nil {
		return fmt.Errorf("failed to get signatures: %w", err)
	}

	// Process signatures in the slot range
	for _, sig := range signatures {
		if sig.Slot < fromSlot {
			continue
		}
		if sig.Slot > toSlot {
			break
		}

		// Get transaction details
		tx, err := el.rpcClient.GetTransaction(ctx, sig.Signature)
		if err != nil {
			el.logger.Error().
				Err(err).
				Str("signature", sig.Signature.String()).
				Msg("failed to get transaction")
			continue
		}

		// Process each log in the transaction
		if tx != nil && tx.Meta != nil && len(tx.Meta.LogMessages) > 0 {
			for logIndex, log := range tx.Meta.LogMessages {
				// Determine event type based on discriminator
				eventType := el.determineEventType(log)
				if eventType == "" {
					continue
				}

				// Parse gateway event from individual log
				event := ParseEvent(log, sig.Signature.String(), sig.Slot, uint(logIndex), eventType, el.chainID, el.logger)
				if event != nil {
					// Insert event if it doesn't already exist
					if stored, err := el.chainStore.InsertEventIfNotExists(event); err != nil {
						el.logger.Error().
							Err(err).
							Str("event_id", event.EventID).
							Str("type", event.Type).
							Uint64("slot", event.BlockHeight).
							Msg("failed to store event")
					} else if stored {
						el.logger.Debug().
							Str("event_id", event.EventID).
							Str("type", event.Type).
							Uint64("slot", event.BlockHeight).
							Str("confirmation_type", event.ConfirmationType).
							Msg("stored new event")
					}
				}
			}
		}
	}

	return nil
}

// getStartSlot returns the slot to start watching from
func (el *EventListener) getStartSlot(ctx context.Context) (uint64, error) {
	// Get chain height from store
	blockHeight, err := el.chainStore.GetChainHeight()
	if err != nil {
		return 0, fmt.Errorf("failed to get chain height: %w", err)
	}

	// If no previous state or invalid, check config
	if blockHeight == 0 {
		return el.getStartSlotFromConfig(ctx)
	}

	el.logger.Info().
		Uint64("slot", blockHeight).
		Msg("resuming from last processed slot")

	return blockHeight, nil
}

// getStartSlotFromConfig determines start slot from configuration
func (el *EventListener) getStartSlotFromConfig(ctx context.Context) (uint64, error) {
	// Check config for EventStartFrom
	if el.eventStartFrom != nil {
		if *el.eventStartFrom >= 0 {
			startSlot := uint64(*el.eventStartFrom)
			el.logger.Info().
				Uint64("slot", startSlot).
				Msg("no previous state found, starting from configured EventStartFrom")
			return startSlot, nil
		}

		// -1 means start from latest slot
		if *el.eventStartFrom == -1 {
			latestSlot, err := el.rpcClient.GetLatestSlot(ctx)
			if err != nil {
				el.logger.Warn().Err(err).Msg("failed to get latest slot, starting from 0")
				return 0, nil
			}
			el.logger.Info().
				Uint64("slot", latestSlot).
				Msg("no previous state found, starting from latest slot (EventStartFrom=-1)")
			return latestSlot, nil
		}
	}

	// No config, get latest slot
	el.logger.Info().Msg("no last processed slot found, starting from latest")
	return el.rpcClient.GetLatestSlot(ctx)
}

// updateLastProcessedSlot updates the last processed slot in the database
func (el *EventListener) updateLastProcessedSlot(slotNumber uint64) error {
	return el.chainStore.UpdateChainHeight(slotNumber)
}

// getPollingInterval returns the polling interval from config with default
func (el *EventListener) getPollingInterval() time.Duration {
	if el.eventPollingSeconds > 0 {
		return time.Duration(el.eventPollingSeconds) * time.Second
	}
	return 5 * time.Second // default
}

// determineEventType determines the event type based on the log discriminator
func (el *EventListener) determineEventType(log string) string {
	if !strings.HasPrefix(log, "Program data: ") {
		return ""
	}

	eventData := strings.TrimPrefix(log, "Program data: ")
	decoded, err := base64.StdEncoding.DecodeString(eventData)
	if err != nil {
		return ""
	}

	if len(decoded) < 8 {
		return ""
	}

	discriminator := strings.ToLower(hex.EncodeToString(decoded[:8]))

	// Look up event type from discriminator map
	eventType, ok := el.discriminatorToEventType[discriminator]
	if !ok {
		return ""
	}

	return eventType
}
