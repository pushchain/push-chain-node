package txresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// ---------------------------------------------------------------------------
// Resolver
// ---------------------------------------------------------------------------

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
type Resolver struct {
	eventStore     *eventstore.Store
	chains         *chains.Chains
	pushSigner     *pushsigner.Signer
	checkInterval  time.Duration
	logger         zerolog.Logger
	notFoundCounts map[string]int // eventID -> consecutive not-found count
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

// resolveEvent dispatches to the appropriate handler based on event type.
func (r *Resolver) resolveEvent(ctx context.Context, event *store.Event) {
	switch event.Type {
	case store.EventTypeSignOutbound:
		r.resolveOutbound(ctx, event)
	case store.EventTypeSignFundMigrate:
		r.resolveFundMigration(ctx, event)
	default:
		r.logger.Warn().Str("event_id", event.EventID).Str("type", event.Type).
			Msg("unknown broadcasted event type, skipping")
	}
}

// ---------------------------------------------------------------------------
// Outbound resolution (parsing + chain dispatch)
// ---------------------------------------------------------------------------

// resolveOutbound parses the CAIP tx hash and delegates to chain-specific resolution.
func (r *Resolver) resolveOutbound(ctx context.Context, event *store.Event) {
	chainID, rawTxHash, err := parseCAIPTxHash(event.BroadcastedTxHash)
	if err != nil {
		txID, utxID, extractErr := extractOutboundIDs(event)
		if extractErr != nil {
			r.logger.Warn().Err(extractErr).Str("event_id", event.EventID).
				Msg("invalid broadcasted tx hash and failed to extract outbound IDs")
			return
		}
		_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "0", "invalid broadcasted tx hash format")
		return
	}

	if !r.chains.IsChainOutboundEnabled(chainID) {
		r.logger.Warn().Str("chain", chainID).Str("event_id", event.EventID).
			Msg("outbound disabled, skipping resolution")
		return
	}

	if r.chains.IsEVMChain(chainID) {
		r.resolveOutboundEVM(ctx, event, chainID, rawTxHash)
	} else {
		r.resolveSVM(ctx, event, chainID)
	}
}

// ---------------------------------------------------------------------------
// Fund migration resolution (parsing + EVM resolution with explicit voting)
// ---------------------------------------------------------------------------

// resolveFundMigration resolves a SIGN_FUND_MIGRATE event.
// Unlike outbound where success voting is done by the destination chain event listener,
// fund migration requires the resolver to vote both success and failure explicitly
// since there is no gateway event to observe for native transfers.
func (r *Resolver) resolveFundMigration(ctx context.Context, event *store.Event) {
	chainID, rawTxHash, err := parseCAIPTxHash(event.BroadcastedTxHash)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).
			Msg("fund migration: invalid broadcasted tx hash format")
		return
	}

	var migrationData utsstypes.FundMigrationInitiatedEventData
	if err := json.Unmarshal(event.EventData, &migrationData); err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).
			Msg("fund migration: failed to parse event data")
		return
	}

	if r.chains.IsEVMChain(chainID) {
		r.resolveFundMigrationEVM(ctx, event, chainID, rawTxHash, migrationData.MigrationID)
	} else {
		r.logger.Warn().Str("chain", chainID).Str("event_id", event.EventID).
			Msg("fund migration resolution not supported for this chain type")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseCAIPTxHash(caipTxHash string) (chainID, txHash string, err error) {
	lastColon := strings.LastIndex(caipTxHash, ":")
	if lastColon <= 0 || lastColon == len(caipTxHash)-1 {
		return "", "", fmt.Errorf("invalid CAIP tx hash format: %s", caipTxHash)
	}
	return caipTxHash[:lastColon], caipTxHash[lastColon+1:], nil
}

func extractOutboundIDs(event *store.Event) (txID, utxID string, err error) {
	var data uexecutortypes.OutboundCreatedEvent
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return "", "", fmt.Errorf("failed to parse outbound event data: %w", err)
	}
	return data.TxID, data.UniversalTxId, nil
}

func (r *Resolver) getBuilder(chainID string) (common.TxBuilder, error) {
	client, err := r.chains.GetClient(chainID)
	if err != nil {
		return nil, err
	}
	return client.GetTxBuilder()
}

func (r *Resolver) verifyTxOnChain(ctx context.Context, chainID, txHash string) (bool, uint64, uint64, uint8, error) {
	builder, err := r.getBuilder(chainID)
	if err != nil {
		return false, 0, 0, 0, err
	}
	return builder.VerifyBroadcastedTx(ctx, txHash)
}

// voteOutboundFailureAndMarkReverted votes failure for an outbound event and marks it REVERTED.
func (r *Resolver) voteOutboundFailureAndMarkReverted(ctx context.Context, event *store.Event, txID, utxID, txHash string, blockHeight uint64, gasFeeUsed string, errorMsg string) error {
	if r.pushSigner == nil {
		r.logger.Warn().Str("event_id", event.EventID).Msg("pushSigner not configured, cannot vote failure")
		return nil
	}
	if gasFeeUsed == "" {
		gasFeeUsed = "0"
	}
	observation := &uexecutortypes.OutboundObservation{
		Success:     false,
		BlockHeight: blockHeight,
		TxHash:      txHash,
		ErrorMsg:    errorMsg,
		GasFeeUsed:  gasFeeUsed,
	}
	voteTxHash, err := r.pushSigner.VoteOutbound(ctx, txID, utxID, observation)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to vote outbound failure")
		return err
	}
	if err := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusReverted, "vote_tx_hash": voteTxHash}); err != nil {
		return fmt.Errorf("failed to mark event %s as reverted: %w", event.EventID, err)
	}
	r.logger.Info().
		Str("event_id", event.EventID).Str("tx_id", txID).
		Str("error_msg", errorMsg).Msg("voted outbound failure and marked REVERTED")
	return nil
}

// voteFundMigrationAndMark votes the fund migration result on Push chain and updates the event status.
func (r *Resolver) voteFundMigrationAndMark(ctx context.Context, event *store.Event, migrationID uint64, txHash string, success bool) {
	if r.pushSigner == nil {
		r.logger.Warn().Str("event_id", event.EventID).Msg("pushSigner not configured, cannot vote fund migration")
		return
	}

	voteTxHash, err := r.pushSigner.VoteFundMigration(ctx, migrationID, txHash, success)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Uint64("migration_id", migrationID).
			Msg("failed to vote fund migration")
		return
	}

	newStatus := store.StatusCompleted
	if !success {
		newStatus = store.StatusReverted
	}

	if err := r.eventStore.Update(event.EventID, map[string]any{"status": newStatus, "vote_tx_hash": voteTxHash}); err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to update fund migration event status")
		return
	}

	r.logger.Info().
		Str("event_id", event.EventID).Uint64("migration_id", migrationID).
		Str("tx_hash", txHash).Bool("success", success).Str("status", newStatus).
		Msg("voted fund migration and updated status")
}
