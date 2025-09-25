package svm

import (
	"context"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
)

// EventWatcher handles watching for events on Solana chains
type EventWatcher struct {
	parentClient *Client
	gatewayAddr  solana.PublicKey
	eventParser  *EventParser
	tracker      *common.ConfirmationTracker
	txVerifier   *TransactionVerifier
	appConfig    *config.Config
	chainID      string
	logger       zerolog.Logger
}

// NewEventWatcher creates a new event watcher
func NewEventWatcher(
	parentClient *Client,
	gatewayAddr solana.PublicKey,
	eventParser *EventParser,
	tracker *common.ConfirmationTracker,
	txVerifier *TransactionVerifier,
	appConfig *config.Config,
	chainID string,
	logger zerolog.Logger,
) *EventWatcher {
	return &EventWatcher{
		parentClient: parentClient,
		gatewayAddr:  gatewayAddr,
		eventParser:  eventParser,
		tracker:      tracker,
		txVerifier:   txVerifier,
		appConfig:    appConfig,
		chainID:      chainID,
		logger:       logger.With().Str("component", "svm_event_watcher").Logger(),
	}
}

// WatchEvents starts watching for events from a specific slot
func (ew *EventWatcher) WatchEvents(
	ctx context.Context,
	fromSlot uint64,
	updateLastSlot func(uint64) error,
	verifyTransactions func(context.Context) error,
) (<-chan *common.GatewayEvent, error) {
	// Use buffered channel to prevent blocking producers
	eventChan := make(chan *common.GatewayEvent, 100)

	go func() {
		defer close(eventChan)

		// Use chain-specific polling interval, then global, then default to 5 seconds
		pollingInterval := 5 * time.Second

		// Check chain-specific config first
		if ew.appConfig != nil && ew.appConfig.ChainConfigs != nil {
			if chainConfig, exists := ew.appConfig.ChainConfigs[ew.chainID]; exists {
				if chainConfig.EventPollingIntervalSeconds != nil && *chainConfig.EventPollingIntervalSeconds > 0 {
					pollingInterval = time.Duration(*chainConfig.EventPollingIntervalSeconds) * time.Second
				} else if ew.appConfig.EventPollingIntervalSeconds > 0 {
					// Fall back to global config
					pollingInterval = time.Duration(ew.appConfig.EventPollingIntervalSeconds) * time.Second
				}
			} else if ew.appConfig.EventPollingIntervalSeconds > 0 {
				// No chain-specific config, use global
				pollingInterval = time.Duration(ew.appConfig.EventPollingIntervalSeconds) * time.Second
			}
		}

		// Create ticker for polling
		ticker := time.NewTicker(pollingInterval)
		defer ticker.Stop()

		currentSlot := fromSlot

		ew.logger.Info().
			Uint64("from_slot", fromSlot).
			Dur("polling_interval", pollingInterval).
			Msg("starting event watcher")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get latest slot
				var latestSlot uint64
				err := ew.parentClient.executeWithFailover(ctx, "get_latest_slot", func(client *rpc.Client) error {
					var innerErr error
					latestSlot, innerErr = client.GetSlot(ctx, rpc.CommitmentFinalized)
					return innerErr
				})
				if err != nil {
					ew.logger.Error().Err(err).Msg("failed to get latest slot")
					continue
				}

				if currentSlot >= latestSlot {
					continue
				}

				// Get signatures for the gateway program
				var signatures []*rpc.TransactionSignature
				err = ew.parentClient.executeWithFailover(ctx, "get_signatures_for_address", func(client *rpc.Client) error {
					var innerErr error
					signatures, innerErr = client.GetSignaturesForAddress(
						ctx,
						ew.gatewayAddr,
					)
					return innerErr
				})
				if err != nil {
					ew.logger.Error().Err(err).Msg("failed to get signatures")
					continue
				}

				// Process signatures
				for _, sig := range signatures {
					if sig.Slot < currentSlot {
						continue
					}

					// Get transaction details
					var tx *rpc.GetTransactionResult
					err = ew.parentClient.executeWithFailover(ctx, "get_transaction", func(client *rpc.Client) error {
						var innerErr error
						maxVersion := uint64(0)
						tx, innerErr = client.GetTransaction(
							ctx,
							sig.Signature,
							&rpc.GetTransactionOpts{
								Encoding:                       solana.EncodingBase64,
								MaxSupportedTransactionVersion: &maxVersion,
							},
						)
						return innerErr
					})
					if err != nil {
						ew.logger.Error().
							Err(err).
							Str("signature", sig.Signature.String()).
							Msg("failed to get transaction")
						continue
					}

					// Parse gateway event from transaction using event parser
					event := ew.eventParser.ParseGatewayEvent(tx, sig.Signature.String(), sig.Slot)
					if event != nil {
						// Track transaction for confirmations
						if err := ew.tracker.TrackTransaction(
							event.TxHash,
							event.BlockNumber,
							event.EventID,
							event.ConfirmationType,
							event.Payload,
						); err != nil {
							ew.logger.Error().Err(err).
								Str("tx_hash", event.TxHash).
								Msg("failed to track transaction")
						}

						select {
						case eventChan <- event:
						case <-ctx.Done():
							return
						}
					}
				}

				// First verify all pending transactions for reorgs (Solana-specific)
				if verifyTransactions != nil {
					if err := verifyTransactions(ctx); err != nil {
						ew.logger.Error().Err(err).Msg("failed to verify pending transactions for reorgs")
					}
				}

				// Then update confirmations for remaining valid transactions
				ew.logger.Debug().
					Uint64("latest_slot", latestSlot).
					Msg("updating confirmations for Solana transactions")
				if err := ew.tracker.UpdateConfirmations(latestSlot); err != nil {
					ew.logger.Error().Err(err).Msg("failed to update confirmations")
				}

				// Update last processed slot
				if updateLastSlot != nil {
					if err := updateLastSlot(latestSlot); err != nil {
						ew.logger.Error().Err(err).Msg("failed to update last processed slot")
					}
				}

				currentSlot = latestSlot
			}
		}
	}()

	return eventChan, nil
}
