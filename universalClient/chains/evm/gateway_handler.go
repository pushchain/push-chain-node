package evm

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/store"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
	"gorm.io/gorm"
)

// GatewayHandler handles gateway operations for EVM chains
type GatewayHandler struct {
	parentClient  *Client // Reference to parent client for RPC pool access
	config        *uregistrytypes.ChainConfig
	appConfig     *config.Config
	logger        zerolog.Logger
	tracker       *common.ConfirmationTracker
	gatewayAddr   ethcommon.Address
	contractABI   interface{} // Will hold minimal ABI when available
	eventTopics   map[string]ethcommon.Hash
	database      *db.DB
}

// NewGatewayHandler creates a new EVM gateway handler
func NewGatewayHandler(
	parentClient *Client,
	config *uregistrytypes.ChainConfig,
	database *db.DB,
	appConfig *config.Config,
	logger zerolog.Logger,
) (*GatewayHandler, error) {
	if config.GatewayAddress == "" {
		return nil, fmt.Errorf("gateway address not configured")
	}

	// Parse gateway address
	gatewayAddr := ethcommon.HexToAddress(config.GatewayAddress)

	// Create confirmation tracker
	tracker := common.NewConfirmationTracker(
		database,
		config.BlockConfirmation,
		logger,
	)

	// Build event topics from config methods
	eventTopics := make(map[string]ethcommon.Hash)
	logger.Info().
		Int("gateway_methods_count", len(config.GatewayMethods)).
		Str("gateway_address", config.GatewayAddress).
		Msg("building event topics")
	for _, method := range config.GatewayMethods {
		if method.EventIdentifier != "" {
			eventTopics[method.Identifier] = ethcommon.HexToHash(method.EventIdentifier)
			logger.Info().
				Str("method", method.Name).
				Str("event_identifier", method.EventIdentifier).
				Str("method_id", method.Identifier).
				Msg("registered event topic from config")
		} else {
			logger.Warn().
				Str("method", method.Name).
				Str("method_id", method.Identifier).
				Msg("no event identifier provided in config for method")
		}
	}

	return &GatewayHandler{
		parentClient: parentClient,
		config:       config,
		appConfig:    appConfig,
		logger:       logger.With().Str("component", "evm_gateway_handler").Logger(),
		tracker:      tracker,
		gatewayAddr:  gatewayAddr,
		eventTopics:  eventTopics,
		database:     database,
	}, nil
}

// GetLatestBlock returns the latest block number
func (h *GatewayHandler) GetLatestBlock(ctx context.Context) (uint64, error) {
	var blockNum uint64
	err := h.parentClient.executeWithFailover(ctx, "get_latest_block", func(client *ethclient.Client) error {
		var innerErr error
		blockNum, innerErr = client.BlockNumber(ctx)
		return innerErr
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get block number: %w", err)
	}
	return blockNum, nil
}

// GetStartBlock returns the block to start watching from
func (h *GatewayHandler) GetStartBlock(ctx context.Context) (uint64, error) {
	// Check database for last processed block
	var chainState store.ChainState
	result := h.database.Client().First(&chainState)
	
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// No record found, get latest block
			h.logger.Info().Msg("no last processed block found, starting from latest")
			return h.GetLatestBlock(ctx)
		}
		return 0, fmt.Errorf("failed to get last processed block: %w", result.Error)
	}
	
	// Found a record, use it
	if chainState.LastBlock < 0 {
		return h.GetLatestBlock(ctx)
	}
	
	h.logger.Info().
		Int64("block", chainState.LastBlock).
		Msg("resuming from last processed block")
	
	return uint64(chainState.LastBlock), nil
}

// UpdateLastProcessedBlock updates the last processed block in the database
func (h *GatewayHandler) UpdateLastProcessedBlock(blockNumber uint64) error {
	var chainState store.ChainState
	
	// Try to find existing record
	result := h.database.Client().First(&chainState)
	
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		return fmt.Errorf("failed to query last processed block: %w", result.Error)
	}
	
	if result.Error == gorm.ErrRecordNotFound {
		// Create new record
		chainState = store.ChainState{
			LastBlock: int64(blockNumber),
		}
		if err := h.database.Client().Create(&chainState).Error; err != nil {
			return fmt.Errorf("failed to create last processed block record: %w", err)
		}
	} else {
		// Update existing record only if new block is higher
		if int64(blockNumber) > chainState.LastBlock {
			chainState.LastBlock = int64(blockNumber)
			if err := h.database.Client().Save(&chainState).Error; err != nil {
				return fmt.Errorf("failed to update last processed block: %w", err)
			}
		}
	}
	
	return nil
}

// WatchGatewayEvents starts watching for gateway events from a specific block
func (h *GatewayHandler) WatchGatewayEvents(ctx context.Context, fromBlock uint64) (<-chan *common.GatewayEvent, error) {
	eventChan := make(chan *common.GatewayEvent)

	// Create topics filter
	var topics []ethcommon.Hash
	for methodID, topic := range h.eventTopics {
		topics = append(topics, topic)
		h.logger.Debug().
			Str("method_id", methodID).
			Str("topic", topic.Hex()).
			Msg("adding topic to filter")
	}

	if len(topics) == 0 {
		close(eventChan)
		return eventChan, fmt.Errorf("no event topics configured")
	}
	
	h.logger.Info().
		Int("topic_count", len(topics)).
		Interface("topics", topics).
		Msg("configured event topics for watching")

	go func() {
		defer close(eventChan)

		// Use configured polling interval or default to 5 seconds
		pollingInterval := 5 * time.Second
		if h.appConfig != nil && h.appConfig.EventPollingInterval > 0 {
			pollingInterval = h.appConfig.EventPollingInterval
		}
		
		// Create subscription for new blocks
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
				err := h.parentClient.executeWithFailover(ctx, "get_latest_block", func(client *ethclient.Client) error {
					var innerErr error
					latestBlock, innerErr = client.BlockNumber(ctx)
					return innerErr
				})
				if err != nil {
					h.logger.Error().Err(err).Msg("failed to get latest block")
					continue
				}

				if currentBlock >= latestBlock {
					continue
				}

				// Create filter query
				// Topics should be in the first position (topic[0])
				query := ethereum.FilterQuery{
					FromBlock: big.NewInt(int64(currentBlock)),
					ToBlock:   big.NewInt(int64(latestBlock)),
					Addresses: []ethcommon.Address{h.gatewayAddr},
					Topics:    [][]ethcommon.Hash{topics}, // This filters for any of the topics in position 0
				}

				// Get logs
				var logs []types.Log
				err = h.parentClient.executeWithFailover(ctx, "filter_logs", func(client *ethclient.Client) error {
					var innerErr error
					logs, innerErr = client.FilterLogs(ctx, query)
					return innerErr
				})
				if err != nil {
					h.logger.Error().Err(err).Msg("failed to filter logs")
					continue
				}

				// Log when events are found
				if len(logs) > 0 {
					h.logger.Info().
						Uint64("from_block", currentBlock).
						Uint64("to_block", latestBlock).
						Int("logs_found", len(logs)).
						Str("gateway_address", h.gatewayAddr.Hex()).
						Msg("found gateway events")
				}

				// Process logs
				for _, log := range logs {
					event := h.parseGatewayEvent(&log)
					if event != nil {
						// Track transaction for confirmations
						if err := h.tracker.TrackTransaction(
							event.TxHash,
							event.BlockNumber,
							event.Method,
							event.EventID,
							nil,
						); err != nil {
							h.logger.Error().Err(err).
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

				// First verify all pending transactions for reorgs (EVM-specific)
				if err := h.verifyPendingTransactions(ctx); err != nil {
					h.logger.Error().Err(err).Msg("failed to verify pending transactions for reorgs")
				}

				// Then update confirmations for remaining valid transactions
				if err := h.tracker.UpdateConfirmations(latestBlock); err != nil {
					h.logger.Error().Err(err).Msg("failed to update confirmations")
				}

				// Update last processed block in database
				if err := h.UpdateLastProcessedBlock(latestBlock); err != nil {
					h.logger.Error().Err(err).Msg("failed to update last processed block")
				}

				currentBlock = latestBlock + 1
			}
		}
	}()

	return eventChan, nil
}

// parseGatewayEvent parses a log into a GatewayEvent
func (h *GatewayHandler) parseGatewayEvent(log *types.Log) *common.GatewayEvent {
	if len(log.Topics) == 0 {
		return nil
	}

	// Find matching method by event topic
	var methodID, methodName string
	for id, topic := range h.eventTopics {
		if log.Topics[0] == topic {
			methodID = id
			// Find method name from config
			for _, method := range h.config.GatewayMethods {
				if method.Identifier == id {
					methodName = method.Name
					break
				}
			}
			break
		}
	}

	if methodID == "" {
		return nil
	}

	event := &common.GatewayEvent{
		ChainID:     h.config.Chain,
		TxHash:      log.TxHash.Hex(),
		BlockNumber: log.BlockNumber,
		Method:      methodName,
		EventID:     methodID,
		Payload:     log.Data,
	}

	// Parse event data based on method
	if methodName == "addFunds" && len(log.Topics) >= 3 {
		// FundsAdded event typically has:
		// topics[0] = event signature
		// topics[1] = indexed sender address
		// topics[2] = indexed token address
		// data contains amount and payload
		
		event.Sender = ethcommon.BytesToAddress(log.Topics[1].Bytes()).Hex()
		
		// Parse amount from data if available
		if len(log.Data) >= 32 {
			amount := new(big.Int).SetBytes(log.Data[:32])
			event.Amount = amount.String()
		}
	}

	return event
}

// GetTransactionConfirmations returns the number of confirmations for a transaction
func (h *GatewayHandler) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	// Get transaction receipt
	hash := ethcommon.HexToHash(txHash)
	var receipt *types.Receipt
	err := h.parentClient.executeWithFailover(ctx, "get_transaction_receipt", func(client *ethclient.Client) error {
		var innerErr error
		receipt, innerErr = client.TransactionReceipt(ctx, hash)
		return innerErr
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	// Get current block
	var currentBlock uint64
	err = h.parentClient.executeWithFailover(ctx, "get_block_number", func(client *ethclient.Client) error {
		var innerErr error
		currentBlock, innerErr = client.BlockNumber(ctx)
		return innerErr
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get current block: %w", err)
	}

	if currentBlock < receipt.BlockNumber.Uint64() {
		return 0, nil
	}

	return currentBlock - receipt.BlockNumber.Uint64(), nil
}

// IsConfirmed checks if a transaction has enough confirmations
func (h *GatewayHandler) IsConfirmed(ctx context.Context, txHash string, mode string) (bool, error) {
	// Check in tracker first
	confirmed, err := h.tracker.IsConfirmed(txHash, mode)
	if err == nil {
		return confirmed, nil
	}

	// Fallback to chain query
	confirmations, err := h.GetTransactionConfirmations(ctx, txHash)
	if err != nil {
		return false, err
	}

	required := h.tracker.GetRequiredConfirmations(mode)
	return confirmations >= required, nil
}

// GetConfirmationTracker returns the confirmation tracker
func (h *GatewayHandler) GetConfirmationTracker() *common.ConfirmationTracker {
	return h.tracker
}

// verifyTransactionExistence checks if an EVM transaction still exists on chain (reorg detection)
func (h *GatewayHandler) verifyTransactionExistence(
	ctx context.Context,
	tx *store.ChainTransaction,
) (bool, error) {
	hash := ethcommon.HexToHash(tx.TxHash)
	var receipt *types.Receipt
	err := h.parentClient.executeWithFailover(ctx, "get_transaction_receipt", func(client *ethclient.Client) error {
		var innerErr error
		receipt, innerErr = client.TransactionReceipt(ctx, hash)
		return innerErr
	})
	if err != nil {
		// Transaction not found - likely reorganized out
		h.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_block", tx.BlockNumber).
			Err(err).
			Msg("EVM transaction not found on chain - marking as reorged")
		
		tx.Status = "reorged"
		tx.Confirmations = 0
		return false, nil
	}

	// Check if transaction moved to a different block due to reorg
	if receipt.BlockNumber.Uint64() != tx.BlockNumber {
		h.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_block", tx.BlockNumber).
			Uint64("new_block", receipt.BlockNumber.Uint64()).
			Msg("EVM transaction moved to different block due to reorg - updating block number")
		
		// Update block number and reset status
		tx.BlockNumber = receipt.BlockNumber.Uint64()
		tx.Status = "pending"
		tx.Confirmations = 0
		return false, nil
	}

	return true, nil
}

// verifyPendingTransactions checks all pending/fast_confirmed transactions for reorgs
func (h *GatewayHandler) verifyPendingTransactions(ctx context.Context) error {
	var pendingTxs []store.ChainTransaction
	
	// Get all transactions that need verification
	err := h.database.Client().
		Where("status IN (?)", []string{"pending", "fast_confirmed"}).
		Find(&pendingTxs).Error
	if err != nil {
		return fmt.Errorf("failed to fetch pending transactions for verification: %w", err)
	}

	h.logger.Debug().
		Str("chain_id", h.config.Chain).
		Int("pending_count", len(pendingTxs)).
		Msg("verifying EVM transactions for reorgs")
	
	// Verify each transaction
	for _, tx := range pendingTxs {
		exists, err := h.verifyTransactionExistence(ctx, &tx)
		if err != nil {
			h.logger.Error().
				Err(err).
				Str("tx_hash", tx.TxHash).
				Msg("failed to verify EVM transaction existence")
			continue
		}
		
		// If transaction was reorged or moved, save the updated status
		if !exists {
			if err := h.database.Client().Save(&tx).Error; err != nil {
				h.logger.Error().
					Err(err).
					Str("tx_hash", tx.TxHash).
					Msg("failed to update reorged EVM transaction")
			}
		}
	}
	
	return nil
}