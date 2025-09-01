package common

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/store"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
	"github.com/rs/zerolog"
)

// VoteHandler interface for voting on confirmed transactions
type VoteHandler interface {
	VoteAndConfirm(ctx context.Context, tx *store.ChainTransaction) error
}

// ConfirmationTracker tracks transaction confirmations for gateway events
type ConfirmationTracker struct {
	db              *db.DB
	fastInbound     uint64
	standardInbound uint64
	logger          zerolog.Logger
	voteHandler     VoteHandler // Optional vote handler for confirmed transactions
}

// NewConfirmationTracker creates a new confirmation tracker
func NewConfirmationTracker(
	database *db.DB,
	config *uregistrytypes.BlockConfirmation,
	logger zerolog.Logger,
) *ConfirmationTracker {
	// Default values if config is nil
	fastConf := uint64(5)
	standardConf := uint64(12)
	
	if config != nil {
		if config.FastInbound > 0 {
			fastConf = uint64(config.FastInbound)
		}
		if config.StandardInbound > 0 {
			standardConf = uint64(config.StandardInbound)
		}
	}
	
	return &ConfirmationTracker{
		db:              database,
		fastInbound:     fastConf,
		standardInbound: standardConf,
		logger:          logger.With().Str("component", "confirmation_tracker").Logger(),
		voteHandler:     nil, // Initialize without vote handler
	}
}

// SetVoteHandler sets the vote handler for the confirmation tracker
func (ct *ConfirmationTracker) SetVoteHandler(handler VoteHandler) {
	ct.voteHandler = handler
}

// TrackTransaction starts tracking a new transaction for confirmations
func (ct *ConfirmationTracker) TrackTransaction(
	txHash string,
	blockNumber uint64,
	method, eventID string,
	confirmationType string,
	data []byte,
) (err error) {
	// Start database transaction to avoid race conditions and improve performance
	dbTx := ct.db.Client().Begin()
	defer func() {
		if r := recover(); r != nil {
			dbTx.Rollback()
			ct.logger.Error().
				Str("tx_hash", txHash).
				Interface("panic", r).
				Msg("panic recovered in TrackTransaction")
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()
	
	// Check if transaction already exists using FOR UPDATE to prevent race conditions
	var existing store.ChainTransaction
	err = dbTx.Set("gorm:query_option", "FOR UPDATE").
		Where("tx_hash = ?", txHash).
		First(&existing).Error
	
	if err == nil {
		// Transaction already exists, update it within the transaction
		existing.Confirmations = 0
		existing.Status = "pending"
		existing.BlockNumber = blockNumber // Update block number in case of reorg
		existing.Method = method
		existing.EventIdentifier = eventID
		existing.ConfirmationType = confirmationType
		existing.Data = data
		
		if err := dbTx.Save(&existing).Error; err != nil {
			dbTx.Rollback()
			ct.logger.Error().
				Err(err).
				Str("tx_hash", txHash).
				Msg("failed to update existing transaction")
			return fmt.Errorf("failed to update transaction: %w", err)
		}
		
		if err := dbTx.Commit().Error; err != nil {
			return fmt.Errorf("failed to commit transaction update: %w", err)
		}
		
		ct.logger.Debug().
			Str("tx_hash", txHash).
			Uint64("block", blockNumber).
			Msg("updated existing transaction")
		
		return nil
	}
	
	// Create new transaction record
	tx := &store.ChainTransaction{
		TxHash:           txHash,
		BlockNumber:      blockNumber,
		Method:           method,
		EventIdentifier:  eventID,
		Status:           "pending",
		Confirmations:    0,
		ConfirmationType: confirmationType,
		Data:             data,
	}
	
	if err := dbTx.Create(tx).Error; err != nil {
		dbTx.Rollback()
		ct.logger.Error().
			Err(err).
			Str("tx_hash", txHash).
			Msg("failed to create transaction")
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	
	if err := dbTx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit new transaction: %w", err)
	}
	
	ct.logger.Debug().
		Str("tx_hash", txHash).
		Uint64("block", blockNumber).
		Msg("tracking new transaction")
	
	return nil
}

// UpdateConfirmations updates confirmation counts for all pending transactions
func (ct *ConfirmationTracker) UpdateConfirmations(
	currentBlock uint64,
) (err error) {
	var pendingTxs []store.ChainTransaction
	
	// Get all transactions that are not yet fully confirmed
	err = ct.db.Client().
		Where("status = ?", "pending").
		Find(&pendingTxs).Error
	if err != nil {
		return fmt.Errorf("failed to fetch pending transactions: %w", err)
	}
	
	if len(pendingTxs) == 0 {
		return nil // No transactions to update
	}
	
	ct.logger.Debug().
		Uint64("current_block", currentBlock).
		Int("pending_count", len(pendingTxs)).
		Msg("updating confirmations")
	
	// Start database transaction for batch updates
	dbTx := ct.db.Client().Begin()
	defer func() {
		if r := recover(); r != nil {
			dbTx.Rollback()
			ct.logger.Error().
				Uint64("current_block", currentBlock).
				Interface("panic", r).
				Msg("panic recovered in UpdateConfirmations")
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()
	
	updatedCount := 0
	confirmedCount := 0
	
	// Update each transaction within the database transaction
	for i := range pendingTxs {
		tx := &pendingTxs[i]
		if currentBlock < tx.BlockNumber {
			// Current block is before transaction block (shouldn't happen)
			continue
		}
		
		confirmations := currentBlock - tx.BlockNumber
		tx.Confirmations = confirmations
		
		// Determine required confirmations based on transaction's confirmation type
		var requiredConfirmations uint64
		if tx.ConfirmationType == "FAST" {
			requiredConfirmations = ct.fastInbound
		} else {
			// Default to STANDARD for any unspecified or STANDARD type
			requiredConfirmations = ct.standardInbound
		}
		
		// Check if transaction meets its required confirmation threshold
		if confirmations >= requiredConfirmations && tx.Status != "confirmed" {
			// If we have a vote handler, use it to vote before confirming
			if ct.voteHandler != nil {
				ctx := context.Background()
				if err := ct.voteHandler.VoteAndConfirm(ctx, tx); err != nil {
					ct.logger.Error().
						Str("tx_hash", tx.TxHash).
						Err(err).
						Msg("failed to vote on transaction, will retry")
					// Transaction stays pending for retry
					updatedCount++
					continue
				}
				// VoteAndConfirm updates the status to confirmed
				confirmedCount++
				ct.logger.Info().
					Str("tx_hash", tx.TxHash).
					Str("confirmation_type", tx.ConfirmationType).
					Uint64("confirmations", confirmations).
					Uint64("required_confirmations", requiredConfirmations).
					Msg("transaction voted and confirmed")
			} else {
				// No vote handler, just mark as confirmed
				tx.Status = "confirmed"
				confirmedCount++
				ct.logger.Warn().
					Str("tx_hash", tx.TxHash).
					Str("confirmation_type", tx.ConfirmationType).
					Uint64("confirmations", confirmations).
					Uint64("required_confirmations", requiredConfirmations).
					Msg("No vote handler configured - marking as confirmed without voting (hot keys not configured)")
			}
		}
		
		// Save within the transaction
		if err := dbTx.Save(tx).Error; err != nil {
			ct.logger.Error().
				Err(err).
				Str("tx_hash", tx.TxHash).
				Msg("failed to update transaction confirmations")
			dbTx.Rollback()
			return fmt.Errorf("failed to update transaction %s: %w", tx.TxHash, err)
		}
		updatedCount++
	}
	
	// Commit all updates at once
	if err := dbTx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit confirmation updates: %w", err)
	}
	
	ct.logger.Debug().
		Int("updated_count", updatedCount).
		Int("confirmed_count", confirmedCount).
		Msg("confirmation updates committed")
	
	return nil
}

// IsConfirmed checks if a transaction has enough confirmations
func (ct *ConfirmationTracker) IsConfirmed(
	txHash string,
) (bool, error) {
	var tx store.ChainTransaction
	err := ct.db.Client().Where("tx_hash = ?", txHash).First(&tx).Error
	if err != nil {
		return false, fmt.Errorf("transaction not found: %w", err)
	}
	
	// Transaction is confirmed when status is "confirmed"
	confirmed := tx.Status == "confirmed"
	
	// Log when checking reorged transactions for visibility
	if tx.Status == "reorged" {
		ct.logger.Debug().
			Str("tx_hash", txHash).
			Msg("transaction was reorganized out - returning false")
	}
	
	ct.logger.Debug().
		Str("tx_hash", txHash).
		Str("status", tx.Status).
		Str("confirmation_type", tx.ConfirmationType).
		Uint64("confirmations", tx.Confirmations).
		Bool("confirmed", confirmed).
		Msg("checking confirmation status")
	
	return confirmed, nil
}

// GetRequiredConfirmations returns the required confirmations for a mode
func (ct *ConfirmationTracker) GetRequiredConfirmations(mode string) uint64 {
	if mode == "fast" {
		return ct.fastInbound
	}
	return ct.standardInbound
}

// GetGatewayTransaction retrieves a gateway transaction by hash
func (ct *ConfirmationTracker) GetGatewayTransaction(txHash string) (*store.ChainTransaction, error) {
	var tx store.ChainTransaction
	err := ct.db.Client().Where("tx_hash = ?", txHash).First(&tx).Error
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}
	return &tx, nil
}

// GetConfirmedTransactions returns all confirmed transactions for a chain
func (ct *ConfirmationTracker) GetConfirmedTransactions(chainID string) ([]store.ChainTransaction, error) {
	var txs []store.ChainTransaction
	err := ct.db.Client().
		Where("status = ?", "confirmed").
		Find(&txs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch confirmed transactions: %w", err)
	}
	return txs, nil
}

// MarkTransactionFailed marks a transaction as failed
func (ct *ConfirmationTracker) MarkTransactionFailed(txHash string) error {
	return ct.db.Client().
		Model(&store.ChainTransaction{}).
		Where("tx_hash = ?", txHash).
		Update("status", "failed").Error
}