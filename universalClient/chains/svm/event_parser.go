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
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/mr-tron/base58"
	"github.com/rs/zerolog"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/near/borsh-go"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// Constants for event parsing
const (
	TxWithFundsDiscriminator = "2b1f1f0204ec6bff"
	TxTypeFunds              = 2 // Default fallback tx type
)

// EventParser handles parsing of SVM gateway events
type EventParser struct {
	gatewayAddr solana.PublicKey
	config      *uregistrytypes.ChainConfig
	eventTopics map[string]string // method identifier -> event discriminator
	logger      zerolog.Logger
}

// base58ToHex converts a base58 encoded string to hex format (0x...)
func (ep *EventParser) base58ToHex(base58Str string) (string, error) {
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

// NewEventParser creates a new event parser
func NewEventParser(
	gatewayAddr solana.PublicKey,
	config *uregistrytypes.ChainConfig,
	logger zerolog.Logger,
) *EventParser {
	// Build event topics from config methods
	eventTopics := make(map[string]string)
	logger.Info().
		Int("gateway_methods_count", len(config.GatewayMethods)).
		Str("gateway_address", config.GatewayAddress).
		Msg("building event topics")

	for _, method := range config.GatewayMethods {
		if method.EventIdentifier != "" {
			eventTopics[method.Identifier] = method.EventIdentifier
			logger.Info().
				Str("method", method.Name).
				Str("event_identifier", method.EventIdentifier).
				Str("method_id", method.Identifier).
				Msg("registered event topic from config")
		} else {
			logger.Warn().
				Str("method", method.Name).
				Str("method_id", method.Identifier).
				Msg("no event identifier provided in config for method")
		}
	}

	return &EventParser{
		gatewayAddr: gatewayAddr,
		config:      config,
		eventTopics: eventTopics,
		logger:      logger.With().Str("component", "svm_event_parser").Logger(),
	}
}

// ParseGatewayEvent parses a transaction into a GatewayEvent
func (ep *EventParser) ParseGatewayEvent(tx *rpc.GetTransactionResult, signature string, slot uint64) *common.GatewayEvent {
	if tx == nil || tx.Meta == nil {
		return nil
	}

	// Find matching method by event discriminator
	var eventID string
	var confirmationType string
	var found bool

	// Look for events in transaction logs
	for _, log := range tx.Meta.LogMessages {
		if !strings.HasPrefix(log, "Program data: ") {
			continue
		}

		eventData := strings.TrimPrefix(log, "Program data: ")
		decoded, err := base64.StdEncoding.DecodeString(eventData)
		if err != nil {
			continue
		}

		if len(decoded) < 8 {
			continue
		}

		discriminator := hex.EncodeToString(decoded[:8])

		if discriminator == "7f1f6cffbb134644" { // add_funds
			continue
		}

		// Find matching method by event discriminator
		for id, topic := range ep.eventTopics {
			if discriminator == topic {
				eventID = topic
				found = true
				// Find method name and confirmation type from config
				for _, method := range ep.config.GatewayMethods {
					if method.Identifier == id {
						// Map confirmation type enum to string
						if method.ConfirmationType == 2 { // CONFIRMATION_TYPE_FAST
							confirmationType = "FAST"
						} else {
							confirmationType = "STANDARD" // Default to STANDARD
						}
						break
					}
				}
				break
			}
		}

		if found {
			break
		}
	}

	// Return nil if no matching event topic found
	if !found {
		return nil
	}

	event := &common.GatewayEvent{
		ChainID:          ep.config.Chain,
		TxHash:           signature,
		BlockNumber:      slot,
		EventID:          eventID,
		ConfirmationType: confirmationType,
	}

	// Parse event data based on method
	ep.parseEventData(event, tx.Meta.LogMessages)

	return event
}

// parseEventData extracts specific data from the event based on method type.
// For TxWithFunds events, it JSON-marshals the decoded fields into event.Payload.
func (ep *EventParser) parseEventData(event *common.GatewayEvent, logs []string) {
	// Look for TxWithFunds event in Program data logs
	for i, log := range logs {
		if !strings.HasPrefix(log, "Program data: ") {
			continue
		}

		eventData := strings.TrimPrefix(log, "Program data: ")
		decoded, err := base64.StdEncoding.DecodeString(eventData)
		if err != nil {
			ep.logger.Warn().
				Err(err).
				Int("log_index", i).
				Msg("failed to decode Program data")
			continue
		}

		if len(decoded) < 8 {
			continue
		}

		discriminator := hex.EncodeToString(decoded[:8])
		if discriminator != TxWithFundsDiscriminator {
			continue
		}

		// Parse the TxWithFunds event
		payload, err := ep.decodeTxWithFundsEvent(decoded)
		if err != nil {
			ep.logger.Warn().
				Err(err).
				Int("log_index", i).
				Msg("failed to decode TxWithFunds event")
			continue
		}

		// Set source chain and log index
		payload.SourceChain = event.ChainID
		payload.LogIndex = uint(i)

		// Marshal and store into event.Payload
		if b, err := json.Marshal(payload); err == nil {
			event.Payload = b
		}

		// Only process the first valid event
		break
	}
}

// decodeTxWithFundsEvent decodes a TxWithFunds event
func (ep *EventParser) decodeTxWithFundsEvent(data []byte) (*common.TxWithFundsPayload, error) {
	if len(data) < 120 {
		ep.logger.Warn().
			Int("data_len", len(data)).
			Msg("data might be too short for complete TxWithFunds event")
	}

	offset := 8
	payload := &common.TxWithFundsPayload{
		TxType:           uint(TxTypeFunds),
		RevertMsg:        "0x",
		VerificationData: "0x",
	}

	// Parse sender (32 bytes)
	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for sender")
	}
	sender := solana.PublicKey(data[offset : offset+32])
	// Convert sender to hex format
	senderHex, err := ep.base58ToHex(sender.String())
	if err != nil {
		ep.logger.Warn().Err(err).Msg("failed to convert sender to hex, using base58")
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

	// Parse bridge_amount (8 bytes)
	if len(data) < offset+8 {
		return nil, fmt.Errorf("not enough data for bridge_amount")
	}
	bridgeAmount := binary.LittleEndian.Uint64(data[offset : offset+8])
	payload.BridgeAmount = fmt.Sprintf("%d", bridgeAmount)
	offset += 8

	// Parse gas_amount (8 bytes)
	if len(data) < offset+8 {
		return nil, fmt.Errorf("not enough data for gas_amount")
	}
	gasAmount := binary.LittleEndian.Uint64(data[offset : offset+8])
	payload.GasAmount = fmt.Sprintf("%d", gasAmount)
	offset += 8

	// Parse bridge_token (32 bytes)
	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for bridge_token")
	}
	bridgeToken := solana.PublicKey(data[offset : offset+32])
	payload.BridgeToken = bridgeToken.String()
	offset += 32

	// Parse data field length (4 bytes)
	if len(data) < offset+4 {
		ep.logger.Warn().Msg("not enough data for data field length")
		return payload, nil
	}
	dataLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Parse data field
	if len(data) < offset+int(dataLen) {
		ep.logger.Warn().
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
			ep.logger.Warn().
				Str("hex_str", hexStr).
				Err(err).
				Msg("failed to decode universal payload")
		} else if up != nil {
			payload.UniversalPayload = *up
		}

		// Data is now stored in UniversalPayload, not as a separate field
		offset += int(dataLen)
	}

	// Parse revert_cfg (RevertConfig struct)
	// RevertConfig: { recipient: Pubkey, message: Vec<u8> }

	// Parse revert recipient (Pubkey)
	if len(data) < offset+32 {
		ep.logger.Warn().Msg("not enough data for revert recipient")
		return payload, nil
	}
	revertRecipient := solana.PublicKey(data[offset : offset+32])
	payload.RevertFundRecipient = revertRecipient.String()
	offset += 32

	// Parse revert message (Vec<u8>)
	if len(data) < offset+4 {
		ep.logger.Warn().Msg("not enough data for revert message length")
		return payload, nil
	}
	revertMsgLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	remainingForRevert := len(data) - offset
	revertLenValid := int(revertMsgLen) <= remainingForRevert
	if !revertLenValid {
		ep.logger.Warn().
			Uint32("revert_msg_len", revertMsgLen).
			Int("available", remainingForRevert).
			Msg("revert message length invalid, skipping revert message parsing")
	}

	if revertLenValid && revertMsgLen > 0 {
		if len(data) < offset+int(revertMsgLen) {
			ep.logger.Warn().
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
		ep.logger.Warn().Msg("not enough data for tx_type, defaulting to Funds")
		payload.TxType = uint(TxTypeFunds)
		return payload, nil
	}
	txType := data[offset]
	payload.TxType = uint(txType)
	offset++

	// Parse signature data length (4 bytes)
	if len(data) < offset+4 {
		ep.logger.Warn().Msg("not enough data for signature length")
		return payload, nil
	}
	sigLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	remainingBytes := len(data) - offset
	if int(sigLen) > remainingBytes {
		ep.logger.Warn().
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

	ep.logger.Debug().
		Str("sender", payload.Sender).
		Str("recipient", payload.Recipient).
		Str("bridge_amount", payload.BridgeAmount).
		Str("gas_amount", payload.GasAmount).
		Str("bridge_token", payload.BridgeToken).
		Str("universal_payload", fmt.Sprintf("%+v", payload.UniversalPayload)).
		Str("verification_data", payload.VerificationData).
		Str("revert_recipient", payload.RevertFundRecipient).
		Str("revert_message", payload.RevertMsg).
		Uint("tx_type", payload.TxType).
		Int("total_bytes_parsed", offset).
		Msg("decoded TxWithFunds event")

	return payload, nil
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
	up, err := decodeUniversalPayloadBorsh(bz)
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

// decodeUniversalPayloadBorsh decodes Rust Anchor/Borsh-serialized UniversalPayload bytes
// into your uetypes.UniversalPayload. It matches this Rust layout:
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
func decodeUniversalPayloadBorsh(bz []byte) (*uetypes.UniversalPayload, error) {
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

// GetEventTopics returns the configured event topics
func (ep *EventParser) GetEventTopics() []string {
	topics := make([]string, 0, len(ep.eventTopics))
	for _, topic := range ep.eventTopics {
		topics = append(topics, topic)
	}
	return topics
}
