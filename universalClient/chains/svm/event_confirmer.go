package svm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/rs/zerolog"

	chaincommon "github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
)

// EventConfirmer periodically checks pending events and marks them as CONFIRMED
// once their transactions are confirmed on-chain.
type EventConfirmer struct {
	rpcClient             *RPCClient
	chainStore            *chaincommon.ChainStore
	chainID               string
	pollIntervalSeconds   int
	fastConfirmations     uint64
	standardConfirmations uint64
	logger                zerolog.Logger
	stopCh                chan struct{}
	wg                    sync.WaitGroup
}

// NewEventConfirmer creates a new event confirmer
func NewEventConfirmer(
	rpcClient *RPCClient,
	database *db.DB,
	chainID string,
	pollIntervalSeconds int,
	fastConfirmations uint64,
	standardConfirmations uint64,
	logger zerolog.Logger,
) *EventConfirmer {
	return &EventConfirmer{
		rpcClient:             rpcClient,
		chainStore:            chaincommon.NewChainStore(database),
		chainID:               chainID,
		pollIntervalSeconds:   pollIntervalSeconds,
		fastConfirmations:     fastConfirmations,
		standardConfirmations: standardConfirmations,
		logger:                logger.With().Str("component", "svm_event_confirmer").Str("chain", chainID).Logger(),
		stopCh:                make(chan struct{}),
	}
}

// Start begins checking and confirming events
func (ec *EventConfirmer) Start(ctx context.Context) error {
	ec.wg.Add(1)
	go ec.checkAndConfirmEvents(ctx)
	return nil
}

// Stop stops the event confirmer
func (ec *EventConfirmer) Stop() {
	close(ec.stopCh)
	ec.wg.Wait()
}

// checkAndConfirmEvents periodically fetches pending events and checks if they are confirmed
func (ec *EventConfirmer) checkAndConfirmEvents(ctx context.Context) {
	defer ec.wg.Done()

	interval := time.Duration(ec.pollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ec.logger.Info().
		Dur("interval", interval).
		Msg("starting event confirmation checking")

	for {
		select {
		case <-ctx.Done():
			ec.logger.Info().Msg("context cancelled, stopping event confirmer")
			return
		case <-ec.stopCh:
			ec.logger.Info().Msg("stop signal received, stopping event confirmer")
			return
		case <-ticker.C:
			if err := ec.processPendingEvents(ctx); err != nil {
				ec.logger.Error().Err(err).Msg("failed to process pending events")
			}
		}
	}
}

// processPendingEvents fetches oldest 1000 pending events and checks if they are confirmed
func (ec *EventConfirmer) processPendingEvents(ctx context.Context) error {
	// Get latest slot
	latestSlot, err := ec.rpcClient.GetLatestSlot(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest slot: %w", err)
	}

	// Fetch oldest 1000 pending events (all types)
	pendingEvents, err := ec.chainStore.GetPendingEvents(1000)
	if err != nil {
		return fmt.Errorf("failed to query pending events: %w", err)
	}

	if len(pendingEvents) == 0 {
		return nil
	}

	ec.logger.Debug().
		Int("count", len(pendingEvents)).
		Msg("checking pending events for confirmation")

	confirmedCount := 0
	for _, event := range pendingEvents {
		// If we don't have a block height, skip
		if event.BlockHeight == 0 {
			continue
		}

		// Extract transaction signature from EventID (format: "signature:logIndex")
		txSignatureStr := ec.getTxSignatureFromEventID(event.EventID)
		if txSignatureStr == "" {
			ec.logger.Debug().
				Str("event_id", event.EventID).
				Uint64("slot", event.BlockHeight).
				Msg("failed to extract tx signature from event ID, skipping")
			continue
		}

		// Parse signature
		sig, err := solana.SignatureFromBase58(txSignatureStr)
		if err != nil {
			ec.logger.Debug().
				Str("event_id", event.EventID).
				Str("signature", txSignatureStr).
				Err(err).
				Msg("failed to parse signature, skipping")
			continue
		}

		// Get transaction
		tx, err := ec.rpcClient.GetTransaction(ctx, sig)
		if err != nil {
			// Transaction not found or not yet confirmed - skip
			continue
		}

		// Check if transaction is confirmed
		if tx.Meta == nil {
			continue
		}

		// Get transaction slot
		txSlot := tx.Slot
		if txSlot == 0 {
			// If slot is 0, use block height from event
			txSlot = event.BlockHeight
		}

		// Check if transaction is confirmed based on confirmation type
		requiredConfirmations := ec.getRequiredConfirmations(event.ConfirmationType)
		confirmations := latestSlot - txSlot

		if confirmations >= requiredConfirmations {
			// Update event status to CONFIRMED
			rowsAffected, err := ec.chainStore.UpdateEventStatus(event.EventID, "PENDING", "CONFIRMED")
			if err != nil {
				ec.logger.Error().
					Err(err).
					Str("event_id", event.EventID).
					Msg("failed to update event status")
				continue
			}

			if rowsAffected > 0 {
				confirmedCount++
				ec.logger.Info().
					Str("event_id", event.EventID).
					Str("tx_signature", txSignatureStr).
					Uint64("slot", txSlot).
					Uint64("latest", latestSlot).
					Uint64("confirmations", confirmations).
					Uint64("required", requiredConfirmations).
					Str("confirmation_type", event.ConfirmationType).
					Msg("event confirmed and marked as CONFIRMED")
			}
		}
	}

	if confirmedCount > 0 {
		ec.logger.Info().
			Int("confirmed_count", confirmedCount).
			Msg("confirmed events")
	}

	return nil
}

// getTxSignatureFromEventID extracts the transaction signature from EventID (format: "signature:logIndex")
func (ec *EventConfirmer) getTxSignatureFromEventID(eventID string) string {
	// EventID format: "signature:logIndex"
	parts := strings.Split(eventID, ":")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// getRequiredConfirmations returns the required number of confirmations based on confirmation type
func (ec *EventConfirmer) getRequiredConfirmations(confirmationType string) uint64 {
	switch confirmationType {
	case "FAST":
		if ec.fastConfirmations > 0 {
			return ec.fastConfirmations
		}
		return 5
	case "STANDARD":
		if ec.standardConfirmations > 0 {
			return ec.standardConfirmations
		}
		return 12
	default:
		// Default to standard if unknown
		if ec.standardConfirmations > 0 {
			return ec.standardConfirmations
		}
		return 12
	}
}
