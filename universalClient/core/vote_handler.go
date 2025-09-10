package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/rs/zerolog"
)

// TxSignerInterface defines the interface for transaction signing
type TxSignerInterface interface {
	SignAndBroadcastAuthZTx(ctx context.Context, msgs []sdk.Msg, memo string, gasLimit uint64, feeAmount sdk.Coins) (*sdk.TxResponse, error)
}

// VoteHandler handles voting on confirmed inbound transactions
type VoteHandler struct {
	txSigner TxSignerInterface
	db       *db.DB
	log      zerolog.Logger
	keys     keys.UniversalValidatorKeys
	granter  string // operator address who granted AuthZ permissions
}

// NewVoteHandler creates a new vote handler
func NewVoteHandler(
	txSigner TxSignerInterface,
	db *db.DB,
	log zerolog.Logger,
	keys keys.UniversalValidatorKeys,
	granter string,
) *VoteHandler {
	return &VoteHandler{
		txSigner: txSigner,
		db:       db,
		log:      log,
		keys:     keys,
		granter:  granter,
	}
}

// VoteAndConfirm votes on a transaction and updates its status to confirmed
func (vh *VoteHandler) VoteAndConfirm(ctx context.Context, tx *store.ChainTransaction) error {
	vh.log.Info().
		Str("tx_hash", tx.TxHash).
		Uint32("tx_id", uint32(tx.ID)).
		Uint64("block", tx.BlockNumber).
		Str("method", tx.Method).
		Str("current_status", tx.Status).
		Msg("starting vote and confirm process")

	// Extract inbound data from transaction
	inbound, err := vh.constructInbound(tx)
	if err != nil {
		return fmt.Errorf("failed to construct inbound: %w", err)
	}

	// STEP 1: Execute blockchain vote FIRST (without holding DB transaction)
	vh.log.Debug().
		Str("tx_hash", tx.TxHash).
		Msg("executing vote on blockchain (no DB transaction held)")

	if err := vh.executeVote(ctx, inbound); err != nil {
		vh.log.Error().
			Str("tx_hash", tx.TxHash).
			Err(err).
			Msg("failed to vote on transaction - keeping status as awaiting_vote for retry")
		return err // Keep as awaiting_vote for retry
	}

	vh.log.Debug().
		Str("tx_hash", tx.TxHash).
		Msg("blockchain vote successful, now updating database status")

	// STEP 2: Only after successful vote, update DB status (minimal transaction time)
	// Use conditional update to prevent race conditions
	originalStatus := tx.Status

	// Use a short timeout for database operations (5 seconds is plenty)
	dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dbCancel()

	// Update status atomically using conditional WHERE clause
	// This prevents race conditions if multiple workers process the same transaction
	result := vh.db.Client().WithContext(dbCtx).
		Model(&store.ChainTransaction{}).
		Where("id = ? AND status IN (?)", tx.ID, []string{"confirmation_pending", "awaiting_vote"}).
		Updates(map[string]interface{}{
			"status":     "confirmed",
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		vh.log.Error().
			Str("tx_hash", tx.TxHash).
			Err(result.Error).
			Msg("failed to update transaction status in database")
		// Note: The vote was successful, but we failed to update the DB
		// The transaction will remain in awaiting_vote and might be voted on again
		// This is safe because voting is idempotent
		return fmt.Errorf("failed to update transaction status after successful vote: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		// Transaction was already processed by another worker or status changed
		vh.log.Warn().
			Str("tx_hash", tx.TxHash).
			Str("expected_status", "awaiting_vote").
			Msg("transaction status was already changed - possibly processed by another worker")
		// This is not an error - the transaction was successfully processed
		return nil
	}

	// Update the local transaction object to reflect the new status
	tx.Status = "confirmed"
	tx.UpdatedAt = time.Now()

	vh.log.Info().
		Str("tx_hash", tx.TxHash).
		Uint32("tx_id", uint32(tx.ID)).
		Str("status_change", fmt.Sprintf("%s -> %s", originalStatus, tx.Status)).
		Int64("rows_affected", result.RowsAffected).
		Msg("transaction voted and confirmed successfully")

	return nil
}

// constructInbound creates an Inbound message from transaction data
func (vh *VoteHandler) constructInbound(tx *store.ChainTransaction) (*uetypes.Inbound, error) {
	// Initialize event data map
	var eventData map[string]interface{}

	// Check if Data field has content
	if tx.Data != nil && len(tx.Data) > 0 {
		// Try to parse the transaction data
		if err := json.Unmarshal(tx.Data, &eventData); err != nil {
			vh.log.Warn().
				Str("tx_hash", tx.TxHash).
				Err(err).
				Msg("failed to parse transaction data, using minimal data")
			eventData = make(map[string]interface{})
		}
	} else {
		// No data provided, create minimal structure
		eventData = make(map[string]interface{})
	}

	// Determine source chain based on method
	sourceChain, _ := eventData["source_chain"].(string)
	if sourceChain == "" {
		sourceChain, _ = eventData["chain_id"].(string)
	}
	if sourceChain == "" {
		// Infer chain from method or use a default
		// EVM methods typically use "addFunds", Solana uses "add_funds"
		if tx.Method == "addFunds" {
			sourceChain = "eip155:11155111" // Sepolia testnet
		} else if tx.Method == "add_funds" {
			sourceChain = "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1" // Solana devnet
		}
	}

	// Extract fields with defaults
	sender, _ := eventData["sender"].(string)
	if sender == "" {
		sender = "0x0000000000000000000000000000000000000000" // Default sender
	}

	recipient, _ := eventData["recipient"].(string)
	if recipient == "" {
		recipient = "0x0000000000000000000000000000000000000000" // Default recipient
	}

	amount, _ := eventData["amount"].(string)
	if amount == "" {
		amount = "0" // Default amount
	}

	assetAddr, _ := eventData["asset_address"].(string)
	if assetAddr == "" {
		assetAddr = "0x0000000000000000000000000000000000000000" // Default asset
	}

	logIndex, _ := eventData["log_index"].(string)
	if logIndex == "" {
		logIndex = "0" // Default log index
	}

	// Default to FUNDS_AND_PAYLOAD_TX type if not specified
	txType := uetypes.InboundTxType_FUNDS_AND_PAYLOAD_TX
	if txTypeStr, ok := eventData["tx_type"].(string); ok {
		switch txTypeStr {
		case "GAS_FUND", "FEE_ABSTRACTION":
			txType = uetypes.InboundTxType_GAS_FUND_TX
		case "FUNDS_BRIDGE":
			txType = uetypes.InboundTxType_FUNDS_BRIDGE_TX
		case "FUNDS_AND_PAYLOAD_INSTANT":
			txType = uetypes.InboundTxType_FUNDS_AND_PAYLOAD_INSTANT_TX
		case "SYNTHETIC", "FUNDS_AND_PAYLOAD":
			txType = uetypes.InboundTxType_FUNDS_AND_PAYLOAD_TX
		case "UNSPECIFIED":
			txType = uetypes.InboundTxType_UNSPECIFIED_TX
		}
	}

	return &uetypes.Inbound{
		SourceChain: sourceChain,
		TxHash:      tx.TxHash,
		Sender:      sender,
		Recipient:   recipient,
		Amount:      amount,
		AssetAddr:   assetAddr,
		LogIndex:    logIndex,
		TxType:      txType,
	}, nil
}

// executeVote executes the MsgVoteInbound transaction via AuthZ
func (vh *VoteHandler) executeVote(ctx context.Context, inbound *uetypes.Inbound) error {
	vh.log.Debug().
		Str("inbound_tx", inbound.TxHash).
		Str("granter", vh.granter).
		Str("source_chain", inbound.SourceChain).
		Msg("starting vote execution - creating MsgVoteInbound")

	// Check if txSigner is available
	if vh.txSigner == nil {
		return fmt.Errorf("txSigner is nil - cannot sign transactions")
	}

	// Validate granter address
	if vh.granter == "" {
		return fmt.Errorf("granter address is empty - AuthZ not properly configured")
	}

	// Create MsgVoteInbound
	msg := &uetypes.MsgVoteInbound{
		Signer:  vh.granter, // The granter (operator) is the signer
		Inbound: inbound,
	}

	vh.log.Debug().
		Str("inbound_tx", inbound.TxHash).
		Str("msg_signer", msg.Signer).
		Msg("created MsgVoteInbound message")

	// Wrap and sign with AuthZ
	msgs := []sdk.Msg{msg}

	// Execute via AuthZ with reasonable gas and fees
	gasLimit := uint64(500000)
	feeAmount, err := sdk.ParseCoinsNormalized("500000000000000upc")
	if err != nil {
		return fmt.Errorf("failed to parse fee amount: %w", err)
	}

	memo := fmt.Sprintf("Vote on inbound tx %s", inbound.TxHash)

	vh.log.Debug().
		Str("inbound_tx", inbound.TxHash).
		Uint64("gas_limit", gasLimit).
		Str("fee_amount", feeAmount.String()).
		Str("memo", memo).
		Msg("prepared transaction parameters, calling SignAndBroadcastAuthZTx")

	// Create timeout context for the AuthZ transaction (30 second timeout)
	voteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Sign and broadcast the AuthZ transaction
	vh.log.Info().
		Str("inbound_tx", inbound.TxHash).
		Msg("calling SignAndBroadcastAuthZTx")

	txResp, err := vh.txSigner.SignAndBroadcastAuthZTx(
		voteCtx,
		msgs,
		memo,
		gasLimit,
		feeAmount,
	)

	vh.log.Debug().
		Str("inbound_tx", inbound.TxHash).
		Bool("success", err == nil).
		Msg("SignAndBroadcastAuthZTx completed")

	if err != nil {
		vh.log.Error().
			Str("inbound_tx", inbound.TxHash).
			Err(err).
			Msg("SignAndBroadcastAuthZTx failed")
		return fmt.Errorf("failed to broadcast vote transaction: %w", err)
	}

	vh.log.Debug().
		Str("inbound_tx", inbound.TxHash).
		Str("response_tx_hash", txResp.TxHash).
		Uint32("response_code", txResp.Code).
		Msg("received transaction response, checking status")

	if txResp.Code != 0 {
		vh.log.Error().
			Str("inbound_tx", inbound.TxHash).
			Str("response_tx_hash", txResp.TxHash).
			Uint32("response_code", txResp.Code).
			Str("raw_log", txResp.RawLog).
			Msg("vote transaction was rejected by blockchain")
		return fmt.Errorf("vote transaction failed with code %d: %s", txResp.Code, txResp.RawLog)
	}

	vh.log.Info().
		Str("tx_hash", txResp.TxHash).
		Str("inbound_tx", inbound.TxHash).
		Int64("gas_used", txResp.GasUsed).
		Msg("successfully voted on inbound transaction")

	return nil
}

// GetPendingTransactions returns all transactions that have enough confirmations but haven't been voted on
func (vh *VoteHandler) GetPendingTransactions(minConfirmations uint64) ([]store.ChainTransaction, error) {
	var pendingTxs []store.ChainTransaction

	err := vh.db.Client().
		Where("status IN (?) AND confirmations >= ?", []string{"confirmation_pending", "awaiting_vote"}, minConfirmations).
		Find(&pendingTxs).Error

	return pendingTxs, err
}
