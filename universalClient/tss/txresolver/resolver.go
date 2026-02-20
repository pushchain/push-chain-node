package txresolver

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// Config holds configuration for the tx resolver.
type Config struct {
	EventStore    *eventstore.Store
	Chains        *chains.Chains
	PushSigner    *pushsigner.Signer
	CheckInterval time.Duration
	Logger        zerolog.Logger
}

// maxNotFoundRetries is the number of consecutive "not found" checks before reverting.
// At a 30s check interval this gives ~5 minutes for a tx to appear on chain.
const maxNotFoundRetries = 10

// Resolver takes BROADCASTED txs and moves them to terminal status (COMPLETED or REVERTED).
//
// Chain-specific behavior:
//   - EVM: Checks on-chain receipt. Success (status=1) → COMPLETED. Failure (status=0)
//     or tx not found after retries → vote failure on Push chain and REVERT (refunds user).
//   - SVM (Solana): Marks COMPLETED immediately. Solana nonces only increment on success,
//     so there's no "reverted receipt" to detect. Success/failure voting comes from
//     destination chain event listening instead.
type Resolver struct {
	eventStore     *eventstore.Store
	chains         *chains.Chains
	pushSigner     *pushsigner.Signer
	checkInterval  time.Duration
	logger         zerolog.Logger
	notFoundCounts map[string]int // eventID → consecutive not-found count
}

// NewResolver creates a new tx resolver.
func NewResolver(cfg Config) *Resolver {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = 15 * time.Second
	}
	return &Resolver{
		eventStore:     cfg.EventStore,
		chains:         cfg.Chains,
		pushSigner:     cfg.PushSigner,
		checkInterval:  interval,
		logger:         cfg.Logger.With().Str("component", "txresolver").Logger(),
		notFoundCounts: make(map[string]int),
	}
}

// Start begins the background loop.
func (r *Resolver) Start(ctx context.Context) {
	go r.run(ctx)
}

func (r *Resolver) run(ctx context.Context) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.processBroadcasted(ctx)
		}
	}
}

const processBroadcastedBatchSize = 100

// processBroadcasted drains all BROADCASTED SIGN events in batches.
func (r *Resolver) processBroadcasted(ctx context.Context) {
	if r.chains == nil {
		return
	}
	for {
		events, err := r.eventStore.GetBroadcastedSignEvents(processBroadcastedBatchSize)
		if err != nil {
			r.logger.Warn().Err(err).Msg("failed to get broadcasted sign events")
			return
		}
		if len(events) == 0 {
			return
		}
		for i := range events {
			r.resolveEvent(ctx, &events[i])
		}
		if len(events) < processBroadcastedBatchSize {
			return
		}
	}
}

func (r *Resolver) resolveEvent(ctx context.Context, event *store.Event) {
	chainID, rawTxHash, err := parseCAIPTxHash(event.BroadcastedTxHash)
	if err != nil {
		txID, utxID, extractErr := extractOutboundIDs(event)
		if extractErr != nil {
			r.logger.Warn().Err(extractErr).Str("event_id", event.EventID).Msg("invalid broadcasted tx hash and failed to extract outbound IDs")
			return
		}
		_ = r.voteFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "invalid broadcasted tx hash format")
		return
	}

	if r.chains.IsEVMChain(chainID) {
		r.resolveEVM(ctx, event, chainID, rawTxHash)
		return
	}

	// Solana (or other non-EVM): mark COMPLETED immediately
	r.resolveSVM(event, chainID)
}

func (r *Resolver) voteFailureAndMarkReverted(ctx context.Context, event *store.Event, txID, utxID, txHash string, blockHeight uint64, errorMsg string) error {
	if r.pushSigner == nil {
		r.logger.Warn().Str("event_id", event.EventID).Msg("pushSigner not configured, cannot vote failure")
		return nil
	}
	observation := &uexecutortypes.OutboundObservation{
		Success:     false,
		BlockHeight: blockHeight,
		TxHash:      txHash,
		ErrorMsg:    errorMsg,
	}
	voteTxHash, err := r.pushSigner.VoteOutbound(ctx, txID, utxID, observation)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to vote failure")
		return err
	}
	if err := r.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusReverted, "vote_tx_hash": voteTxHash}); err != nil {
		return errors.Wrapf(err, "failed to mark event %s as reverted", event.EventID)
	}
	r.logger.Info().
		Str("event_id", event.EventID).Str("tx_id", txID).
		Str("error_msg", errorMsg).Msg("voted failure and marked REVERTED")
	return nil
}

func extractOutboundIDs(event *store.Event) (txID, utxID string, err error) {
	var data uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return "", "", errors.Wrap(err, "failed to parse outbound event data")
	}
	return data.TxID, data.UniversalTxId, nil
}

func parseCAIPTxHash(caipTxHash string) (chainID, txHash string, err error) {
	lastColon := strings.LastIndex(caipTxHash, ":")
	if lastColon <= 0 || lastColon == len(caipTxHash)-1 {
		return "", "", errors.Errorf("invalid CAIP tx hash format: %s", caipTxHash)
	}
	return caipTxHash[:lastColon], caipTxHash[lastColon+1:], nil
}
