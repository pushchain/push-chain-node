package common

import (
	"fmt"

	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/store"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
	"github.com/rs/zerolog"
)

// ConfirmationTracker tracks transaction confirmations for gateway events
type ConfirmationTracker struct {
	db              *db.DB
	fastInbound     uint64
	standardInbound uint64
	logger          zerolog.Logger
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
	}
}

// TrackTransaction starts tracking a new transaction for confirmations
func (ct *ConfirmationTracker) TrackTransaction(
	chainID, txHash string,
	blockNumber uint64,
	method, eventID string,
	data []byte,
) error {
	// Start database transaction to avoid race conditions and improve performance
	dbTx := ct.db.Client().Begin()
	defer func() {
		if r := recover(); r != nil {
			dbTx.Rollback()
			panic(r)
		}
	}()
	
	// Check if transaction already exists using FOR UPDATE to prevent race conditions
	var existing store.GatewayTransaction
	err := dbTx.Set("gorm:query_option", "FOR UPDATE").
		Where("tx_hash = ?", txHash).
		First(&existing).Error
	
	if err == nil {
		// Transaction already exists, update it within the transaction
		existing.Confirmations = 0
		existing.Status = "pending"
		existing.BlockNumber = blockNumber // Update block number in case of reorg
		existing.Method = method
		existing.EventIdentifier = eventID
		existing.Data = data
		
		if err := dbTx.Save(&existing).Error; err != nil {
			dbTx.Rollback()
			ct.logger.Error().
				Err(err).
				Str("tx_hash", txHash).
				Str("chain_id", chainID).
				Msg("failed to update existing transaction")
			return fmt.Errorf("failed to update transaction: %w", err)
		}
		
		if err := dbTx.Commit().Error; err != nil {
			return fmt.Errorf("failed to commit transaction update: %w", err)
		}
		
		ct.logger.Debug().
			Str("tx_hash", txHash).
			Str("chain_id", chainID).
			Uint64("block", blockNumber).
			Msg("updated existing transaction")
		
		return nil
	}
	
	// Create new transaction record
	tx := &store.GatewayTransaction{
		ChainID:         chainID,
		TxHash:          txHash,
		BlockNumber:     blockNumber,
		Method:          method,
		EventIdentifier: eventID,
		Status:          "pending",
		Confirmations:   0,
		Data:            data,
	}
	
	if err := dbTx.Create(tx).Error; err != nil {
		dbTx.Rollback()
		ct.logger.Error().
			Err(err).
			Str("tx_hash", txHash).
			Str("chain_id", chainID).
			Msg("failed to create transaction")
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	
	if err := dbTx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit new transaction: %w", err)
	}
	
	ct.logger.Debug().
		Str("tx_hash", txHash).
		Str("chain_id", chainID).
		Uint64("block", blockNumber).
		Msg("tracking new transaction")
	
	return nil
}

// UpdateConfirmations updates confirmation counts for all pending transactions
func (ct *ConfirmationTracker) UpdateConfirmations(
	chainID string,
	currentBlock uint64,
) error {
	var pendingTxs []store.GatewayTransaction
	
	// Get all transactions that are not yet fully confirmed for this chain
	err := ct.db.Client().
		Where("chain_id = ? AND status IN (?)", chainID, []string{"pending", "fast_confirmed"}).
		Find(&pendingTxs).Error
	if err != nil {
		return fmt.Errorf("failed to fetch pending transactions: %w", err)
	}
	
	if len(pendingTxs) == 0 {
		return nil // No transactions to update
	}
	
	ct.logger.Debug().
		Str("chain_id", chainID).
		Uint64("current_block", currentBlock).
		Int("pending_count", len(pendingTxs)).
		Msg("updating confirmations")
	
	// Start database transaction for batch updates
	dbTx := ct.db.Client().Begin()
	defer func() {
		if r := recover(); r != nil {
			dbTx.Rollback()
			panic(r)
		}
	}()
	
	updatedCount := 0
	fastConfirmedCount := 0
	confirmedCount := 0
	
	// Update each transaction within the database transaction
	for _, tx := range pendingTxs {
		if currentBlock < tx.BlockNumber {
			// Current block is before transaction block (shouldn't happen)
			continue
		}
		
		confirmations := currentBlock - tx.BlockNumber
		tx.Confirmations = confirmations
		
		// Check if transaction meets fast threshold and is still pending
		if confirmations >= ct.fastInbound && tx.Status == "pending" {
			tx.Status = "fast_confirmed"
			fastConfirmedCount++
			ct.logger.Info().
				Str("tx_hash", tx.TxHash).
				Uint64("confirmations", confirmations).
				Uint64("fast_threshold", ct.fastInbound).
				Msg("transaction fast confirmed")
		}
		
		// Check if transaction meets standard threshold and is not already confirmed
		if confirmations >= ct.standardInbound && tx.Status != "confirmed" {
			tx.Status = "confirmed"
			confirmedCount++
			ct.logger.Info().
				Str("tx_hash", tx.TxHash).
				Uint64("confirmations", confirmations).
				Uint64("standard_threshold", ct.standardInbound).
				Msg("transaction confirmed (standard)")
		}
		
		// Save within the transaction
		if err := dbTx.Save(&tx).Error; err != nil {
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
		Str("chain_id", chainID).
		Int("updated_count", updatedCount).
		Int("fast_confirmed_count", fastConfirmedCount).
		Int("confirmed_count", confirmedCount).
		Msg("confirmation updates committed")
	
	return nil
}

// IsConfirmed checks if a transaction has enough confirmations
func (ct *ConfirmationTracker) IsConfirmed(
	txHash string,
	mode string,
) (bool, error) {
	var tx store.GatewayTransaction
	err := ct.db.Client().Where("tx_hash = ?", txHash).First(&tx).Error
	if err != nil {
		return false, fmt.Errorf("transaction not found: %w", err)
	}
	
	var confirmed bool
	if mode == "fast" {
		// For fast mode, accept both fast_confirmed and confirmed status
		confirmed = tx.Status == "fast_confirmed" || tx.Status == "confirmed"
	} else {
		// For standard mode, only accept confirmed status
		confirmed = tx.Status == "confirmed"
	}
	
	// Log when checking reorged transactions for visibility
	if tx.Status == "reorged" {
		ct.logger.Debug().
			Str("tx_hash", txHash).
			Str("mode", mode).
			Msg("transaction was reorganized out - returning false")
	}
	
	ct.logger.Debug().
		Str("tx_hash", txHash).
		Str("mode", mode).
		Str("status", tx.Status).
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
func (ct *ConfirmationTracker) GetGatewayTransaction(txHash string) (*store.GatewayTransaction, error) {
	var tx store.GatewayTransaction
	err := ct.db.Client().Where("tx_hash = ?", txHash).First(&tx).Error
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}
	return &tx, nil
}

// GetConfirmedTransactions returns all confirmed transactions for a chain
func (ct *ConfirmationTracker) GetConfirmedTransactions(chainID string) ([]store.GatewayTransaction, error) {
	var txs []store.GatewayTransaction
	err := ct.db.Client().
		Where("chain_id = ? AND status = ?", chainID, "confirmed").
		Find(&txs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch confirmed transactions: %w", err)
	}
	return txs, nil
}

// MarkTransactionFailed marks a transaction as failed
func (ct *ConfirmationTracker) MarkTransactionFailed(txHash string) error {
	return ct.db.Client().
		Model(&store.GatewayTransaction{}).
		Where("tx_hash = ?", txHash).
		Update("status", "failed").Error
}