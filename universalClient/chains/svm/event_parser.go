package svm

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

// Event type constants
const (
	EventTypeSendFunds = "send_funds"
	// Outbound observation events (emitted by gateway on SVM since there's no vault)
	EventTypeFinalizeUniversalTx = "finalize_universal_tx"
	EventTypeRevertUniversalTx   = "revert_universal_tx"
	EventTypeFundsRescued        = "funds_rescued"
)

// base58ToHex converts a base58 encoded string to hex format (0x...)
func base58ToHex(base58Str string) (string, error) {
	if base58Str == "" {
		return "0x", nil
	}

	// Decode base58 to bytes
	decoded, err := base58.Decode(base58Str)
	if err != nil {
		return "", fmt.Errorf("failed to decode base58: %w", err)
	}

	// Convert to hex with 0x prefix
	return "0x" + hex.EncodeToString(decoded), nil
}

// ParseEvent parses a log into a store.Event based on the event type.
// eventType should be one of: "send_funds", "executeUniversalTx", "revertUniversalTx"
func ParseEvent(log string, signature string, slot uint64, logIndex uint, eventType string, chainID string, logger zerolog.Logger) *store.Event {
	switch eventType {
	case EventTypeSendFunds:
		return parseSendFundsEvent(log, signature, slot, logIndex, chainID, logger)
	case EventTypeFinalizeUniversalTx, EventTypeRevertUniversalTx, EventTypeFundsRescued:
		return parseOutboundObservationEvent(log, signature, slot, logIndex, eventType, chainID, logger)
	default:
		logger.Debug().
			Str("event_type", eventType).
			Str("signature", signature).
			Msg("unknown event type, skipping")
		return nil
	}
}

// parseSendFundsEvent parses a sendFunds event as UniversalTx
func parseSendFundsEvent(log string, signature string, slot uint64, logIndex uint, chainID string, logger zerolog.Logger) *store.Event {
	if !strings.HasPrefix(log, "Program data: ") {
		return nil
	}

	eventData := strings.TrimPrefix(log, "Program data: ")
	decoded, err := base64.StdEncoding.DecodeString(eventData)
	if err != nil {
		return nil
	}

	if len(decoded) < 8 {
		return nil
	}

	// Create EventID in format: signature:LogIndex
	eventID := fmt.Sprintf("%s:%d", signature, logIndex)

	logger.Debug().
		Str("event_id", eventID).
		Str("signature", signature).
		Uint("log_index", logIndex).
		Uint64("slot", slot).
		Msg("processing sendFunds event")

	// Create store.Event
	event := &store.Event{
		EventID:           eventID,
		BlockHeight:       slot,
		Type:              store.EventTypeInbound, // Gateway events from external chains are INBOUND
		Status:            store.StatusPending,
		ExpiryBlockHeight: 0, // Will be set based on confirmation type if needed
	}

	// Parse event data from this log. A malformed event is dropped rather than
	// stored half-decoded: the zero values it would carry are not neutral.
	if err := parseUniversalTxEvent(event, decoded, logIndex, chainID, logger); err != nil {
		logger.Warn().
			Err(err).
			Str("event_id", eventID).
			Msg("discarding malformed UniversalTx event")
		return nil
	}

	return event
}

// parseOutboundObservationEvent parses an outboundObservation event (UniversalTxFinalized)
// Event structure (Borsh serialized):
// - discriminator (8 bytes)
// - sub_tx_id (32 bytes)
// - universal_tx_id (32 bytes)
// The events diverge after universal_tx_id:
//
//	UniversalTxFinalized  ... wrapper_address(32) gas_fee(8)   gas_used -> 112
//	FundsRescued          ... token(32) amount(8)                       -> 112
//	RevertUniversalTx     ... revert_recipient(32) token(32) amount(8)  -> no gas_used
//
// Revert carries no gas_used: it always reimburses from the fee vault, and
// InboundFeeReimbursed.amount_lamports records the amount instead. Push never
// refunds on a revert (applyGasRefund returns early for INBOUND_REVERT), so the
// value is not load bearing on this path.
func parseOutboundObservationEvent(log string, signature string, slot uint64, logIndex uint, eventType string, chainID string, logger zerolog.Logger) *store.Event {
	if !strings.HasPrefix(log, "Program data: ") {
		return nil
	}

	eventData := strings.TrimPrefix(log, "Program data: ")
	decoded, err := base64.StdEncoding.DecodeString(eventData)
	if err != nil {
		return nil
	}

	// -1 means the event carries no gas_used field.
	gasUsedOffset := -1
	switch eventType {
	case EventTypeFinalizeUniversalTx, EventTypeFundsRescued:
		gasUsedOffset = 112
	case EventTypeRevertUniversalTx:
	default:
		return nil
	}

	need := 72 // the shared prefix
	if gasUsedOffset >= 0 {
		need = gasUsedOffset + 8
	}
	if len(decoded) < need {
		logger.Warn().
			Int("data_len", len(decoded)).
			Int("need", need).
			Str("event_type", eventType).
			Msg("data too short for outboundObservation event")
		return nil
	}

	// Create EventID in format: signature:LogIndex
	eventID := fmt.Sprintf("%s:%d", signature, logIndex)

	logger.Debug().
		Str("event_id", eventID).
		Str("signature", signature).
		Uint("log_index", logIndex).
		Uint64("slot", slot).
		Msg("processing outboundObservation event")

	// Shared prefix: disc(8) sub_tx_id(32) universal_tx_id(32).
	txID := "0x" + hex.EncodeToString(decoded[8:40])
	universalTxID := "0x" + hex.EncodeToString(decoded[40:72])
	gasFeeUsed := ""
	if gasUsedOffset >= 0 {
		gasFeeUsed = fmt.Sprintf("%d", binary.LittleEndian.Uint64(decoded[gasUsedOffset:gasUsedOffset+8]))
	}

	// Create OutboundEvent payload
	payload := common.OutboundEvent{
		TxID:          txID,
		UniversalTxID: universalTxID,
		GasFeeUsed:    gasFeeUsed,
	}

	// Marshal payload to JSON
	payloadData, err := json.Marshal(payload)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("signature", signature).
			Msg("failed to marshal outbound event payload")
		return nil
	}

	// Create store.Event
	event := &store.Event{
		EventID:           eventID,
		BlockHeight:       slot,
		Type:              store.EventTypeOutbound, // Outbound observation events
		Status:            store.StatusPending,
		ConfirmationType:  store.ConfirmationStandard, // Use STANDARD confirmation for outbound events
		ExpiryBlockHeight: 0,                          // 0 means no expiry
		EventData:         payloadData,
	}

	logger.Debug().
		Str("event_id", eventID).
		Str("tx_id", txID).
		Str("universal_tx_id", universalTxID).
		Str("gas_fee_used", gasFeeUsed).
		Msg("parsed outboundObservation event")

	return event
}

// parseUniversalTxEvent extracts specific data from a single log event
// For TxWithFunds events, it JSON-marshals the decoded fields into event.EventData.
func parseUniversalTxEvent(event *store.Event, decoded []byte, logIndex uint, chainID string, logger zerolog.Logger) error {
	// Parse the UniversalTx event
	payload, err := decodeUniversalTxEvent(decoded, logger)
	if err != nil {
		return fmt.Errorf("decode UniversalTx event: %w", err)
	}

	// Set source chain and log index
	payload.SourceChain = chainID
	payload.LogIndex = logIndex

	// Marshal and store into event.EventData
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal universal tx payload: %w", err)
	}
	event.EventData = b

	// if TxType is 0 or 1, use FAST else use STANDARD
	if payload.TxType == 0 || payload.TxType == 1 {
		event.ConfirmationType = store.ConfirmationFast
	} else {
		event.ConfirmationType = store.ConfirmationStandard
	}

	return nil
}

// decodeUniversalTxEvent decodes the gateway's Borsh-encoded UniversalTx event:
//
//	sender 32, recipient 20, token 32, amount u64, payload (u32 len + bytes),
//	revert_recipient 32, tx_type 1, signature_data (u32 len + bytes), from_cea 1
//
// Every field through signature_data is required. A truncated event is rejected
// rather than returned half-filled, since the zero values are not neutral:
// tx_type 0 is GAS, which routes funds to a different account than FUNDS does.
// Only from_cea is optional, defaulting to false as its absence cannot misroute.
func decodeUniversalTxEvent(data []byte, logger zerolog.Logger) (*common.UniversalTx, error) {
	offset := 8
	payload := &common.UniversalTx{}

	// Parse sender (32 bytes)
	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for sender")
	}
	sender := solana.PublicKey(data[offset : offset+32])
	// Convert sender to hex format
	senderHex, err := base58ToHex(sender.String())
	if err != nil {
		logger.Warn().Err(err).Msg("failed to convert sender to hex, using base58")
		payload.Sender = sender.String()
	} else {
		payload.Sender = senderHex
	}
	offset += 32

	// Parse recipient (20 bytes - byte20 format)
	if len(data) < offset+20 {
		return nil, fmt.Errorf("not enough data for recipient")
	}
	// Convert 20 bytes to hex string (0x + 40 hex chars)
	recipientBytes := data[offset : offset+20]
	payload.Recipient = "0x" + hex.EncodeToString(recipientBytes)
	offset += 20

	// Parse bridge_token (32 bytes)
	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for bridge_token")
	}
	bridgeToken := solana.PublicKey(data[offset : offset+32])
	payload.Token = bridgeToken.String()
	offset += 32

	// Parse bridge_amount (8 bytes)
	if len(data) < offset+8 {
		return nil, fmt.Errorf("not enough data for bridge_amount")
	}
	bridgeAmount := binary.LittleEndian.Uint64(data[offset : offset+8])
	payload.Amount = fmt.Sprintf("%d", bridgeAmount)
	offset += 8

	// Parse data field length (4 bytes)
	if len(data) < offset+4 {
		return nil, fmt.Errorf("not enough data for data field length")
	}
	dataLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Parse data field
	if len(data) < offset+int(dataLen) {
		return nil, fmt.Errorf("data field claims %d bytes, only %d available", dataLen, len(data)-offset)
	}
	if dataLen > 0 {
		dataField := data[offset : offset+int(dataLen)]
		payload.RawPayload = "0x" + hex.EncodeToString(dataField)
		offset += int(dataLen)
	}

	// Parse revert_recipient (Pubkey)
	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for revert recipient")
	}
	revertRecipient := solana.PublicKey(data[offset : offset+32])
	payload.RevertFundRecipient = revertRecipient.String()
	offset += 32

	// Parse tx_type (TxType enum)
	//
	// No default. Wire 0 is GAS, which credits the sender UEA via swap rather
	// than depositing to the recipient, and also selects fast confirmation.
	// Guessing it on a truncated event silently changes where the money goes.
	if len(data) <= offset {
		return nil, fmt.Errorf("not enough data for tx_type")
	}
	txType := data[offset]
	payload.TxType = uint(txType)
	offset++

	// Parse signature data length (4 bytes)
	if len(data) < offset+4 {
		return nil, fmt.Errorf("not enough data for signature length")
	}
	sigLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	remainingBytes := len(data) - offset
	if int(sigLen) > remainingBytes {
		return nil, fmt.Errorf("signature data claims %d bytes, only %d available", sigLen, remainingBytes)
	}

	if sigLen > 0 {
		sigData := data[offset : offset+int(sigLen)]
		payload.VerificationData = "0x" + hex.EncodeToString(sigData)
		offset += int(sigLen)
	}

	// Parse fromCEA (bool, 1 byte) - if not present, defaults to false
	if len(data) > offset {
		payload.FromCEA = data[offset] != 0
		offset++
	}

	logger.Debug().
		Str("sender", payload.Sender).
		Str("recipient", payload.Recipient).
		Str("bridge_amount", payload.Amount).
		Str("bridge_token", payload.Token).
		Str("raw_payload", payload.RawPayload).
		Str("verification_data", payload.VerificationData).
		Str("revert_recipient", payload.RevertFundRecipient).
		Uint("tx_type", payload.TxType).
		Bool("from_cea", payload.FromCEA).
		Int("total_bytes_parsed", offset).
		Msg("decoded UniversalTx event")

	return payload, nil
}
