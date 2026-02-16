package reverthandler

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// Config holds configuration for the RevertHandler.
type Config struct {
	EventStore    *eventstore.Store
	PushCore      *pushcore.Client
	Chains        *chains.Chains
	PushSigner    *pushsigner.Signer
	CheckInterval time.Duration
	Logger        zerolog.Logger
}

// Handler finds FAILED or block-expired events and votes to revert them.
//
// Event lifecycle context:
//   - FAILED: TSS signing succeeded but post-signing step failed (broadcast or vote).
//     Set by SessionManager. Safe to vote failure immediately.
//   - Block-expired CONFIRMED: coordinator never picked up the event before expiry.
//     Safe to vote failure immediately.
//   - Block-expired BROADCASTED: tx was sent to external chain.
//     MUST verify tx status on-chain before voting to prevent double-spend.
//   - Block-expired IN_PROGRESS: session still active in SessionManager.
//     Skipped here — SessionManager's checkExpiredSessions handles these.
type Handler struct {
	eventStore    *eventstore.Store
	pushCore      *pushcore.Client
	chains        *chains.Chains
	pushSigner    *pushsigner.Signer
	checkInterval time.Duration
	logger        zerolog.Logger
}

// NewHandler creates a new RevertHandler.
func NewHandler(cfg Config) *Handler {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	return &Handler{
		eventStore:    cfg.EventStore,
		pushCore:      cfg.PushCore,
		chains:        cfg.Chains,
		pushSigner:    cfg.PushSigner,
		checkInterval: interval,
		logger:        cfg.Logger.With().Str("component", "revert_handler").Logger(),
	}
}

// Start begins the background loop that processes FAILED and block-expired events.
func (h *Handler) Start(ctx context.Context) {
	go h.run(ctx)
}

func (h *Handler) run(ctx context.Context) {
	ticker := time.NewTicker(h.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.processFailedEvents(ctx)
			h.processBlockExpiredEvents(ctx)
		}
	}
}

// processFailedEvents handles events where TSS succeeded but post-signing step failed.
func (h *Handler) processFailedEvents(ctx context.Context) {
	events, err := h.eventStore.GetFailedEvents(100)
	if err != nil {
		h.logger.Warn().Err(err).Msg("failed to get failed events")
		return
	}
	for i := range events {
		h.handleEvent(ctx, &events[i])
	}
}

// processBlockExpiredEvents handles events that passed their expiry block height.
func (h *Handler) processBlockExpiredEvents(ctx context.Context) {
	currentBlock, err := h.pushCore.GetLatestBlock(ctx)
	if err != nil {
		h.logger.Warn().Err(err).Msg("failed to get current block for expired events")
		return
	}

	events, err := h.eventStore.GetBlockExpiredEvents(currentBlock, 100)
	if err != nil {
		h.logger.Warn().Err(err).Msg("failed to get block-expired events")
		return
	}
	for i := range events {
		// IN_PROGRESS sessions are still live in SessionManager — its checkExpiredSessions
		// will clean them up and reset status to CONFIRMED for retry.
		if events[i].Status == eventstore.StatusInProgress {
			continue
		}
		h.handleEvent(ctx, &events[i])
	}
}

// handleEvent routes a single event to the appropriate handler based on type.
func (h *Handler) handleEvent(ctx context.Context, event *store.Event) {
	var err error
	switch event.Type {
	case string(coordinator.ProtocolSign):
		err = h.revertSignEvent(ctx, event)
	case string(coordinator.ProtocolKeygen), string(coordinator.ProtocolKeyrefresh), string(coordinator.ProtocolQuorumChange):
		err = h.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusReverted})
		if err == nil {
			h.logger.Info().Str("event_id", event.EventID).Str("type", event.Type).Msg("key event reverted")
		}
	default:
		h.logger.Warn().Str("event_id", event.EventID).Str("type", event.Type).Msg("unknown event type")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).
			Str("event_id", event.EventID).Str("type", event.Type).Str("status", event.Status).
			Msg("failed to process event")
	}
}

// revertSignEvent handles a SIGN (outbound) event.
// For BROADCASTED events with a tx hash, it verifies on-chain status first to prevent double-spend.
// For all other statuses (CONFIRMED, FAILED, BROADCASTED without hash), it votes failure directly.
func (h *Handler) revertSignEvent(ctx context.Context, event *store.Event) error {
	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		return err
	}

	// BROADCASTED with tx hash: must verify on external chain before voting
	if event.Status == eventstore.StatusBroadcasted && event.BroadcastedTxHash != "" {
		return h.verifyAndRevertBroadcasted(ctx, event, txID, utxID)
	}

	// All other cases: no tx reached external chain, safe to vote failure
	return h.voteFailure(ctx, event, txID, utxID, "", errorMsgForStatus(event.Status))
}

// verifyAndRevertBroadcasted checks a broadcasted tx on the external chain before deciding how to vote.
// This is the critical double-spend prevention path.
func (h *Handler) verifyAndRevertBroadcasted(ctx context.Context, event *store.Event, txID, utxID string) error {
	chainID, rawTxHash, err := parseCAIPTxHash(event.BroadcastedTxHash)
	if err != nil {
		// Can't parse the hash — treat as failed broadcast
		return h.voteFailure(ctx, event, txID, utxID, "", "invalid broadcasted tx hash format")
	}

	found, confirmations, txStatus, err := h.verifyTxOnChain(ctx, event.EventID, chainID, rawTxHash)
	if err != nil || !found {
		// RPC error or tx not found yet — skip, will retry next cycle
		return nil
	}

	h.logger.Info().
		Str("event_id", event.EventID).Str("tx_hash", rawTxHash).
		Uint64("confirmations", confirmations).Uint8("status", txStatus).
		Msg("broadcasted tx found on external chain")

	if txStatus == 1 {
		// Tx succeeded — do NOT vote failure. The destination chain's event parser
		// will observe the outbound event and vote success through the normal path.
		h.logger.Info().
			Str("event_id", event.EventID).Str("tx_hash", rawTxHash).
			Msg("broadcasted tx succeeded on-chain, skipping — event parser will handle")
		return nil
	}

	// Tx confirmed but reverted on-chain — safe to vote failure
	return h.voteFailure(ctx, event, txID, utxID, rawTxHash, "tx execution reverted on chain")
}

// verifyTxOnChain looks up a tx on the external chain via the chain's TxBuilder.
// Returns (found=false) with nil error if verification should be skipped/retried.
func (h *Handler) verifyTxOnChain(ctx context.Context, eventID, chainID, txHash string) (bool, uint64, uint8, error) {
	if h.chains == nil {
		h.logger.Warn().Str("event_id", eventID).Msg("chains not configured, cannot verify tx")
		return false, 0, 0, nil
	}

	client, err := h.chains.GetClient(chainID)
	if err != nil {
		h.logger.Warn().Err(err).Str("event_id", eventID).Str("chain", chainID).Msg("chain client unavailable, skipping")
		return false, 0, 0, nil
	}

	builder, err := client.GetTxBuilder()
	if err != nil {
		h.logger.Warn().Err(err).Str("event_id", eventID).Str("chain", chainID).Msg("tx builder unavailable, skipping")
		return false, 0, 0, nil
	}

	found, confirmations, status, err := builder.VerifyBroadcastedTx(ctx, txHash)
	if err != nil {
		h.logger.Warn().Err(err).Str("event_id", eventID).Str("tx_hash", txHash).Msg("tx verification error, skipping")
		return false, 0, 0, nil
	}

	if !found {
		h.logger.Debug().Str("event_id", eventID).Str("tx_hash", txHash).Msg("broadcasted tx not found on-chain, will retry")
	}

	return found, confirmations, status, nil
}

// voteFailure votes failure for a SIGN event and marks it REVERTED.
func (h *Handler) voteFailure(ctx context.Context, event *store.Event, txID, utxID, txHash, errorMsg string) error {
	if h.pushSigner == nil {
		return errors.New("pushSigner not configured — cannot vote to revert")
	}

	observation := &uexecutortypes.OutboundObservation{
		Success:  false,
		TxHash:   txHash,
		ErrorMsg: errorMsg,
	}

	voteTxHash, err := h.pushSigner.VoteOutbound(ctx, txID, utxID, observation)
	if err != nil {
		return errors.Wrapf(err, "failed to vote failure for event %s", event.EventID)
	}

	if err := h.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusReverted}); err != nil {
		return errors.Wrapf(err, "failed to mark event %s as reverted", event.EventID)
	}

	h.logger.Info().
		Str("event_id", event.EventID).Str("tx_id", txID).
		Str("vote_tx_hash", voteTxHash).Str("error_msg", errorMsg).
		Msg("voted failure and marked REVERTED")

	return nil
}

// --- Helpers ---

// extractOutboundIDs parses the event data to get txID and universalTxId.
func extractOutboundIDs(event *store.Event) (txID, utxID string, err error) {
	var data uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return "", "", errors.Wrap(err, "failed to parse outbound event data")
	}
	if data.TxID == "" {
		return "", "", errors.Errorf("outbound event %s missing tx_id", event.EventID)
	}
	return data.TxID, data.UniversalTxId, nil
}

// errorMsgForStatus returns a human-readable error message for the event's current status.
func errorMsgForStatus(status string) string {
	switch status {
	case eventstore.StatusFailed:
		return "TSS succeeded but broadcast/vote failed"
	case eventstore.StatusConfirmed:
		return "event expired before TSS started"
	case eventstore.StatusBroadcasted:
		return "broadcast attempted but tx not found on chain"
	default:
		return "event expired or failed"
	}
}

// parseCAIPTxHash parses "{chainId}:{txHash}" (e.g. "eip155:11155111:0xabc").
// Chain IDs contain colons, so we split on the last one.
func parseCAIPTxHash(caipTxHash string) (chainID, txHash string, err error) {
	lastColon := strings.LastIndex(caipTxHash, ":")
	if lastColon <= 0 || lastColon == len(caipTxHash)-1 {
		return "", "", errors.Errorf("invalid CAIP tx hash format: %s", caipTxHash)
	}
	return caipTxHash[:lastColon], caipTxHash[lastColon+1:], nil
}
