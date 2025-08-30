package svm

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/rollchains/pchain/universalClient/chains/common"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/store"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// TransactionVerifier handles transaction verification for Solana
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
		logger:       logger.With().Str("component", "svm_tx_verifier").Logger(),
	}
}

// GetTransactionConfirmations returns the number of confirmations for a transaction
func (tv *TransactionVerifier) GetTransactionConfirmations(ctx context.Context, txHash string) (uint64, error) {
	// Parse signature
	sig, err := solana.SignatureFromBase58(txHash)
	if err != nil {
		return 0, fmt.Errorf("invalid transaction hash: %w", err)
	}

	// Get transaction status
	var statuses *rpc.GetSignatureStatusesResult
	err = tv.parentClient.executeWithFailover(ctx, "get_signature_statuses", func(client *rpc.Client) error {
		var innerErr error
		statuses, innerErr = client.GetSignatureStatuses(ctx, false, sig)
		return innerErr
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get signature status: %w", err)
	}

	if len(statuses.Value) == 0 || statuses.Value[0] == nil {
		return 0, fmt.Errorf("transaction not found")
	}

	status := statuses.Value[0]
	
	// Map Solana confirmation status to confirmation count
	// Solana uses different confirmation levels rather than counts
	switch status.ConfirmationStatus {
	case rpc.ConfirmationStatusProcessed:
		return 1, nil
	case rpc.ConfirmationStatusConfirmed:
		return 5, nil // Approximately equivalent to "fast" confirmations
	case rpc.ConfirmationStatusFinalized:
		return 12, nil // Approximately equivalent to "standard" confirmations
	default:
		return 0, nil
	}
}

// IsConfirmed checks if a transaction has enough confirmations
func (tv *TransactionVerifier) IsConfirmed(ctx context.Context, txHash string, mode string) (bool, error) {
	// Check in tracker first
	confirmed, err := tv.tracker.IsConfirmed(txHash, mode)
	if err == nil {
		return confirmed, nil
	}

	// Fallback to chain query
	confirmations, err := tv.GetTransactionConfirmations(ctx, txHash)
	if err != nil {
		return false, err
	}

	required := tv.tracker.GetRequiredConfirmations(mode)
	return confirmations >= required, nil
}

// VerifyTransactionExistence checks if a Solana transaction still exists on chain
func (tv *TransactionVerifier) VerifyTransactionExistence(
	ctx context.Context,
	tx *store.ChainTransaction,
) (bool, error) {
	// Parse signature
	sig, err := solana.SignatureFromBase58(tx.TxHash)
	if err != nil {
		tv.logger.Error().
			Err(err).
			Str("tx_hash", tx.TxHash).
			Msg("invalid transaction hash format")
		return false, fmt.Errorf("invalid transaction hash: %w", err)
	}

	// Get transaction details to verify it exists
	var txResult *rpc.GetTransactionResult
	err = tv.parentClient.executeWithFailover(ctx, "get_transaction", func(client *rpc.Client) error {
		var innerErr error
		txResult, innerErr = client.GetTransaction(
			ctx,
			sig,
			&rpc.GetTransactionOpts{
				Encoding: solana.EncodingBase64,
			},
		)
		return innerErr
	})
	
	if err != nil {
		// Transaction not found - likely dropped or reorganized
		tv.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_slot", tx.BlockNumber).
			Err(err).
			Msg("Solana transaction not found on chain - marking as reorged")
		
		tx.Status = "reorged"
		tx.Confirmations = 0
		return false, nil
	}

	// Check if transaction moved to a different slot
	if txResult.Slot != tx.BlockNumber {
		tv.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Uint64("original_slot", tx.BlockNumber).
			Uint64("new_slot", txResult.Slot).
			Msg("Solana transaction moved to different slot - updating slot number")
		
		// Update slot number and reset status
		tx.BlockNumber = txResult.Slot
		tx.Status = "pending"
		tx.Confirmations = 0
		return false, nil
	}

	// Check if transaction failed
	if txResult.Meta != nil && txResult.Meta.Err != nil {
		tv.logger.Warn().
			Str("tx_hash", tx.TxHash).
			Interface("error", txResult.Meta.Err).
			Msg("Solana transaction failed on chain")
		
		tx.Status = "failed"
		return false, nil
	}

	return true, nil
}

// VerifyPendingTransactions checks all pending/fast_confirmed transactions for reorgs
func (tv *TransactionVerifier) VerifyPendingTransactions(ctx context.Context) error {
	var pendingTxs []store.ChainTransaction
	
	// Get all transactions that need verification
	err := tv.database.Client().
		Where("status IN (?)", []string{"pending", "fast_confirmed"}).
		Find(&pendingTxs).Error
	if err != nil {
		return fmt.Errorf("failed to fetch pending transactions for verification: %w", err)
	}

	tv.logger.Debug().
		Str("chain_id", tv.config.Chain).
		Int("pending_count", len(pendingTxs)).
		Msg("verifying Solana transactions")
	
	// Verify each transaction
	for _, tx := range pendingTxs {
		exists, err := tv.VerifyTransactionExistence(ctx, &tx)
		if err != nil {
			tv.logger.Error().
				Err(err).
				Str("tx_hash", tx.TxHash).
				Msg("failed to verify Solana transaction existence")
			continue
		}
		
		// If transaction was reorged, failed, or moved, save the updated status
		if !exists {
			if err := tv.database.Client().Save(&tx).Error; err != nil {
				tv.logger.Error().
					Err(err).
					Str("tx_hash", tx.TxHash).
					Msg("failed to update Solana transaction status")
			}
		}
	}
	
	return nil
}

// GetTransactionStatus returns detailed status of a transaction
func (tv *TransactionVerifier) GetTransactionStatus(ctx context.Context, txHash string) (*TransactionStatus, error) {
	// Parse signature
	sig, err := solana.SignatureFromBase58(txHash)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction hash: %w", err)
	}

	// Get transaction status
	var statuses *rpc.GetSignatureStatusesResult
	err = tv.parentClient.executeWithFailover(ctx, "get_signature_statuses", func(client *rpc.Client) error {
		var innerErr error
		statuses, innerErr = client.GetSignatureStatuses(ctx, false, sig)
		return innerErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get signature status: %w", err)
	}

	if len(statuses.Value) == 0 || statuses.Value[0] == nil {
		return &TransactionStatus{
			Exists: false,
			Status: "not_found",
		}, nil
	}

	status := statuses.Value[0]
	
	result := &TransactionStatus{
		Exists:             true,
		Slot:               status.Slot,
		ConfirmationStatus: string(status.ConfirmationStatus),
	}

	// Set status based on confirmation level
	switch status.ConfirmationStatus {
	case rpc.ConfirmationStatusProcessed:
		result.Status = "processed"
		result.Confirmations = 1
	case rpc.ConfirmationStatusConfirmed:
		result.Status = "confirmed"
		result.Confirmations = 5
	case rpc.ConfirmationStatusFinalized:
		result.Status = "finalized"
		result.Confirmations = 12
	default:
		result.Status = "pending"
		result.Confirmations = 0
	}

	// Check if transaction failed
	if status.Err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("%v", status.Err)
	}

	return result, nil
}

// TransactionStatus holds detailed transaction status information
type TransactionStatus struct {
	Exists             bool   `json:"exists"`
	Status             string `json:"status"`
	Slot               uint64 `json:"slot"`
	Confirmations      uint64 `json:"confirmations"`
	ConfirmationStatus string `json:"confirmation_status"`
	Error              string `json:"error,omitempty"`
}

// WaitForTransaction waits for a transaction to reach a specific confirmation level
func (tv *TransactionVerifier) WaitForTransaction(
	ctx context.Context,
	txHash string,
	confirmationLevel string,
) error {
	// Check if transaction exists in database first
	var dbTx store.ChainTransaction
	err := tv.database.Client().
		Where("tx_hash = ?", txHash).
		First(&dbTx).Error
	
	if err != nil && err != gorm.ErrRecordNotFound {
		return fmt.Errorf("failed to query transaction: %w", err)
	}

	// Monitor transaction status
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Check confirmation status
			confirmed, err := tv.IsConfirmed(ctx, txHash, confirmationLevel)
			if err != nil {
				tv.logger.Debug().
					Err(err).
					Str("tx_hash", txHash).
					Msg("error checking confirmation status")
				continue
			}

			if confirmed {
				tv.logger.Info().
					Str("tx_hash", txHash).
					Str("level", confirmationLevel).
					Msg("transaction confirmed")
				return nil
			}

			// Check if transaction failed
			status, err := tv.GetTransactionStatus(ctx, txHash)
			if err == nil && status.Status == "failed" {
				return fmt.Errorf("transaction failed: %s", status.Error)
			}
		}
	}
}