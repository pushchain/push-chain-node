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
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Warn (not refuse) once a single poll has processed this many in-range sigs;
// re-emitted per subsequent page so ops sees a sustained signal, not a blip.
const largePollWarnThreshold uint64 = 100_000

// rpcClientInterface is the subset of *RPCClient methods the listener depends on.
// Defined as an interface so tests can supply a mock without spinning up a real
// JSON-RPC server. *RPCClient satisfies it implicitly.
type rpcClientInterface interface {
	GetLatestSlot(ctx context.Context) (uint64, error)
	GetSignaturesForAddress(ctx context.Context, address solana.PublicKey, before solana.Signature) ([]*solanarpc.TransactionSignature, error)
	GetTransaction(ctx context.Context, signature solana.Signature) (*solanarpc.GetTransactionResult, error)
}

// EventListener listens for gateway events on SVM chains and stores them in the database
type EventListener struct {
	// Core dependencies
	rpcClient  rpcClientInterface
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
	rpcClient rpcClientInterface,
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
			EventTypeFinalizeUniversalTx,
			EventTypeRevertUniversalTx,
			EventTypeFundsRescued:
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

	return nil
}

// Stop gracefully stops the event listener
func (el *EventListener) Stop() error {
	if !el.running {
		return nil
	}

	el.logger.Debug().Msg("stopping SVM event listener")
	close(el.stopCh)
	el.running = false

	el.wg.Wait()
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

	el.logger.Debug().
		Uint64("from_slot", fromSlot).
		Dur("poll_interval", pollInterval).
		Msg("starting event watching")

	currentSlot := fromSlot
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			el.logger.Debug().Msg("context cancelled, stopping event listener")
			return
		case <-el.stopCh:
			el.logger.Debug().Msg("stop signal received, stopping event listener")
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

	// Per-page streaming so memory stays bounded on long bootstraps. Termination
	// and cursor use min(slot) of the batch — per
	// https://github.com/solana-labs/solana/issues/22456 in-page order is not
	// guaranteed descending, so batch[len-1] would risk an early break.
	var beforeSig solana.Signature
	var processedInRange uint64
	for page := 0; ; page++ {
		batch, err := el.rpcClient.GetSignaturesForAddress(ctx, gatewayAddr, beforeSig)
		if err != nil {
			return fmt.Errorf("failed to get signatures (page %d): %w", page, err)
		}
		if len(batch) == 0 {
			break
		}

		processed, err := el.processSignatureBatch(ctx, batch, fromSlot, toSlot)
		if err != nil {
			return err
		}
		processedInRange += processed
		if processedInRange >= largePollWarnThreshold {
			el.logger.Warn().
				Uint64("processed_in_range", processedInRange).
				Uint64("threshold", largePollWarnThreshold).
				Uint64("from_slot", fromSlot).
				Uint64("to_slot", toSlot).
				Int("pages", page+1).
				Msg("large signature backlog being processed; if this is unexpected, " +
					"restart with EventStartFrom set to -1 (latest) or a recent slot, " +
					"and verify the RPC tier can sustain the request volume")
		}

		minSlot := batch[0].Slot
		minSig := batch[0].Signature
		for _, s := range batch[1:] {
			if s.Slot < minSlot {
				minSlot = s.Slot
				minSig = s.Signature
			}
		}

		if minSlot < fromSlot {
			break
		}
		beforeSig = minSig
	}

	return nil
}

// Processes in-range sigs from `batch`, returns how many. `continue` on both
// bounds so it tolerates any in-page order.
func (el *EventListener) processSignatureBatch(
	ctx context.Context,
	batch []*solanarpc.TransactionSignature,
	fromSlot, toSlot uint64,
) (uint64, error) {
	var processed uint64
	for _, sig := range batch {
		if sig.Slot < fromSlot {
			continue
		}
		if sig.Slot > toSlot {
			continue
		}
		processed++

		// Get transaction details
		tx, err := el.rpcClient.GetTransaction(ctx, sig.Signature)
		if err != nil {
			el.logger.Error().
				Err(err).
				Str("signature", sig.Signature.String()).
				Msg("failed to get transaction")
			continue
		}

		// Process each log in the transaction.
		// getSignaturesForAddress returns any tx that merely references the
		// gateway in accountKeys, and a discriminator is a schema tag rather than
		// an authenticator. So track the invocation stack and accept a
		// "Program data:" line only while the gateway is the executing program;
		// otherwise any program could emit a forged gateway event.
		if tx != nil && tx.Meta != nil && len(tx.Meta.LogMessages) > 0 {
			// Surface truncation loudly: a gateway event may have been dropped and
			// is unrecoverable from RPC, so the deposit needs manual reconciliation.
			// Visible logs are still processed, since events before the cut are real.
			if logsTruncated(tx.Meta.LogMessages) {
				el.logger.Error().
					Str("signature", sig.Signature.String()).
					Uint64("slot", sig.Slot).
					Msg("solana log buffer truncated; a gateway event may have been dropped and needs manual review")
			}

			fromGateway := gatewayEmittedLogs(tx.Meta.LogMessages, el.gatewayAddress)
			for logIndex, log := range tx.Meta.LogMessages {
				if !fromGateway[logIndex] {
					continue
				}

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

	return processed, nil
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

// invokedProgram returns the program ID from a "Program <id> invoke [<depth>]"
// runtime log. Programs cannot emit these: sol_log and sol_log_data are always
// prefixed with "Program log: " / "Program data: ", so the invoke and exit lines
// are runtime-generated and safe to build an attribution stack from.
func invokedProgram(log string) (string, bool) {
	const prefix = "Program "
	if !strings.HasPrefix(log, prefix) {
		return "", false
	}
	rest := log[len(prefix):]
	idx := strings.Index(rest, " invoke [")
	if idx <= 0 {
		return "", false
	}
	programID := rest[:idx]
	if _, err := solana.PublicKeyFromBase58(programID); err != nil {
		return "", false
	}
	return programID, true
}

// gatewayEmittedLogs returns the indexes of "Program data:" lines emitted while
// gatewayAddress was the executing program, walking the invoke/exit stack.
// Lines emitted by any other program are excluded: a discriminator identifies an
// encoding schema, not the emitter, so without this any program could log a
// well-formed gateway event and have it observed as a real deposit.
func gatewayEmittedLogs(logs []string, gatewayAddress string) map[int]bool {
	emitted := make(map[int]bool)
	var stack []string
	for i, log := range logs {
		if programID, ok := invokedProgram(log); ok {
			stack = append(stack, programID)
			continue
		}
		if isProgramExit(log) {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if !strings.HasPrefix(log, "Program data: ") {
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1] == gatewayAddress {
			emitted[i] = true
		}
	}
	return emitted
}

// logsTruncated reports whether the runtime dropped part of this transaction's
// log buffer. Programs can only emit "Program log:" and "Program data:" lines,
// so a bare line is runtime-generated and cannot be spoofed. Matching on the
// word rather than one exact literal keeps this working if the wording changes.
//
// It matters because gateway events are emitted with sol_log_data: once the
// buffer overflows the event line is gone, and no RPC call can recover it. The
// deposit would otherwise be missed in silence.
func logsTruncated(logs []string) bool {
	for _, log := range logs {
		if strings.HasPrefix(log, "Program ") {
			continue
		}
		if strings.Contains(strings.ToLower(log), "truncated") {
			return true
		}
	}
	return false
}

// isProgramExit reports whether log ends an invocation frame, i.e.
// "Program <id> success" or "Program <id> failed: ...".
// The program ID must parse as a pubkey: otherwise a program logging "success"
// emits "Program log: success", which would pop a frame it does not own and let
// a later log be attributed to its caller.
func isProgramExit(log string) bool {
	const prefix = "Program "
	if !strings.HasPrefix(log, prefix) {
		return false
	}
	rest := log[len(prefix):]
	sp := strings.IndexByte(rest, ' ')
	if sp <= 0 {
		return false
	}
	if _, err := solana.PublicKeyFromBase58(rest[:sp]); err != nil {
		return false
	}
	tail := rest[sp+1:]
	return tail == "success" || strings.HasPrefix(tail, "failed")
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
