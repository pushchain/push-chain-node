package evm

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// TransactionVerifier handles transaction verification for EVM
type TransactionVerifier struct {
	parentClient *Client
	config       *uregistrytypes.ChainConfig
	database     *db.DB
	tracker      *common.ConfirmationTracker
	logger       zerolog.Logger
}

// NewTransactionVerifier creates a new transaction verifier
func NewTransactionVerifier(
	parentClient *Client,
	config *uregistrytypes.ChainConfig,
	database *db.DB,
	tracker *common.ConfirmationTracker,
	logger zerolog.Logger,
) *TransactionVerifier {
	return &TransactionVerifier{
		parentClient: parentClient,
		config:       config,
		database:     database,
		tracker:      tracker,
		logger:       logger.With().Str("component", "evm_tx_verifier").Logger(),
	}
}

// GetTransactionConfirmations returns the number of confirmations for a transaction
func (tv *TransactionVerifier) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	// Get transaction receipt
	hash := ethcommon.HexToHash(txHash)
	var receipt *types.Receipt
	err := tv.parentClient.executeWithFailover(ctx, "get_transaction_receipt", func(client *ethclient.Client) error {
		var innerErr error
		receipt, innerErr = client.TransactionReceipt(ctx, hash)
		return innerErr
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if receipt == nil || receipt.BlockNumber == nil {
		return 0, nil
	}

	// Get current block number
	var currentBlock uint64
	err = tv.parentClient.executeWithFailover(ctx, "get_block_number", func(client *ethclient.Client) error {
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

	confirmations := currentBlock - receipt.BlockNumber.Uint64() + 1
	return confirmations, nil
}

// VerifyTransactionExistence checks if an EVM transaction still exists on chain (reorg detection)
func (tv *TransactionVerifier) VerifyTransactionExistence(
	ctx context.Context,
	tx *store.ChainTransaction,
) (bool, error) {
	hash := ethcommon.HexToHash(tx.TxHash)
	var receipt *types.Receipt

	err := tv.parentClient.executeWithFailover(ctx, "get_transaction_receipt", func(client *ethclient.Client) error {
		var innerErr error
		receipt, innerErr = client.TransactionReceipt(ctx, hash)
		return innerErr
	})

	// Handle different error cases
	if err != nil {
		// Check if this is a "not found" error vs an RPC/network error
		if errors.Is(err, ethereum.NotFound) {
			// Transaction genuinely not found on chain - likely reorganized out
			tv.logger.Warn().
				Str("tx_hash", tx.TxHash).
				Uint64("original_block", tx.BlockNumber).
				Msg("EVM transaction not found on chain - marking as reorged")

			tx.Status = "reorged"
			tx.Confirmations = 0
			return false, nil
		}

		// RPC/network error - don't change status, return error for retry
		tv.logger.Error().
			Str("tx_hash", tx.TxHash).
			Err(err).
			Msg("RPC error while verifying transaction - will retry")
		return false, fmt.Errorf("RPC error verifying transaction: %w", err)
	}

	// Check if receipt exists and transaction succeeded
	if receipt == nil {
		tv.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Msg("EVM transaction receipt is nil - transaction may have been reorged")
		tx.Status = "reorged"
		return false, nil
	}

	// Check transaction status (1 = success, 0 = failure)
	if receipt.Status == 0 {
		tv.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("status", receipt.Status).
			Msg("EVM transaction failed on-chain")
		tx.Status = "failed"
		return false, nil
	}

	// Check if block number matches (detect if transaction moved to different block)
	if receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() != tx.BlockNumber {
		tv.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("expected_block", tx.BlockNumber).
			Uint64("actual_block", receipt.BlockNumber.Uint64()).
			Msg("EVM transaction moved to different block - possible reorg")

		// Update to new block number
		tx.BlockNumber = receipt.BlockNumber.Uint64()
		// Note: We still return true because the transaction exists, just in a different block
	}

	return true, nil
}

// VerifyPendingTransactions checks all pending transactions for reorgs
func (tv *TransactionVerifier) VerifyPendingTransactions(ctx context.Context) error {
	var pendingTxs []store.ChainTransaction

	// Get all transactions that need verification
	err := tv.database.Client().
		Where("status IN (?)", []string{"confirmation_pending", "awaiting_vote"}).
		Find(&pendingTxs).Error
	if err != nil {
		return fmt.Errorf("failed to fetch pending transactions for verification: %w", err)
	}

	tv.logger.Debug().
		Str("chain_id", tv.config.Chain).
		Int("pending_count", len(pendingTxs)).
		Msg("verifying EVM transactions for reorgs")

	// Verify each transaction
	for _, tx := range pendingTxs {
		exists, err := tv.VerifyTransactionExistence(ctx, &tx)
		if err != nil {
			// RPC error - log but don't change status, will retry next time
			tv.logger.Error().
				Err(err).
				Str("tx_hash", tx.TxHash).
				Msg("RPC error verifying EVM transaction - will retry later")
			continue
		}

		// If transaction status changed (reorged, failed, or moved), save the updated status
		if !exists {
			if err := tv.database.Client().Save(&tx).Error; err != nil {
				tv.logger.Error().
					Err(err).
					Str("tx_hash", tx.TxHash).
					Str("new_status", tx.Status).
					Msg("failed to update EVM transaction status")
			}
		}
	}

	return nil
}