package evm

import (
	"context"
	"fmt"

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
	database      *db.DB
	
	// Extracted components
	eventParser   *EventParser
	eventWatcher  *EventWatcher
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

	// Create extracted components
	eventParser := NewEventParser(gatewayAddr, config, logger)
	eventWatcher := NewEventWatcher(parentClient, gatewayAddr, eventParser, tracker, appConfig, logger)

	return &GatewayHandler{
		parentClient: parentClient,
		config:       config,
		appConfig:    appConfig,
		logger:       logger.With().Str("component", "evm_gateway_handler").Logger(),
		tracker:      tracker,
		gatewayAddr:  gatewayAddr,
		database:     database,
		eventParser:  eventParser,
		eventWatcher: eventWatcher,
	}, nil
}

// SetVoteHandler sets the vote handler on the confirmation tracker
func (h *GatewayHandler) SetVoteHandler(handler common.VoteHandler) {
	if h.tracker != nil {
		h.tracker.SetVoteHandler(handler)
		h.logger.Info().Msg("vote handler set on confirmation tracker")
	}
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
	
	// Found a record, check if it has a valid block number
	if chainState.LastBlock <= 0 {
		// If LastBlock is 0 or negative, start from latest block
		h.logger.Info().
			Int64("stored_block", chainState.LastBlock).
			Msg("invalid or zero last block, starting from latest")
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

// WatchGatewayEvents delegates to event watcher
func (h *GatewayHandler) WatchGatewayEvents(ctx context.Context, fromBlock uint64) (<-chan *common.GatewayEvent, error) {
	return h.eventWatcher.WatchEvents(
		ctx,
		fromBlock,
		h.UpdateLastProcessedBlock,
		h.verifyPendingTransactions,
	)
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
func (h *GatewayHandler) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	// Check in tracker
	return h.tracker.IsConfirmed(txHash)
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