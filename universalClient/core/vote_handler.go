package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/mr-tron/base58"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
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

// base58ToHex converts a base58 encoded string to hex format (0x...)
func (vh *VoteHandler) base58ToHex(base58Str string) (string, error) {
	if base58Str == "" {
		return "0x", nil
	}

	// Check if it's already in hex format
	if strings.HasPrefix(base58Str, "0x") {
		return base58Str, nil
	}

	// Decode base58 to bytes
	decoded, err := base58.Decode(base58Str)
	if err != nil {
		return "", fmt.Errorf("failed to decode base58: %w", err)
	}

	// Convert to hex with 0x prefix
	return "0x" + hex.EncodeToString(decoded), nil
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

	voteTxHash, err := vh.executeVote(ctx, inbound)
	if err != nil {
		vh.log.Error().
			Str("tx_hash", tx.TxHash).
			Err(err).
			Msg("failed to vote on transaction - keeping status as awaiting_vote for retry")
		return err // Keep as awaiting_vote for retry
	}

	vh.log.Debug().
		Str("tx_hash", tx.TxHash).
		Str("vote_tx_hash", voteTxHash).
		Msg("blockchain vote successful, now updating database status")

	// STEP 2: Only after successful vote, update DB status (minimal transaction time)
	// Use conditional update to prevent race conditions
	originalStatus := tx.Status

	// Use a short timeout for database operations (5 seconds is plenty)
	dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dbCancel()

	// Update status atomically using conditional WHERE clause
	// This prevents race conditions if multiple workers process the same transaction
	votedAt := time.Now()
	result := vh.db.Client().WithContext(dbCtx).
		Model(&store.ChainTransaction{}).
		Where("id = ? AND status IN (?)", tx.ID, []string{"confirmation_pending", "awaiting_vote"}).
		Updates(map[string]interface{}{
			"status":       "confirmed",
			"vote_tx_hash": voteTxHash,
			"voted_at":     votedAt,
			"updated_at":   votedAt,
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
		Str("vote_tx_hash", voteTxHash).
		Uint32("tx_id", uint32(tx.ID)).
		Str("status_change", fmt.Sprintf("%s -> %s", originalStatus, tx.Status)).
		Int64("rows_affected", result.RowsAffected).
		Msg("transaction voted and confirmed successfully")

	return nil
}

// constructInbound creates an Inbound message from transaction data
func (vh *VoteHandler) constructInbound(tx *store.ChainTransaction) (*uetypes.Inbound, error) {
	// Initialize event data map
	var eventData common.TxWithFundsPayload

	if tx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}

	if tx.Data == nil {
		return nil, fmt.Errorf("transaction data is missing for tx_hash: %s", tx.TxHash)
	}

	if err := json.Unmarshal(tx.Data, &eventData); err != nil {
		return nil, fmt.Errorf("failed to  unmarshal transaction data: %w", err)
	}

	// Map txType from eventData to proper enum value
	// Event data uses: 0=GAS, 1=GAS_AND_PAYLOAD, 2=FUNDS, 3=FUNDS_AND_PAYLOAD
	// Enum values are: 0=UNSPECIFIED_TX, 1=GAS, 2=FUNDS, 3=FUNDS_AND_PAYLOAD, 4=GAS_AND_PAYLOAD
	txType := uetypes.InboundTxType_UNSPECIFIED_TX
	switch eventData.TxType {
	case 0:
		txType = uetypes.InboundTxType_GAS
	case 1:
		txType = uetypes.InboundTxType_GAS_AND_PAYLOAD
	case 2:
		txType = uetypes.InboundTxType_FUNDS
	case 3:
		txType = uetypes.InboundTxType_FUNDS_AND_PAYLOAD
	default:
		// For any unknown value, default to GAS
		txType = uetypes.InboundTxType_UNSPECIFIED_TX
	}

	// Convert tx.TxHash to hex format if it's in base58
	txHashHex, err := vh.base58ToHex(tx.TxHash)
	if err != nil {
		vh.log.Warn().
			Str("tx_hash", tx.TxHash).
			Err(err).
			Msg("failed to convert txHash to hex, using original value")
		txHashHex = tx.TxHash
	}

	inboundMsg := &uetypes.Inbound{
		SourceChain: eventData.SourceChain,
		TxHash:      txHashHex,
		Sender:      eventData.Sender,
		Amount:      eventData.BridgeAmount,
		AssetAddr:   eventData.BridgeToken,
		LogIndex:    strconv.FormatUint(uint64(eventData.LogIndex), 10),
		TxType:      txType,
	}

	// Check if VerificationData is zero hash and replace with TxHash
	if strings.ToLower(strings.TrimPrefix(eventData.VerificationData, "0x")) == strings.Repeat("0", 64) {
		inboundMsg.VerificationData = txHashHex
	} else {
		inboundMsg.VerificationData = eventData.VerificationData
	}

	// Set recipient for transactions that involve funds
	if txType == uetypes.InboundTxType_FUNDS {
		inboundMsg.Recipient = eventData.Recipient
	}

	if txType == uetypes.InboundTxType_FUNDS_AND_PAYLOAD {
		inboundMsg.VerificationData = eventData.VerificationData
		inboundMsg.UniversalPayload = &eventData.UniversalPayload
	}

	return inboundMsg, nil
}

// executeVote executes the MsgVoteInbound transaction via AuthZ and returns the vote tx hash
func (vh *VoteHandler) executeVote(ctx context.Context, inbound *uetypes.Inbound) (string, error) {
	vh.log.Debug().
		Str("inbound_tx", inbound.TxHash).
		Str("granter", vh.granter).
		Str("source_chain", inbound.SourceChain).
		Msg("starting vote execution - creating MsgVoteInbound")

	// Check if txSigner is available
	if vh.txSigner == nil {
		return "", fmt.Errorf("txSigner is nil - cannot sign transactions")
	}

	// Validate granter address
	if vh.granter == "" {
		return "", fmt.Errorf("granter address is empty - AuthZ not properly configured")
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
	gasLimit := uint64(500000000)
	feeAmount, err := sdk.ParseCoinsNormalized("500000000000000upc")
	if err != nil {
		return "", fmt.Errorf("failed to parse fee amount: %w", err)
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
		return "", fmt.Errorf("failed to broadcast vote transaction: %w", err)
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
		return "", fmt.Errorf("vote transaction failed with code %d: %s", txResp.Code, txResp.RawLog)
	}

	vh.log.Info().
		Str("tx_hash", txResp.TxHash).
		Str("inbound_tx", inbound.TxHash).
		Int64("gas_used", txResp.GasUsed).
		Msg("successfully voted on inbound transaction")

	return txResp.TxHash, nil
}

// GetPendingTransactions returns all transactions that have enough confirmations but haven't been voted on
func (vh *VoteHandler) GetPendingTransactions(minConfirmations uint64) ([]store.ChainTransaction, error) {
	var pendingTxs []store.ChainTransaction

	err := vh.db.Client().
		Where("status IN (?) AND confirmations >= ?", []string{"confirmation_pending", "awaiting_vote"}, minConfirmations).
		Find(&pendingTxs).Error

	return pendingTxs, err
}
