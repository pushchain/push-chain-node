package evm

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// EventListener listens for gateway and vault events on EVM chains and stores them in the database
type EventListener struct {
	// Core dependencies
	rpcClient  *RPCClient
	chainStore *common.ChainStore
	database   *db.DB

	// Configuration
	gatewayAddress      string
	vaultAddress        string
	chainID             string
	eventTopics         []ethcommon.Hash
	topicToEventType    map[ethcommon.Hash]string
	eventPollingSeconds int
	eventStartFrom      *int64

	// State
	logger  zerolog.Logger
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewEventListener creates a new EVM event listener
func NewEventListener(
	rpcClient *RPCClient,
	gatewayAddress string,
	vaultAddress string,
	chainID string,
	gatewayMethods []*uregistrytypes.GatewayMethods,
	vaultMethods []*uregistrytypes.VaultMethods,
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

	// Build event topics for filtering
	eventTopics := make([]ethcommon.Hash, 0, 4)
	topicToEventType := make(map[ethcommon.Hash]string)

	// Gateway event topics
	for _, method := range gatewayMethods {
		if method.EventIdentifier == "" {
			continue
		}
		switch method.Name {
		case EventTypeSendFunds,
			EventTypeRevertUniversalTx:
			topic := ethcommon.HexToHash(method.EventIdentifier)
			eventTopics = append(eventTopics, topic)
			topicToEventType[topic] = method.Name
		}
	}

	// Vault event topics
	for _, method := range vaultMethods {
		if method.EventIdentifier == "" {
			continue
		}
		switch method.Name {
		case EventTypeFinalizeUniversalTx, EventTypeFundsRescued:
			topic := ethcommon.HexToHash(method.EventIdentifier)
			eventTopics = append(eventTopics, topic)
			topicToEventType[topic] = method.Name
		}
	}

	return &EventListener{
		rpcClient:           rpcClient,
		chainStore:          common.NewChainStore(database),
		database:            database,
		gatewayAddress:      gatewayAddress,
		vaultAddress:        vaultAddress,
		chainID:             chainID,
		eventTopics:         eventTopics,
		topicToEventType:    topicToEventType,
		eventPollingSeconds: eventPollingSeconds,
		eventStartFrom:      eventStartFrom,
		logger:              logger.With().Str("component", "evm_event_listener").Str("chain", chainID).Logger(),
		stopCh:              make(chan struct{}),
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

	el.logger.Debug().Msg("stopping EVM event listener")
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

	// Get starting block
	fromBlock, err := el.getStartBlock(ctx)
	if err != nil {
		el.logger.Error().Err(err).Msg("failed to get start block")
		return
	}

	// Get event topics
	topics := el.eventTopics
	if len(topics) == 0 {
		el.logger.Error().Msg("no event topics configured, event listener will not process events")
		return
	}

	el.logger.Debug().
		Int("topic_count", len(topics)).
		Uint64("from_block", fromBlock).
		Dur("poll_interval", pollInterval).
		Msg("starting event watching")

	currentBlock := fromBlock
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
			if err := el.processNewBlocks(ctx, &currentBlock, topics); err != nil {
				el.logger.Error().Err(err).Msg("failed to process new blocks")
				// Continue processing on error
			}
		}
	}
}

// processNewBlocks processes new blocks since last processed block
func (el *EventListener) processNewBlocks(
	ctx context.Context,
	currentBlock *uint64,
	topics []ethcommon.Hash,
) error {
	// Get latest block
	latestBlock, err := el.rpcClient.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	// Skip if no new blocks
	if *currentBlock >= latestBlock {
		return nil
	}

	// Process blocks in range
	nextBlock, rangeErr := el.processBlockRange(ctx, *currentBlock, latestBlock, topics)

	// Commit whatever was covered even when a later chunk failed. Holding the
	// cursor back would re-read the blocks already handled on every tick, so one
	// unreadable window would sit in front of everything behind it indefinitely.
	if nextBlock > *currentBlock {
		if err := el.updateLastProcessedBlock(nextBlock - 1); err != nil {
			el.logger.Error().Err(err).Msg("failed to update last processed block")
			// Don't return error - continue processing
		}
		*currentBlock = nextBlock
	}

	if rangeErr != nil {
		return fmt.Errorf("failed to process block range: %w", rangeErr)
	}
	return nil
}

// Block span for a single eth_getLogs call. Providers cap the result set rather
// than the block count, so a dense window can be rejected at a span that is
// normally fine. maxBlockRange is the optimistic starting point and minBlockRange
// the floor we stop shrinking at.
const (
	maxBlockRange uint64 = 9000 // Safe under the 10000 RPC limit
	minBlockRange uint64 = 100
)

// processBlockRange processes events in a range of blocks, returning the first
// block it did not cover. That is fromBlock when nothing was processed and
// toBlock+1 when everything was, so the caller can commit partial progress
// whether or not an error is also returned.
//
// A rejected query is retried over a smaller span rather than abandoned: the
// limit is on results, so halving until the window fits gets past a dense range
// that a fixed span cannot. Shrinking is linear rather than a recursive split,
// which would issue exponentially many calls against a range that keeps failing.
func (el *EventListener) processBlockRange(
	ctx context.Context,
	fromBlock, toBlock uint64,
	topics []ethcommon.Hash,
) (uint64, error) {
	span := maxBlockRange
	nextFrom := fromBlock

	for nextFrom <= toBlock {
		currentTo := nextFrom + span - 1
		if currentTo > toBlock || currentTo < nextFrom { // second test catches overflow
			currentTo = toBlock
		}

		blockRange := currentTo - nextFrom + 1
		if blockRange > 1000 {
			el.logger.Debug().
				Uint64("from_block", nextFrom).
				Uint64("to_block", currentTo).
				Uint64("range_size", blockRange).
				Msg("processing block chunk")
		}

		if err := el.processBlockChunk(ctx, nextFrom, currentTo, topics); err != nil {
			// Halve what was actually attempted, not the nominal span: near the end
			// of a range the span is clamped to toBlock, so shrinking the span alone
			// would resend the identical query until it dropped below the remainder.
			if blockRange > minBlockRange {
				span = blockRange / 2
				if span < minBlockRange {
					span = minBlockRange
				}
				el.logger.Warn().
					Err(err).
					Uint64("from_block", nextFrom).
					Uint64("to_block", currentTo).
					Uint64("retry_span", span).
					Msg("log query failed, retrying the same start over a smaller span")
				continue
			}

			// At the floor the span is no longer the problem. Report the failure
			// and leave the cursor here: skipping ahead would drop any deposits in
			// these blocks permanently, which is worse than waiting for the RPC.
			return nextFrom, fmt.Errorf("failed to process chunk %d-%d at minimum span: %w", nextFrom, currentTo, err)
		}

		nextFrom = currentTo + 1
	}

	return nextFrom, nil
}

// processBlockChunk processes a single chunk of blocks
func (el *EventListener) processBlockChunk(
	ctx context.Context,
	fromBlock, toBlock uint64,
	topics []ethcommon.Hash,
) error {
	// Build address filter: gateway + vault
	addresses := []ethcommon.Address{ethcommon.HexToAddress(el.gatewayAddress), ethcommon.HexToAddress(el.vaultAddress)}

	// Create filter query
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Addresses: addresses,
		Topics:    [][]ethcommon.Hash{topics},
	}

	// Get logs for this chunk
	logs, err := el.rpcClient.FilterLogs(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}

	// Log when events are found
	if len(logs) > 0 {
		el.logger.Debug().
			Uint64("from_block", fromBlock).
			Uint64("to_block", toBlock).
			Int("logs_found", len(logs)).
			Msg("found contract events")
	}

	// Process each log
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}

		// Determine event type based on topic
		eventType, ok := el.topicToEventType[log.Topics[0]]
		if !ok {
			continue
		}

		event := ParseEvent(&log, eventType, el.chainID, el.logger)
		if event != nil {
			// Insert event if it doesn't already exist
			if stored, err := el.chainStore.InsertEventIfNotExists(event); err != nil {
				el.logger.Error().Err(err).
					Str("event_id", event.EventID).
					Str("type", event.Type).
					Uint64("block", event.BlockHeight).
					Msg("failed to store event")
			} else if stored {
				el.logger.Debug().
					Str("event_id", event.EventID).
					Str("type", event.Type).
					Uint64("block", event.BlockHeight).
					Str("confirmation_type", event.ConfirmationType).
					Msg("stored new event")
			}
		}
	}

	return nil
}

// getStartBlock returns the block to start watching from
func (el *EventListener) getStartBlock(ctx context.Context) (uint64, error) {
	// Get chain height from store
	blockHeight, err := el.chainStore.GetChainHeight()
	if err != nil {
		return 0, fmt.Errorf("failed to get chain height: %w", err)
	}

	// If no previous state or invalid, check config
	if blockHeight == 0 {
		return el.getStartBlockFromConfig(ctx)
	}

	el.logger.Info().
		Uint64("block", blockHeight).
		Msg("resuming from last processed block")

	return blockHeight, nil
}

// getStartBlockFromConfig determines start block from configuration
func (el *EventListener) getStartBlockFromConfig(ctx context.Context) (uint64, error) {
	// Check config for EventStartFrom
	if el.eventStartFrom != nil {
		if *el.eventStartFrom >= 0 {
			startBlock := uint64(*el.eventStartFrom)
			el.logger.Info().
				Uint64("block", startBlock).
				Msg("no previous state found, starting from configured EventStartFrom")
			return startBlock, nil
		}

		// -1 means start from latest block
		if *el.eventStartFrom == -1 {
			latestBlock, err := el.rpcClient.GetLatestBlock(ctx)
			if err != nil {
				el.logger.Warn().Err(err).Msg("failed to get latest block, starting from 0")
				return 0, nil
			}
			el.logger.Info().
				Uint64("block", latestBlock).
				Msg("no previous state found, starting from latest block (EventStartFrom=-1)")
			return latestBlock, nil
		}
	}

	// No config, get latest block
	el.logger.Info().Msg("no last processed block found, starting from latest")
	return el.rpcClient.GetLatestBlock(ctx)
}

// updateLastProcessedBlock updates the last processed block in the database
func (el *EventListener) updateLastProcessedBlock(blockNumber uint64) error {
	return el.chainStore.UpdateChainHeight(blockNumber)
}

// getPollingInterval returns the polling interval from config with default
func (el *EventListener) getPollingInterval() time.Duration {
	if el.eventPollingSeconds > 0 {
		return time.Duration(el.eventPollingSeconds) * time.Second
	}
	return 5 * time.Second // default
}
