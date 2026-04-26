package txbroadcaster

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// ---------------------------------------------------------------------------
// Signed event data types
// ---------------------------------------------------------------------------

// SigningData holds the signing parameters persisted by sessionManager when marking SIGNED.
type SigningData struct {
	Signature              string   `json:"signature"`    // hex-encoded 64/65 byte signature
	SigningHash            string   `json:"signing_hash"` // hex-encoded signing hash
	Nonce                  uint64   `json:"nonce"`
	TSSFundMigrationAmount *big.Int `json:"tss_fund_migration_amount,omitempty"`
}

// SignedOutboundData wraps OutboundCreatedEvent with signing data.
type SignedOutboundData struct {
	uexecutortypes.OutboundCreatedEvent
	SigningData *SigningData `json:"signing_data,omitempty"`
}

// SignedFundMigrationData wraps FundMigrationInitiatedEventData with signing data.
type SignedFundMigrationData struct {
	utsstypes.FundMigrationInitiatedEventData
	SigningData *SigningData `json:"signing_data,omitempty"`
}

// ---------------------------------------------------------------------------
// Broadcaster
// ---------------------------------------------------------------------------

// Config holds configuration for the broadcaster.
type Config struct {
	EventStore    *eventstore.Store
	Chains        *chains.Chains
	CheckInterval time.Duration
	Logger        zerolog.Logger
	GetTSSAddress func(ctx context.Context) (string, error)
}

// Broadcaster polls SIGNED events and broadcasts them to external chains.
type Broadcaster struct {
	eventStore    *eventstore.Store
	chains        *chains.Chains
	checkInterval time.Duration
	logger        zerolog.Logger
	getTSSAddress func(ctx context.Context) (string, error)
}

// NewBroadcaster creates a new tx broadcaster.
func NewBroadcaster(cfg Config) *Broadcaster {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = 15 * time.Second
	}
	return &Broadcaster{
		eventStore:    cfg.EventStore,
		chains:        cfg.Chains,
		checkInterval: interval,
		logger:        cfg.Logger.With().Str("component", "txbroadcaster").Logger(),
		getTSSAddress: cfg.GetTSSAddress,
	}
}

// Start begins the background broadcast loop.
func (b *Broadcaster) Start(ctx context.Context) {
	go b.run(ctx)
}

func (b *Broadcaster) run(ctx context.Context) {
	ticker := time.NewTicker(b.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.processSigned(ctx)
		}
	}
}

const processSignedBatchSize = 100

// processSigned drains all SIGNED events in batches.
func (b *Broadcaster) processSigned(ctx context.Context) {
	if b.chains == nil {
		return
	}
	for {
		events, err := b.eventStore.GetSignedSignEvents(processSignedBatchSize)
		if err != nil {
			b.logger.Warn().Err(err).Msg("failed to get signed events")
			return
		}
		if len(events) == 0 {
			return
		}
		for i := range events {
			b.broadcastEvent(ctx, &events[i])
		}
		if len(events) < processSignedBatchSize {
			return
		}
	}
}

// broadcastEvent dispatches to the appropriate handler based on event type.
func (b *Broadcaster) broadcastEvent(ctx context.Context, event *store.Event) {
	switch event.Type {
	case store.EventTypeSignOutbound:
		b.broadcastOutbound(ctx, event)
	case store.EventTypeSignFundMigrate:
		b.broadcastFundMigration(ctx, event)
	default:
		b.logger.Warn().Str("event_id", event.EventID).Str("type", event.Type).
			Msg("unknown signed event type, skipping")
	}
}

// ---------------------------------------------------------------------------
// Outbound broadcast (parsing + chain dispatch)
// ---------------------------------------------------------------------------

// broadcastOutbound parses outbound event data and delegates to chain-specific broadcast.
func (b *Broadcaster) broadcastOutbound(ctx context.Context, event *store.Event) {
	var data SignedOutboundData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to parse signed outbound data")
		return
	}
	if data.SigningData == nil {
		b.logger.Warn().Str("event_id", event.EventID).Msg("signing_data missing from outbound event")
		return
	}

	chainID := data.DestinationChain
	if !b.chains.IsChainOutboundEnabled(chainID) {
		b.logger.Warn().Str("chain", chainID).Str("event_id", event.EventID).
			Msg("outbound disabled, skipping broadcast")
		return
	}

	if b.chains.IsEVMChain(chainID) {
		b.broadcastEVM(ctx, event, &data, chainID)
	} else {
		b.broadcastSVM(ctx, event, &data, chainID)
	}
}

// ---------------------------------------------------------------------------
// Fund migration broadcast (parsing + chain dispatch)
// ---------------------------------------------------------------------------

// broadcastFundMigration parses fund migration event data and delegates to chain-specific broadcast.
func (b *Broadcaster) broadcastFundMigration(ctx context.Context, event *store.Event) {
	var data SignedFundMigrationData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to parse fund migration signed data")
		return
	}
	if data.SigningData == nil {
		b.logger.Warn().Str("event_id", event.EventID).Msg("signing_data missing from fund migration event")
		return
	}

	chainID := data.Chain

	if b.chains.IsEVMChain(chainID) {
		b.broadcastFundMigrationEVM(ctx, event, &data, chainID)
	} else {
		b.logger.Warn().Str("chain", chainID).Str("event_id", event.EventID).
			Msg("fund migration not supported for this chain type")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// decodeSigningData extracts the UnsignedSigningReq and raw signature bytes from SigningData.
func decodeSigningData(sd *SigningData) (*common.UnsignedSigningReq, []byte, error) {
	signingHash, err := hex.DecodeString(sd.SigningHash)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode signing hash: %w", err)
	}

	signature, err := hex.DecodeString(sd.Signature)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode signature: %w", err)
	}

	return &common.UnsignedSigningReq{
		SigningHash:            signingHash,
		Nonce:                  sd.Nonce,
		TSSFundMigrationAmount: sd.TSSFundMigrationAmount,
	}, signature, nil
}

// markBroadcasted updates the event status to BROADCASTED with the given tx hash.
func (b *Broadcaster) markBroadcasted(event *store.Event, chainID, txHash string) {
	caipTxHash := chainID + ":" + txHash
	if err := b.eventStore.Update(event.EventID, map[string]any{
		"broadcasted_tx_hash": caipTxHash,
		"status":              store.StatusBroadcasted,
	}); err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to update event to BROADCASTED")
		return
	}
	b.logger.Info().Str("event_id", event.EventID).Str("tx_hash", txHash).Str("chain", chainID).
		Msg("marked BROADCASTED")
}
