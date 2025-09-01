package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/universalClient/authz"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/keys"
	"github.com/rollchains/pchain/universalClient/store"
	uetypes "github.com/rollchains/pchain/x/ue/types"
	"github.com/rs/zerolog"
)

// VoteHandler handles voting on confirmed inbound transactions
type VoteHandler struct {
	txSigner *authz.TxSigner
	db       *db.DB
	log      zerolog.Logger
	keys     keys.UniversalValidatorKeys
	granter  string // operator address who granted AuthZ permissions
}

// NewVoteHandler creates a new vote handler
func NewVoteHandler(
	txSigner *authz.TxSigner,
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
		Uint64("block", tx.BlockNumber).
		Str("method", tx.Method).
		Msg("voting on inbound transaction")

	// Extract inbound data from transaction
	inbound, err := vh.constructInbound(tx)
	if err != nil {
		return fmt.Errorf("failed to construct inbound: %w", err)
	}

	// Execute MsgVoteInbound via AuthZ
	if err := vh.executeVote(ctx, inbound); err != nil {
		vh.log.Error().
			Str("tx_hash", tx.TxHash).
			Err(err).
			Msg("failed to vote on transaction")
		return err // Keep as pending for retry
	}

	// Update status to confirmed only after successful vote
	tx.Status = "confirmed"
	tx.UpdatedAt = time.Now()
	
	if err := vh.db.Client().Save(tx).Error; err != nil {
		return fmt.Errorf("failed to update transaction status: %w", err)
	}

	vh.log.Info().
		Str("tx_hash", tx.TxHash).
		Msg("transaction voted and confirmed")

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
	
	// Default to SYNTHETIC type if not specified
	txType := uetypes.InboundTxType_SYNTHETIC
	if txTypeStr, ok := eventData["tx_type"].(string); ok {
		switch txTypeStr {
		case "FEE_ABSTRACTION":
			txType = uetypes.InboundTxType_FEE_ABSTRACTION
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
	// Create MsgVoteInbound
	msg := &uetypes.MsgVoteInbound{
		Signer:  vh.granter, // The granter (operator) is the signer
		Inbound: inbound,
	}

	// Wrap and sign with AuthZ
	msgs := []sdk.Msg{msg}
	
	// Execute via AuthZ with reasonable gas and fees
	gasLimit := uint64(500000)
	feeAmount, err := sdk.ParseCoinsNormalized("500000000000000upc")
	if err != nil {
		return fmt.Errorf("failed to parse fee amount: %w", err)
	}

	memo := fmt.Sprintf("Vote on inbound tx %s", inbound.TxHash)
	
	// Sign and broadcast the AuthZ transaction
	txResp, err := vh.txSigner.SignAndBroadcastAuthZTx(
		ctx,
		msgs,
		memo,
		gasLimit,
		feeAmount,
	)
	
	if err != nil {
		return fmt.Errorf("failed to broadcast vote transaction: %w", err)
	}

	if txResp.Code != 0 {
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
		Where("status = ? AND confirmations >= ?", "pending", minConfirmations).
		Find(&pendingTxs).Error
		
	return pendingTxs, err
}