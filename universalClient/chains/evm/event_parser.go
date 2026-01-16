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

// Event type constants
const (
	EventTypeSendFunds           = "sendFunds"
	EventTypeOutboundObservation = "outboundObservation"
)

// ParseEvent parses a log into a store.Event based on the event type
// eventType should be "sendFunds" or "outboundObservation"
func ParseEvent(log *types.Log, eventType string, chainID string, logger zerolog.Logger) *store.Event {
	if len(log.Topics) == 0 {
		return nil
	}

	switch eventType {
	case EventTypeSendFunds:
		return parseSendFundsEvent(log, chainID, logger)
	case EventTypeOutboundObservation:
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
		Type:              "INBOUND", // Gateway events from external chains are INBOUND
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
		Type:              "OUTBOUND", // Outbound observation events
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

/*
UniversalTx Event:
 1. sender (address)
 2. recipient (address)
 3. token (address)
 4. amount (uint256)
 5. payload (bytes)
 6. revertInstructions (tuple)
    6.1. revertRecipient (address)
    6.2. revertMsg (bytes)
 7. txType (uint)
 8. signatureData (bytes)
*/
func parseUniversalTxEvent(event *store.Event, log *types.Log, chainID string, logger zerolog.Logger) {
	if len(log.Topics) < 3 {
		logger.Warn().
			Msg("not enough indexed fields; nothing to do")
		return
	}

	payload := common.UniversalTx{
		SourceChain: chainID,
		Sender:      ethcommon.BytesToAddress(log.Topics[1].Bytes()).Hex(),
		Recipient:   ethcommon.BytesToAddress(log.Topics[2].Bytes()).Hex(),
		LogIndex:    log.Index,
	}

	// Helper: fetch the i-th 32-byte word from log.Data.
	word := func(i int) []byte {
		start := i * 32
		end := start + 32
		if start < 0 || end > len(log.Data) {
			return nil
		}
		return log.Data[start:end]
	}

	// Need at least 5 words for the static head we rely on.
	if len(log.Data) < 32*5 {
		b, _ := json.Marshal(payload)
		event.EventData = b
		return
	}

	// bridgeToken (address in the right-most 20 bytes of the first word)
	if w := word(0); w != nil {
		payload.Token = ethcommon.BytesToAddress(w[12:32]).Hex()
	}

	// bridgeAmount (uint256)
	if w := word(1); w != nil {
		amt := new(big.Int).SetBytes(w)
		payload.Amount = amt.String()
	}

	// dynamic offsets (relative to start of log.Data)
	var dataOffset, revertOffset uint64
	if w := word(2); w != nil {
		dataOffset = new(big.Int).SetBytes(w).Uint64()
	}
	if w := word(3); w != nil {
		revertOffset = new(big.Int).SetBytes(w).Uint64()
	}

	// txType (enum -> padded uint)
	if w := word(4); w != nil {
		txType := new(big.Int).SetBytes(w)
		payload.TxType = uint(txType.Uint64())
	}

	// Decode dynamic bytes at absolute offset in log.Data:
	readBytesAt := func(absOff uint64) (hexStr string, ok bool) {
		if absOff+32 > uint64(len(log.Data)) {
			return "", false
		}
		lstart := int(absOff)
		lend := lstart + 32
		byteLen := new(big.Int).SetBytes(log.Data[lstart:lend]).Uint64()

		dataStart := lend
		dataEnd := dataStart + int(byteLen)
		if dataEnd > len(log.Data) {
			return "", false
		}
		return "0x" + hex.EncodeToString(log.Data[dataStart:dataEnd]), true
	}

	// Decode address at absolute offset in log.Data:
	readAddressAt := func(absOff uint64) (string, bool) {
		if absOff+32 > uint64(len(log.Data)) {
			return "", false
		}
		w := log.Data[absOff : absOff+32]
		return ethcommon.BytesToAddress(w[12:32]).Hex(), true
	}

	// --- payload (bytes) ---
	// Offsets for dynamic fields in the head will be >= head size (5*32)
	if dataOffset >= uint64(32*5) {
		if hexStr, ok := readBytesAt(dataOffset); ok {
			up, err := decodeUniversalPayload(hexStr)
			if err != nil {
				logger.Warn().
					Str("hex_str", hexStr).
					Err(err).
					Msg("failed to decode universal payload")
			} else if up != nil {
				payload.Payload = *up
			}
		}
	}

	// --- revertCFG (tuple(address fundRecipient, bytes revertMsg)) ---
	// Decode the tuple if we have enough room for its head (2 words).
	// NOTE: Offsets inside a tuple are RELATIVE TO THE START OF THE TUPLE.
	if revertOffset >= uint64(32*5) && revertOffset+64 <= uint64(len(log.Data)) {
		tupleBase := revertOffset

		// fundRecipient @ tupleBase + 0
		if addr, ok := readAddressAt(tupleBase); ok {
			payload.RevertFundRecipient = addr
		}

		// revertMsg offset word @ tupleBase + 32 (relative to tupleBase)
		offWordStart := tupleBase + 32
		offWordEnd := offWordStart + 32
		revertMsgRelOff := new(big.Int).SetBytes(log.Data[offWordStart:offWordEnd]).Uint64()

		revertMsgAbsOff := tupleBase + revertMsgRelOff
		if hexStr, ok := readBytesAt(revertMsgAbsOff); ok {
			payload.RevertMsg = hexStr
		}
	}

	// --- signatureData (bytes) ---
	// Check if we have a 6th word that contains an offset to dynamic bytes
	if len(log.Data) >= 32*6 {
		if w := word(5); w != nil {
			// Check if this is an offset (dynamic bytes) or fixed bytes32
			offset := new(big.Int).SetBytes(w).Uint64()

			// If offset is reasonable (points to data after the static head), treat as dynamic bytes
			if offset >= uint64(32*6) && offset < uint64(len(log.Data)) {
				if hexStr, ok := readBytesAt(offset); ok {
					payload.VerificationData = hexStr
				}
			} else {
				// Fallback: treat as fixed bytes32 (for backward compatibility)
				payload.VerificationData = "0x" + hex.EncodeToString(w)
			}
		}
	}

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
