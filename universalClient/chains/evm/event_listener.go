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

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// EventListener listens for gateway events on EVM chains and stores them in the database
type EventListener struct {
	// Core dependencies
	rpcClient  *RPCClient
	chainStore *common.ChainStore
	database   *db.DB

	// Configuration
	gatewayAddress      string
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

	// Build event topics for filtering
	eventTopics := make([]ethcommon.Hash, 0, 3)
	topicToEventType := make(map[ethcommon.Hash]string)
	for _, method := range gatewayMethods {
		if method.EventIdentifier == "" {
			continue
		}
		switch method.Name {
		case EventTypeSendFunds,
			EventTypeExecuteUniversalTx,
			EventTypeRevertUniversalTx:
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

	el.logger.Info().Msg("EVM event listener started")
	return nil
}

// Stop gracefully stops the event listener
func (el *EventListener) Stop() error {
	if !el.running {
		return nil
	}

	el.logger.Info().Msg("stopping EVM event listener")
	close(el.stopCh)
	el.running = false

	el.wg.Wait()
	el.logger.Info().Msg("EVM event listener stopped")
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
		el.logger.Warn().Msg("no event topics configured, event listener will not process events")
		return
	}

	el.logger.Info().
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
			el.logger.Info().Msg("context cancelled, stopping event listener")
			return
		case <-el.stopCh:
			el.logger.Info().Msg("stop signal received, stopping event listener")
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
	if err := el.processBlockRange(ctx, *currentBlock, latestBlock, topics); err != nil {
		return fmt.Errorf("failed to process block range: %w", err)
	}

	// Update last processed block in database
	if err := el.updateLastProcessedBlock(latestBlock); err != nil {
		el.logger.Error().Err(err).Msg("failed to update last processed block")
		// Don't return error - continue processing
	}

	// Move to next block
	*currentBlock = latestBlock + 1
	return nil
}

// processBlockRange processes events in a range of blocks
func (el *EventListener) processBlockRange(
	ctx context.Context,
	fromBlock, toBlock uint64,
	topics []ethcommon.Hash,
) error {
	const maxBlockRange uint64 = 9000 // Safe under the 10000 RPC limit

	currentFrom := fromBlock

	// Process in chunks if the range is too large
	for currentFrom <= toBlock {
		currentTo := currentFrom + maxBlockRange - 1
		if currentTo > toBlock {
			currentTo = toBlock
		}

		// Log chunk processing for large ranges
		blockRange := currentTo - currentFrom + 1
		if blockRange > 1000 {
			el.logger.Debug().
				Uint64("from_block", currentFrom).
				Uint64("to_block", currentTo).
				Uint64("range_size", blockRange).
				Msg("processing block chunk")
		}

		// Process chunk
		if err := el.processBlockChunk(ctx, currentFrom, currentTo, topics); err != nil {
			return fmt.Errorf("failed to process chunk %d-%d: %w", currentFrom, currentTo, err)
		}

		// Move to next chunk
		currentFrom = currentTo + 1
	}

	return nil
}

// processBlockChunk processes a single chunk of blocks
func (el *EventListener) processBlockChunk(
	ctx context.Context,
	fromBlock, toBlock uint64,
	topics []ethcommon.Hash,
) error {
	// Parse gateway address
	gatewayAddr := ethcommon.HexToAddress(el.gatewayAddress)

	// Create filter query
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Addresses: []ethcommon.Address{gatewayAddr},
		Topics:    [][]ethcommon.Hash{topics},
	}

	// Get logs for this chunk
	logs, err := el.rpcClient.FilterLogs(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}

	// Log when events are found
	if len(logs) > 0 {
		el.logger.Info().
			Uint64("from_block", fromBlock).
			Uint64("to_block", toBlock).
			Int("logs_found", len(logs)).
			Str("gateway_address", el.gatewayAddress).
			Msg("found gateway events")
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
