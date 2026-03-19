package evm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Event type constants matching gateway method names in chain config.
const (
	EventTypeSendFunds          = "sendFunds"
	EventTypeExecuteUniversalTx = "executeUniversalTx"
	EventTypeRevertUniversalTx  = "revertUniversalTx"
)

// Vault event type constants matching vault method names in chain config.
const (
	EventTypeFinalizeUniversalTx = "finalizeUniversalTx"
)

// ParseEvent parses a log into a store.Event based on the event type.
// eventType should be one of: sendFunds, executeUniversalTx, revertUniversalTx.
func ParseEvent(log *types.Log, eventType string, chainID string, logger zerolog.Logger) *store.Event {
	if len(log.Topics) == 0 {
		return nil
	}

	switch eventType {
	case EventTypeSendFunds:
		return parseSendFundsEvent(log, chainID, logger)
	case EventTypeExecuteUniversalTx, EventTypeRevertUniversalTx, EventTypeFinalizeUniversalTx:
		// All share the same topic layout: Topics[1]=txID, Topics[2]=universalTxID.
		return parseOutboundObservationEvent(log, chainID, logger)
	default:
		logger.Debug().
			Str("event_type", eventType).
			Str("tx_hash", log.TxHash.Hex()).
			Msg("unknown event type, skipping")
		return nil
	}
}

// parseSendFundsEvent parses a sendFunds event as UniversalTx
func parseSendFundsEvent(log *types.Log, chainID string, logger zerolog.Logger) *store.Event {
	if len(log.Topics) < 3 {
		logger.Warn().
			Msg("not enough indexed fields; nothing to do")
		return nil
	}

	// Create EventID in format: TxHash:LogIndex
	eventID := fmt.Sprintf("%s:%d", log.TxHash.Hex(), log.Index)

	logger.Debug().
		Str("event_id", eventID).
		Str("tx_hash", log.TxHash.Hex()).
		Uint("log_index", log.Index).
		Msg("processing sendFunds event")

	// Create store.Event
	event := &store.Event{
		EventID:           eventID,
		BlockHeight:       log.BlockNumber,
		Type:              store.EventTypeInbound, // Gateway events from external chains are INBOUND
		Status:            "PENDING",
		ExpiryBlockHeight: 0, // 0 means no expiry
	}

	// Parse universal tx event data
	parseUniversalTxEvent(event, log, chainID, logger)

	return event
}

// parseOutboundObservationEvent parses an outboundObservation event
// Event structure:
// - Topics[0]: event signature hash
// - Topics[1]: txID (bytes32)
// - Topics[2]: universalTxID (bytes32)
func parseOutboundObservationEvent(log *types.Log, chainID string, logger zerolog.Logger) *store.Event {
	if len(log.Topics) < 3 {
		logger.Warn().
			Str("tx_hash", log.TxHash.Hex()).
			Int("topic_count", len(log.Topics)).
			Msg("not enough indexed fields for outboundObservation event; need at least 3 topics")
		return nil
	}

	// Create EventID in format: TxHash:LogIndex
	eventID := fmt.Sprintf("%s:%d", log.TxHash.Hex(), log.Index)

	logger.Debug().
		Str("event_id", eventID).
		Str("tx_hash", log.TxHash.Hex()).
		Uint("log_index", log.Index).
		Msg("processing outboundObservation event")

	// Extract txID from Topics[1] (bytes32)
	txID := "0x" + hex.EncodeToString(log.Topics[1].Bytes())

	// Extract universalTxID from Topics[2] (bytes32)
	universalTxID := "0x" + hex.EncodeToString(log.Topics[2].Bytes())

	// Create OutboundEvent payload
	payload := common.OutboundEvent{
		TxID:          txID,
		UniversalTxID: universalTxID,
	}

	// Marshal payload to JSON
	eventData, err := json.Marshal(payload)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("tx_hash", log.TxHash.Hex()).
			Msg("failed to marshal outbound event payload")
		return nil
	}

	// Create store.Event
	event := &store.Event{
		EventID:           eventID,
		BlockHeight:       log.BlockNumber,
		Type:              store.EventTypeOutbound, // Outbound observation events
		Status:            "PENDING",
		ConfirmationType:  "STANDARD", // Use STANDARD confirmation for outbound events
		ExpiryBlockHeight: 0,          // 0 means no expiry
		EventData:         eventData,
	}

	logger.Debug().
		Str("event_id", eventID).
		Str("tx_id", txID).
		Str("universal_tx_id", universalTxID).
		Msg("parsed outboundObservation event")

	return event
}

// parseUniversalTxEvent parses a UniversalTx event from log data.
// Detects V1 (legacy) vs V2 (upgraded) format based on the payload offset:
// V2 has 7 static words (head=224), V1 has 6 (head=192).
func parseUniversalTxEvent(event *store.Event, log *types.Log, chainID string, logger zerolog.Logger) {
	if len(log.Topics) < 3 {
		logger.Warn().Msg("not enough indexed fields; nothing to do")
		return
	}

	payload := common.UniversalTx{
		SourceChain: chainID,
		Sender:      ethcommon.BytesToAddress(log.Topics[1].Bytes()).Hex(),
		Recipient:   ethcommon.BytesToAddress(log.Topics[2].Bytes()).Hex(),
		LogIndex:    log.Index,
	}

	if len(log.Data) < 32*5 {
		b, _ := json.Marshal(payload)
		event.EventData = b
		return
	}

	// Parse common static fields: token (Word 0), amount (Word 1)
	payload.Token = ethcommon.BytesToAddress(log.Data[0*32+12 : 0*32+32]).Hex()
	payload.Amount = new(big.Int).SetBytes(log.Data[1*32 : 2*32]).String()

	// Detect format via payload offset (Word 2): >= 224 means V2 (7-word head)
	dataOffset := new(big.Int).SetBytes(log.Data[2*32 : 3*32]).Uint64()
	if dataOffset >= uint64(32*7) {
		parseUniversalTxV2(event, log, dataOffset, &payload, logger)
	} else {
		parseUniversalTxV1Legacy(event, log, dataOffset, &payload, logger)
	}
}

// readDynamicBytes decodes ABI-encoded dynamic bytes at the given absolute offset in data.
func readDynamicBytes(data []byte, absOff uint64) (string, bool) {
	if absOff+32 > uint64(len(data)) {
		return "", false
	}
	byteLen := new(big.Int).SetBytes(data[absOff : absOff+32]).Uint64()
	dataStart := absOff + 32
	dataEnd := dataStart + byteLen
	if dataEnd > uint64(len(data)) {
		return "", false
	}
	return "0x" + hex.EncodeToString(data[dataStart:dataEnd]), true
}

// readWord returns the i-th 32-byte word from data, or nil if out of bounds.
func readWord(data []byte, i int) []byte {
	start := i * 32
	end := start + 32
	if start < 0 || end > len(data) {
		return nil
	}
	return data[start:end]
}

// decodePayload decodes the universal payload bytes at the given offset into the payload struct.
func decodePayload(data []byte, dataOffset uint64, payload *common.UniversalTx, logger zerolog.Logger) {
	if dataOffset < uint64(32*5) {
		return
	}
	hexStr, ok := readDynamicBytes(data, dataOffset)
	if !ok {
		return
	}
	up, err := decodeUniversalPayload(hexStr)
	if err != nil {
		logger.Warn().Str("hex_str", hexStr).Err(err).Msg("failed to decode universal payload")
	} else if up != nil {
		payload.Payload = *up
	}
}

// decodeSignatureData decodes the signature/verification data from a word that contains
// either a dynamic offset or fixed bytes32.
func decodeSignatureData(data []byte, w []byte, minOffset uint64) string {
	offset := new(big.Int).SetBytes(w).Uint64()
	if offset >= minOffset && offset < uint64(len(data)) {
		if hexStr, ok := readDynamicBytes(data, offset); ok {
			return hexStr
		}
	}
	// Fallback: treat as fixed bytes32
	return "0x" + hex.EncodeToString(w)
}

// finalizeEvent marshals the payload and sets confirmation type on the event.
func finalizeEvent(event *store.Event, payload *common.UniversalTx, logger zerolog.Logger) {
	if b, err := json.Marshal(payload); err == nil {
		event.EventData = b
	} else {
		logger.Warn().Err(err).Msg("failed to marshal universal tx payload")
	}

	if payload.TxType == 0 || payload.TxType == 1 {
		event.ConfirmationType = "FAST"
	} else {
		event.ConfirmationType = "STANDARD"
	}
}

/*
UniversalTx Event (V2 - upgraded chains):
  - sender (address, indexed)
  - recipient (address, indexed)
  - token (address)             — Word 0
  - amount (uint256)            — Word 1
  - payload (bytes)             — Word 2 (offset)
  - revertRecipient (address)   — Word 3
  - txType (TX_TYPE)            — Word 4
  - signatureData (bytes)       — Word 5 (offset)
  - fromCEA (bool)              — Word 6
*/
func parseUniversalTxV2(event *store.Event, log *types.Log, dataOffset uint64, payload *common.UniversalTx, logger zerolog.Logger) {
	data := log.Data

	decodePayload(data, dataOffset, payload, logger)

	// revertRecipient (plain address at Word 3)
	if w := readWord(data, 3); w != nil {
		payload.RevertFundRecipient = ethcommon.BytesToAddress(w[12:32]).Hex()
	}

	// txType (Word 4)
	if w := readWord(data, 4); w != nil {
		payload.TxType = uint(new(big.Int).SetBytes(w).Uint64())
	}

	// signatureData (Word 5 offset)
	if w := readWord(data, 5); w != nil {
		payload.VerificationData = decodeSignatureData(data, w, uint64(32*7))
	}

	// fromCEA (Word 6)
	if w := readWord(data, 6); w != nil {
		payload.FromCEA = new(big.Int).SetBytes(w).Uint64() != 0
	}

	finalizeEvent(event, payload, logger)
}

/*
UniversalTx Event (V1 - legacy, to be removed):
  - sender (address, indexed)
  - recipient (address, indexed)
  - token (address)                — Word 0
  - amount (uint256)               — Word 1
  - payload (bytes)                — Word 2 (offset)
  - revertInstructions (tuple)     — Word 3 (offset)
  - revertRecipient (address)
  - revertMsg (bytes)
  - txType (uint)                  — Word 4
  - signatureData (bytes)          — Word 5 (offset)
*/
func parseUniversalTxV1Legacy(event *store.Event, log *types.Log, dataOffset uint64, payload *common.UniversalTx, logger zerolog.Logger) {
	data := log.Data

	decodePayload(data, dataOffset, payload, logger)

	// revertInstructions tuple offset (Word 3)
	revertOffset := new(big.Int).SetBytes(data[3*32 : 4*32]).Uint64()

	// txType (Word 4)
	if w := readWord(data, 4); w != nil {
		payload.TxType = uint(new(big.Int).SetBytes(w).Uint64())
	}

	// Decode revert recipient from tuple head
	if revertOffset >= uint64(32*5) && revertOffset+64 <= uint64(len(data)) {
		payload.RevertFundRecipient = ethcommon.BytesToAddress(data[revertOffset+12 : revertOffset+32]).Hex()
	}

	// signatureData (Word 5 offset)
	if w := readWord(data, 5); w != nil {
		payload.VerificationData = decodeSignatureData(data, w, uint64(32*6))
	}

	finalizeEvent(event, payload, logger)
}

// decodeUniversalPayload takes a hex string and decodes it into UniversalPayload
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

	// The data starts with an offset to where the actual tuple data begins
	if len(bz) < 32 {
		return nil, fmt.Errorf("insufficient data length: got %d, need at least 32", len(bz))
	}

	// Read the offset (first 32 bytes)
	offset := new(big.Int).SetBytes(bz[:32]).Uint64()

	// The actual tuple data starts at the offset
	if int(offset) >= len(bz) {
		return nil, fmt.Errorf("offset %d exceeds data length %d", offset, len(bz))
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
	decoded, err := args.Unpack(bz)
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
