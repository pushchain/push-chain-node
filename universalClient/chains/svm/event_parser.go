package svm

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
	"github.com/rs/zerolog"

	"github.com/near/borsh-go"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// Event type constants
const (
	EventTypeSendFunds           = "send_funds"
	EventTypeOutboundObservation = "outboundObservation"
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

// ParseEvent parses a log into a store.Event based on the event type
// eventType should be "sendFunds" or "outboundObservation"
func ParseEvent(log string, signature string, slot uint64, logIndex uint, eventType string, chainID string, logger zerolog.Logger) *store.Event {
	switch eventType {
	case EventTypeSendFunds:
		return parseSendFundsEvent(log, signature, slot, logIndex, chainID, logger)
	case EventTypeOutboundObservation:
		return parseOutboundObservationEvent(log, signature, slot, logIndex, chainID, logger)
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
		Type:              "INBOUND", // Gateway events from external chains are INBOUND
		Status:            "PENDING",
		ExpiryBlockHeight: 0, // Will be set based on confirmation type if needed
	}

	// Parse event data from this log
	parseUniversalTxEvent(event, decoded, logIndex, chainID, logger)

	return event
}

// parseOutboundObservationEvent parses an outboundObservation event
// Event structure (Borsh serialized):
// - discriminator (8 bytes)
// - txID (32 bytes - bytes32)
// - universalTxID (32 bytes - bytes32)
func parseOutboundObservationEvent(log string, signature string, slot uint64, logIndex uint, chainID string, logger zerolog.Logger) *store.Event {
	if !strings.HasPrefix(log, "Program data: ") {
		return nil
	}

	eventData := strings.TrimPrefix(log, "Program data: ")
	decoded, err := base64.StdEncoding.DecodeString(eventData)
	if err != nil {
		return nil
	}

	// Need at least: 8 bytes discriminator + 32 bytes txID + 32 bytes universalTxID = 72 bytes
	if len(decoded) < 72 {
		logger.Warn().
			Int("data_len", len(decoded)).
			Msg("data too short for outboundObservation event; need at least 72 bytes")
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

	// Skip discriminator (8 bytes)
	offset := 8

	// Extract txID (32 bytes)
	txID := "0x" + hex.EncodeToString(decoded[offset:offset+32])
	offset += 32

	// Extract universalTxID (32 bytes)
	universalTxID := "0x" + hex.EncodeToString(decoded[offset:offset+32])

	// Create OutboundEvent payload
	payload := common.OutboundEvent{
		TxID:          txID,
		UniversalTxID: universalTxID,
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
		Type:              "OUTBOUND", // Outbound observation events
		Status:            "PENDING",
		ConfirmationType:  "STANDARD", // Use STANDARD confirmation for outbound events
		ExpiryBlockHeight: 0,          // 0 means no expiry
		EventData:         payloadData,
	}

	logger.Debug().
		Str("event_id", eventID).
		Str("tx_id", txID).
		Str("universal_tx_id", universalTxID).
		Msg("parsed outboundObservation event")

	return event
}

// parseUniversalTxEvent extracts specific data from a single log event
// For TxWithFunds events, it JSON-marshals the decoded fields into event.EventData.
func parseUniversalTxEvent(event *store.Event, decoded []byte, logIndex uint, chainID string, logger zerolog.Logger) {
	// Parse the TxWithFunds event
	payload, err := decodeUniversalTxEvent(decoded, logger)
	if err != nil {
		logger.Warn().
			Err(err).
			Uint("log_index", logIndex).
			Msg("failed to decode TxWithFunds event")
		return
	}

	// Set source chain and log index
	payload.SourceChain = chainID
	payload.LogIndex = logIndex

	// Marshal and store into event.EventData
	if b, err := json.Marshal(payload); err == nil {
		event.EventData = b
	} else {
		logger.Warn().
			Err(err).
			Msg("failed to marshal universal tx payload")
	}

	// if TxType is 0 or 1, use FAST else use STANDARD
	if payload.TxType == 0 || payload.TxType == 1 {
		event.ConfirmationType = "FAST"
	} else {
		event.ConfirmationType = "STANDARD"
	}
}

// decodeUniversalTxEvent decodes a TxWithFunds event
func decodeUniversalTxEvent(data []byte, logger zerolog.Logger) (*common.UniversalTx, error) {
	if len(data) < 120 {
		logger.Warn().
			Int("data_len", len(data)).
			Msg("data might be too short for complete TxWithFunds event")
	}

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
		logger.Warn().Msg("not enough data for data field length")
		return payload, nil
	}
	dataLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Parse data field
	if len(data) < offset+int(dataLen) {
		logger.Warn().
			Uint32("expected_len", dataLen).
			Int("available", len(data)-offset).
			Msg("not enough data for data field")
		return payload, nil
	}
	if dataLen > 0 {
		dataField := data[offset : offset+int(dataLen)]
		hexStr := "0x" + hex.EncodeToString(dataField)

		// Try to decode as UniversalPayload
		up, err := decodeUniversalPayload(hexStr)
		if err != nil {
			logger.Warn().
				Str("hex_str", hexStr).
				Err(err).
				Msg("failed to decode universal payload")
		} else if up != nil {
			payload.Payload = *up
		}

		// Data is now stored in UniversalPayload, not as a separate field
		offset += int(dataLen)
	}

	// Parse revert_cfg (RevertConfig struct)
	// RevertConfig: { recipient: Pubkey, message: Vec<u8> }

	// Parse revert recipient (Pubkey)
	if len(data) < offset+32 {
		logger.Warn().Msg("not enough data for revert recipient")
		return payload, nil
	}
	revertRecipient := solana.PublicKey(data[offset : offset+32])
	payload.RevertFundRecipient = revertRecipient.String()
	offset += 32

	// Parse revert message (Vec<u8>)
	if len(data) < offset+4 {
		logger.Warn().Msg("not enough data for revert message length")
		return payload, nil
	}
	revertMsgLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	remainingForRevert := len(data) - offset
	revertLenValid := int(revertMsgLen) <= remainingForRevert
	if !revertLenValid {
		logger.Warn().
			Uint32("revert_msg_len", revertMsgLen).
			Int("available", remainingForRevert).
			Msg("revert message length invalid, skipping revert message parsing")
	}

	if revertLenValid && revertMsgLen > 0 {
		if len(data) < offset+int(revertMsgLen) {
			logger.Warn().
				Uint32("expected_len", revertMsgLen).
				Int("available", len(data)-offset).
				Msg("not enough data for revert message")
			return payload, nil
		}
		revertMsg := data[offset : offset+int(revertMsgLen)]
		payload.RevertMsg = "0x" + hex.EncodeToString(revertMsg)
		offset += int(revertMsgLen)
	}

	// Parse tx_type (TxType enum)
	if len(data) <= offset {
		logger.Warn().Msg("not enough data for tx_type, defaulting to Funds")
		payload.TxType = uint(0)
		return payload, nil
	}
	txType := data[offset]
	payload.TxType = uint(txType)
	offset++

	// Parse signature data length (4 bytes)
	if len(data) < offset+4 {
		logger.Warn().Msg("not enough data for signature length")
		return payload, nil
	}
	sigLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	remainingBytes := len(data) - offset
	if int(sigLen) > remainingBytes {
		logger.Warn().
			Uint32("expected_len", sigLen).
			Int("available", remainingBytes).
			Msg("signature data length exceeds available data, skipping")
		return payload, nil
	}

	if sigLen > 0 {
		sigData := data[offset : offset+int(sigLen)]
		payload.VerificationData = "0x" + hex.EncodeToString(sigData)
		offset += int(sigLen)
	}

	logger.Debug().
		Str("sender", payload.Sender).
		Str("recipient", payload.Recipient).
		Str("bridge_amount", payload.Amount).
		Str("bridge_token", payload.Token).
		Str("universal_payload", fmt.Sprintf("%+v", payload.Payload)).
		Str("verification_data", payload.VerificationData).
		Str("revert_recipient", payload.RevertFundRecipient).
		Str("revert_message", payload.RevertMsg).
		Uint("tx_type", payload.TxType).
		Int("total_bytes_parsed", offset).
		Msg("decoded UniversalTx event")

	return payload, nil
}

// decodeUniversalPayload takes a hex string and decodes it into UniversalPayload
// It decodes Rust Anchor/Borsh-serialized UniversalPayload bytes matching this Rust layout:
//
// #[derive(AnchorSerialize, AnchorDeserialize)]
//
//	pub struct UniversalPayload {
//	    pub to: [u8; 20],
//	    pub value: u64,
//	    pub data: Vec<u8>,
//	    pub gas_limit: u64,
//	    pub max_fee_per_gas: u64,
//	    pub max_priority_fee_per_gas: u64,
//	    pub nonce: u64,
//	    pub deadline: i64,
//	    pub v_type: u8, // enum variant index (no payload)
//	}
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

	// Mirror the exact Rust/Borsh field order & types
	type universalPayloadBorsh struct {
		To                   [20]byte
		Value                uint64
		Data                 []byte
		GasLimit             uint64
		MaxFeePerGas         uint64
		MaxPriorityFeePerGas uint64
		Nonce                uint64
		Deadline             int64
		VType                uint8
	}

	var raw universalPayloadBorsh
	if err := borsh.Deserialize(&raw, bz); err != nil {
		return nil, fmt.Errorf("borsh decode failed: %w", err)
	}

	up := &uetypes.UniversalPayload{
		To:                   "0x" + hex.EncodeToString(raw.To[:]),
		Value:                strconv.FormatUint(raw.Value, 10),
		Data:                 "0x" + hex.EncodeToString(raw.Data),
		GasLimit:             strconv.FormatUint(raw.GasLimit, 10),
		MaxFeePerGas:         strconv.FormatUint(raw.MaxFeePerGas, 10),
		MaxPriorityFeePerGas: strconv.FormatUint(raw.MaxPriorityFeePerGas, 10),
		Nonce:                strconv.FormatUint(raw.Nonce, 10),
		Deadline:             strconv.FormatInt(raw.Deadline, 10),
		VType:                uetypes.VerificationType(raw.VType), // enum index -> your type
	}

	return up, nil
}
