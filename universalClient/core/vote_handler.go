package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Default to FUNDS_AND_PAYLOAD_TX type if not specified
	txType := uetypes.InboundTxType_GAS
	switch eventData.TxType {
	case 0:
		txType = uetypes.InboundTxType_GAS
	case 1:
		txType = uetypes.InboundTxType_GAS_AND_PAYLOAD
	case 2:
		txType = uetypes.InboundTxType_FUNDS
	case 3:
		txType = uetypes.InboundTxType_FUNDS_AND_PAYLOAD
	case 4:
		txType = uetypes.InboundTxType_UNSPECIFIED_TX
	}

	inboundMsg := &uetypes.Inbound{
		SourceChain: eventData.SourceChain,
		TxHash:      tx.TxHash,
		Sender:      eventData.Sender,
		Amount:      eventData.BridgeAmount,
		AssetAddr:   eventData.BridgeToken,
		LogIndex:    strconv.FormatUint(uint64(eventData.LogIndex), 10),
		TxType:      txType,
	}

	// Check if VerificationData is zero hash and replace with TxHash
	if strings.ToLower(strings.TrimPrefix(eventData.VerificationData, "0x")) == strings.Repeat("0", 64) {
		inboundMsg.VerificationData = tx.TxHash
	} else {
		inboundMsg.VerificationData = eventData.VerificationData
	}

	if txType == uetypes.InboundTxType_FUNDS {
		inboundMsg.Recipient = eventData.Recipient
	}

	if txType == uetypes.InboundTxType_FUNDS_AND_PAYLOAD {
		inboundMsg.VerificationData = eventData.VerificationData

		up, err := decodeUniversalPayload(eventData.Data)
		// if error, return error
		if err != nil {
			return nil, fmt.Errorf("failed to decode universal payload: %w", err)
		}
		inboundMsg.UniversalPayload = up
	}

	return inboundMsg, nil
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
	gasLimit := uint64(500000000)
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

// DecodeUniversalPayload takes a hex string and unmarshals it into UniversalPayload
func decodeUniversalPayload(hexStr string) (*uetypes.UniversalPayload, error) {
	// Handle empty string case
	if hexStr == "" || strings.TrimSpace(hexStr) == "" {
		return nil, nil
	}

	clean := strings.TrimPrefix(hexStr, "0x")

	// Handle case where hex string is empty after removing 0x prefix
	if clean == "" {
		return nil, nil
	}

	bz, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("hex decode: %w", err)
	}

	// Handle case where decoded bytes are empty
	if len(bz) == 0 {
		return nil, nil
	}

	// Try to decode as ABI-encoded UniversalPayload first
	up, err := decodeABIUniversalPayload(bz)
	if err == nil {
		return up, nil
	}

	// If ABI decoding fails, try protobuf decoding as fallback
	up = new(uetypes.UniversalPayload)
	if err := gogoproto.Unmarshal(bz, up); err != nil {
		return nil, fmt.Errorf("failed to decode UniversalPayload as both ABI and protobuf: ABI error: %v, protobuf error: %w", err, err)
	}
	return up, nil
}

// decodeABIUniversalPayload decodes ABI-encoded UniversalPayload data using standard library
func decodeABIUniversalPayload(data []byte) (*uetypes.UniversalPayload, error) {
	// The data starts with an offset to where the actual tuple data begins
	if len(data) < 32 {
		return nil, fmt.Errorf("insufficient data length: got %d, need at least 32", len(data))
	}

	// Read the offset (first 32 bytes)
	offset := new(big.Int).SetBytes(data[:32]).Uint64()

	// The actual tuple data starts at the offset
	if int(offset) >= len(data) {
		return nil, fmt.Errorf("offset %d exceeds data length %d", offset, len(data))
	}

	// Define the UniversalPayload struct components
	components := []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
		{Name: "gasLimit", Type: "uint256"},
		{Name: "maxFeePerGas", Type: "uint256"},
		{Name: "maxPriorityFeePerGas", Type: "uint256"},
		{Name: "nonce", Type: "uint256"},
		{Name: "deadline", Type: "uint256"},
		{Name: "vType", Type: "uint8"},
	}

	// Create the tuple type
	tupleType, err := abi.NewType("tuple", "UniversalPayload", components)
	if err != nil {
		return nil, fmt.Errorf("failed to create tuple type: %w", err)
	}

	// Create arguments from the tuple type
	args := abi.Arguments{
		{Type: tupleType},
	}

	// Unpack the tuple data using the full data (not just tupleData)
	// because dynamic fields like bytes are stored after the tuple
	decoded, err := args.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack tuple data: %w", err)
	}

	// Convert decoded data to UniversalPayload
	if len(decoded) != 1 {
		return nil, fmt.Errorf("expected 1 decoded value, got %d", len(decoded))
	}

	// Extract the struct from the decoded result using reflection
	// The struct has JSON tags, so we need to use reflection to access fields
	payloadValue := decoded[0]

	// Use reflection to get the struct fields
	payloadReflect := reflect.ValueOf(payloadValue)
	if payloadReflect.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %T", payloadValue)
	}

	// Get the struct type to access fields by name
	payloadType := payloadReflect.Type()

	// Helper function to get field value by name
	getField := func(name string) reflect.Value {
		field, found := payloadType.FieldByName(name)
		if !found {
			return reflect.Value{}
		}
		return payloadReflect.FieldByIndex(field.Index)
	}

	// Extract values using reflection
	toValue := getField("To")
	valueValue := getField("Value")
	dataValue := getField("Data")
	gasLimitValue := getField("GasLimit")
	maxFeePerGasValue := getField("MaxFeePerGas")
	maxPriorityFeePerGasValue := getField("MaxPriorityFeePerGas")
	nonceValue := getField("Nonce")
	deadlineValue := getField("Deadline")
	vTypeValue := getField("VType")

	// Convert to the expected types
	to, ok := toValue.Interface().(ethcommon.Address)
	if !ok {
		return nil, fmt.Errorf("expected address for 'to', got %T", toValue.Interface())
	}

	value, ok := valueValue.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'value', got %T", valueValue.Interface())
	}

	dataBytes, ok := dataValue.Interface().([]byte)
	if !ok {
		return nil, fmt.Errorf("expected []byte for 'data', got %T", dataValue.Interface())
	}

	gasLimit, ok := gasLimitValue.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'gasLimit', got %T", gasLimitValue.Interface())
	}

	maxFeePerGas, ok := maxFeePerGasValue.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'maxFeePerGas', got %T", maxFeePerGasValue.Interface())
	}

	maxPriorityFeePerGas, ok := maxPriorityFeePerGasValue.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'maxPriorityFeePerGas', got %T", maxPriorityFeePerGasValue.Interface())
	}

	nonce, ok := nonceValue.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'nonce', got %T", nonceValue.Interface())
	}

	deadline, ok := deadlineValue.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'deadline', got %T", deadlineValue.Interface())
	}

	vType, ok := vTypeValue.Interface().(uint8)
	if !ok {
		return nil, fmt.Errorf("expected uint8 for 'vType', got %T", vTypeValue.Interface())
	}

	// Create UniversalPayload
	up := &uetypes.UniversalPayload{
		To:    to.Hex(),
		Value: value.String(),
		// add 0x prefix to data
		Data:                 "0x" + hex.EncodeToString(dataBytes),
		GasLimit:             gasLimit.String(),
		MaxFeePerGas:         maxFeePerGas.String(),
		MaxPriorityFeePerGas: maxPriorityFeePerGas.String(),
		Nonce:                nonce.String(),
		Deadline:             deadline.String(),
		VType:                uetypes.VerificationType(vType),
	}

	return up, nil
}
