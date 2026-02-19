package txbroadcaster

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// SigningData holds the signing parameters persisted by sessionManager when marking SIGNED.
type SigningData struct {
	Signature   string `json:"signature"`    // hex-encoded 64/65 byte signature
	SigningHash string `json:"signing_hash"` // hex-encoded signing hash
	Nonce       uint64 `json:"nonce"`
	GasPrice    string `json:"gas_price"` // string for big.Int
}

// SignedEventData wraps OutboundCreatedEvent with signing data appended by sessionManager.
type SignedEventData struct {
	uexecutortypes.OutboundCreatedEvent
	SigningData *SigningData `json:"signing_data,omitempty"`
}

// Config holds configuration for the broadcaster.
type Config struct {
	EventStore     *eventstore.Store
	Chains         *chains.Chains
	CheckInterval  time.Duration
	Logger         zerolog.Logger
	GetTSSAddress  func(ctx context.Context) (string, error)
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

func (b *Broadcaster) broadcastEvent(ctx context.Context, event *store.Event) {
	data, err := parseSignedEventData(event.EventData)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to parse signed event data")
		return
	}

	chainID := data.DestinationChain
	if b.chains.IsEVMChain(chainID) {
		b.broadcastEVM(ctx, event, data, chainID)
	} else {
		b.broadcastSVM(ctx, event, data, chainID)
	}
}

// parseSignedEventData unmarshals EventData into SignedEventData (OutboundCreatedEvent + SigningData).
func parseSignedEventData(eventData []byte) (*SignedEventData, error) {
	var data SignedEventData
	if err := json.Unmarshal(eventData, &data); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal signed event data")
	}
	if data.SigningData == nil {
		return nil, errors.New("signing_data missing from event data")
	}
	return &data, nil
}

// markBroadcasted updates the event status to BROADCASTED with the given tx hash.
func (b *Broadcaster) markBroadcasted(event *store.Event, chainID, txHash string) {
	caipTxHash := chainID + ":" + txHash
	if err := b.eventStore.Update(event.EventID, map[string]any{
		"broadcasted_tx_hash": caipTxHash,
		"status":              eventstore.StatusBroadcasted,
	}); err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to update event to BROADCASTED")
		return
	}
	b.logger.Info().Str("event_id", event.EventID).Str("tx_hash", txHash).Str("chain", chainID).
		Msg("marked BROADCASTED")
}

// reconstructSigningReq rebuilds UnSignedOutboundTxReq from persisted SigningData.
func reconstructSigningReq(sd *SigningData) (*common.UnSignedOutboundTxReq, error) {
	signingHash, err := hex.DecodeString(sd.SigningHash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode signing hash")
	}

	gasPrice := new(big.Int)
	if sd.GasPrice != "" {
		if _, ok := gasPrice.SetString(sd.GasPrice, 10); !ok {
			return nil, errors.Errorf("invalid gas_price: %s", sd.GasPrice)
		}
	}

	return &common.UnSignedOutboundTxReq{
		SigningHash: signingHash,
		Nonce:      sd.Nonce,
		GasPrice:   gasPrice,
	}, nil
}
