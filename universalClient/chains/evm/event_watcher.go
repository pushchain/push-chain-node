package evm

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
)

// EventWatcher handles watching for events on EVM chains
type EventWatcher struct {
	parentClient *Client
	gatewayAddr  ethcommon.Address
	eventParser  *EventParser
	tracker      *common.ConfirmationTracker
	appConfig    *config.Config
	logger       zerolog.Logger
}

// NewEventWatcher creates a new event watcher
func NewEventWatcher(
	parentClient *Client,
	gatewayAddr ethcommon.Address,
	eventParser *EventParser,
	tracker *common.ConfirmationTracker,
	appConfig *config.Config,
	logger zerolog.Logger,
) *EventWatcher {
	return &EventWatcher{
		parentClient: parentClient,
		gatewayAddr:  gatewayAddr,
		eventParser:  eventParser,
		tracker:      tracker,
		appConfig:    appConfig,
		logger:       logger.With().Str("component", "evm_event_watcher").Logger(),
	}
}

// WatchEvents starts watching for events from a specific block
func (ew *EventWatcher) WatchEvents(
	ctx context.Context,
	fromBlock uint64,
	updateLastBlock func(uint64) error,
	verifyTransactions func(context.Context) error,
) (<-chan *common.GatewayEvent, error) {
	// Use buffered channel to prevent blocking producers
	eventChan := make(chan *common.GatewayEvent, 100)

	// Get topics from event parser
	topics := ew.eventParser.GetEventTopics()
	
	if len(topics) == 0 {
		close(eventChan)
		return eventChan, nil
	}
	
	ew.logger.Info().
		Int("topic_count", len(topics)).
		Interface("topics", topics).
		Msg("configured event topics for watching")

	go func() {
		defer close(eventChan)

		// Use configured polling interval or default to 5 seconds
		pollingInterval := 5 * time.Second
		if ew.appConfig != nil && ew.appConfig.EventPollingInterval > 0 {
			pollingInterval = ew.appConfig.EventPollingInterval
		}
		
		// Create ticker for polling
		ticker := time.NewTicker(pollingInterval)
		defer ticker.Stop()

		currentBlock := fromBlock

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get latest block
				var latestBlock uint64
				err := ew.parentClient.executeWithFailover(ctx, "get_latest_block", func(client *ethclient.Client) error {
					var innerErr error
					latestBlock, innerErr = client.BlockNumber(ctx)
					return innerErr
				})
				if err != nil {
					ew.logger.Error().Err(err).Msg("failed to get latest block")
					continue
				}

				if currentBlock >= latestBlock {
					continue
				}

				// Process blocks in batches
				if err := ew.processBlockRange(ctx, currentBlock, latestBlock, topics, eventChan); err != nil {
					ew.logger.Error().
						Err(err).
						Uint64("from_block", currentBlock).
						Uint64("to_block", latestBlock).
						Msg("failed to process block range")
					continue
				}

				// Verify pending transactions for reorgs (EVM-specific)
				if verifyTransactions != nil {
					if err := verifyTransactions(ctx); err != nil {
						ew.logger.Error().Err(err).Msg("failed to verify pending transactions for reorgs")
					}
				}

				// Update confirmations for remaining valid transactions
				if err := ew.tracker.UpdateConfirmations(latestBlock); err != nil {
					ew.logger.Error().Err(err).Msg("failed to update confirmations")
				}

				// Update last processed block in database
				if updateLastBlock != nil {
					if err := updateLastBlock(latestBlock); err != nil {
						ew.logger.Error().Err(err).Msg("failed to update last processed block")
					}
				}

				currentBlock = latestBlock + 1
			}
		}
	}()

	return eventChan, nil
}

// processBlockRange processes events in a range of blocks
func (ew *EventWatcher) processBlockRange(
	ctx context.Context,
	fromBlock, toBlock uint64,
	topics []ethcommon.Hash,
	eventChan chan<- *common.GatewayEvent,
) error {
	// Create filter query
	// Topics should be in the first position (topic[0])
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Addresses: []ethcommon.Address{ew.gatewayAddr},
		Topics:    [][]ethcommon.Hash{topics}, // This filters for any of the topics in position 0
	}

	// Get logs
	var logs []types.Log
	err := ew.parentClient.executeWithFailover(ctx, "filter_logs", func(client *ethclient.Client) error {
		var innerErr error
		logs, innerErr = client.FilterLogs(ctx, query)
		return innerErr
	})
	if err != nil {
		return err
	}

	// Log when events are found
	if len(logs) > 0 {
		ew.logger.Info().
			Uint64("from_block", fromBlock).
			Uint64("to_block", toBlock).
			Int("logs_found", len(logs)).
			Str("gateway_address", ew.gatewayAddr.Hex()).
			Msg("found gateway events")
	}

	// Process logs
	for _, log := range logs {
		event := ew.eventParser.ParseGatewayEvent(&log)
		if event != nil {
			// Track transaction for confirmations
			if err := ew.tracker.TrackTransaction(
				event.TxHash,
				event.BlockNumber,
				event.Method,
				event.EventID,
				nil,
			); err != nil {
				ew.logger.Error().Err(err).
					Str("tx_hash", event.TxHash).
					Msg("failed to track transaction")
			}

			select {
			case eventChan <- event:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}