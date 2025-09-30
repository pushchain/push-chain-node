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
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Event identifier constants
const (
	SendFundsEventID     = "0x313800e2e529b7d45906548dd908bb537772d390b660787b2a929ddf1facf6e4"
	AddFundsEventID      = "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	SendTxWithGasEventID = "0xfc9b0ad90b92705792c6281e89beaf1d977aa0d66afccefba2f5b207787c9aab"
)

// EventParser handles parsing of EVM gateway events
type EventParser struct {
	gatewayAddr ethcommon.Address
	config      *uregistrytypes.ChainConfig
	eventTopics map[string]uregistrytypes.ConfirmationType // EventIdentifier (hex) -> ConfirmationType
	logger      zerolog.Logger
}

// NewEventParser creates a new event parser
func NewEventParser(
	gatewayAddr ethcommon.Address,
	config *uregistrytypes.ChainConfig,
	logger zerolog.Logger,
) *EventParser {
	// Build event topics from config methods
	// Map: EventIdentifier (hex string) -> ConfirmationType
	eventTopics := make(map[string]uregistrytypes.ConfirmationType)
	logger.Info().
		Int("gateway_methods_count", len(config.GatewayMethods)).
		Str("gateway_address", config.GatewayAddress).
		Msg("building event topics")

	for _, method := range config.GatewayMethods {
		if method.EventIdentifier != "" {
			eventTopics[method.EventIdentifier] = method.ConfirmationType
			logger.Info().
				Str("event_identifier", method.EventIdentifier).
				Int("confirmation_type", int(method.ConfirmationType)).
				Msg("registered event topic from config")
		} else {
			logger.Warn().
				Str("event_identifier", "").
				Msg("no event identifier provided in config for method")
		}
	}

	// Debug log the complete cache
	logger.Debug().
		Interface("event_topics_cache", eventTopics).
		Int("total_events", len(eventTopics)).
		Msg("event topics cache built")

	return &EventParser{
		gatewayAddr: gatewayAddr,
		config:      config,
		eventTopics: eventTopics,
		logger:      logger.With().Str("component", "evm_event_parser").Logger(),
	}
}

// ParseGatewayEvent parses a log into a GatewayEvent
func (ep *EventParser) ParseGatewayEvent(log *types.Log) *common.GatewayEvent {
	if len(log.Topics) == 0 {
		return nil
	}

	// Find matching method by event topic using direct lookup
	eventID := log.Topics[0]
	eventIDHex := eventID.Hex()

	// Direct lookup using EventIdentifier
	confType, found := ep.eventTopics[eventIDHex]
	if !found {
		return nil
	}

	// Skip add_funds events
	if eventIDHex == AddFundsEventID {
		return nil
	}

	// Map confirmation type enum to string
	var confirmationType string
	if confType == 2 { // CONFIRMATION_TYPE_FAST
		confirmationType = "FAST"
	} else {
		confirmationType = "STANDARD" // Default to STANDARD
	}

	event := &common.GatewayEvent{
		ChainID:          ep.config.Chain,
		TxHash:           log.TxHash.Hex(),
		BlockNumber:      log.BlockNumber,
		EventID:          eventID.Hex(),
		ConfirmationType: confirmationType,
	}
	ep.logger.Debug().
		Str("event_id", eventID.Hex()).
		Str("tx_hash", log.TxHash.Hex()).
		Msg("processing gateway event")

	// Route to appropriate parser based on event ID
	switch eventIDHex {
	case SendTxWithGasEventID:
		// Only use sendTxWithGas parser for this specific event
		ep.parseSendTxWithGasEvent(event, log)
	case SendFundsEventID:
		// Only use sendFunds parser for this specific event
		ep.parseSendFundsEvent(event, log)
	default:
		// Unknown event - return nil (no fallback)
		ep.logger.Warn().
			Str("event_id", eventIDHex).
			Msg("unknown event identifier - no parser available")
		return nil
	}

	// Validate parsing succeeded
	if event.Payload == nil || len(event.Payload) <= 2 {
		ep.logger.Warn().
			Str("event_id", eventIDHex).
			Msg("parser returned empty payload")
		return nil
	}

	return event
}

// parseSendTxWithGasEvent parses the sendTxWithGas event
// Event structure: sendTxWithGas(address indexed sender, address indexed recipient, address bridgeToken, uint256 bridgeAmount, uint txType, bytes payload)
// Data layout: word[0]=recipient offset, word[1]=bridgeAmount, word[2]=bridgeToken offset, word[3]=txType, word[4]=payload offset
func (ep *EventParser) parseSendTxWithGasEvent(event *common.GatewayEvent, log *types.Log) {
	payload := common.TxWithFundsPayload{
		SourceChain: event.ChainID,
		LogIndex:    log.Index,
	}

	// Helper to get 32-byte words from data
	word := func(i int) []byte {
		start := i * 32
		end := start + 32
		if end > len(log.Data) {
			return nil
		}
		return log.Data[start:end]
	}

	// Parse sender from topic[1] (indexed)
	if len(log.Topics) >= 2 {
		payload.Sender = ethcommon.BytesToAddress(log.Topics[1].Bytes()).Hex()
	}

	// Parse bridgeAmount (direct value at word[1])
	if w := word(1); w != nil {
		payload.BridgeAmount = new(big.Int).SetBytes(w).String()
	}

	// Parse bridgeToken from dynamic offset (word[2])
	if w := word(2); w != nil {
		bridgeTokenOffset := new(big.Int).SetBytes(w).Uint64()
		bridgeTokenIdx := int(bridgeTokenOffset / 32)
		if btw := word(bridgeTokenIdx); btw != nil {
			payload.BridgeToken = ethcommon.BytesToAddress(btw).Hex()
		}
	}

	// Parse txType (direct value at word[3])
	if w := word(3); w != nil {
		payload.TxType = uint(new(big.Int).SetBytes(w).Uint64())
	}

	// Parse recipient from dynamic offset (word[0] points to struct, address at +2)
	if w := word(0); w != nil {
		recipientOffset := new(big.Int).SetBytes(w).Uint64()
		recipientAddrIdx := int(recipientOffset/32) + 2
		if rw := word(recipientAddrIdx); rw != nil {
			payload.Recipient = ethcommon.BytesToAddress(rw).Hex()
		}
	}

	// Parse payload from dynamic offset (word[4])
	if w := word(4); w != nil {
		payloadOffset := new(big.Int).SetBytes(w).Uint64()
		lengthStart := int(payloadOffset)
		lengthEnd := lengthStart + 32
		if lengthEnd <= len(log.Data) {
			dataLength := new(big.Int).SetBytes(log.Data[lengthStart:lengthEnd]).Uint64()
			dataStart := lengthEnd
			dataEnd := dataStart + int(dataLength)

			if dataEnd <= len(log.Data) {
				payloadData := log.Data[dataStart:dataEnd]
				hexStr := "0x" + hex.EncodeToString(payloadData)

				up, err := decodeUniversalPayload(hexStr)
				if err != nil {
					payload.VerificationData = hexStr
				} else if up != nil {
					payload.UniversalPayload = *up
				}
			} else {
				ep.logger.Warn().
					Int("dataEnd", dataEnd).
					Int("dataLength", len(log.Data)).
					Msg("payload data extends beyond log.Data")
			}
		}
	}

	// Marshal and store
	b, err := json.Marshal(payload)
	if err != nil {
		ep.logger.Error().Err(err).Msg("failed to marshal sendTxWithGas payload")
		event.Payload = []byte(`{"sourceChain":"` + event.ChainID + `","txHash":"` + log.TxHash.Hex() + `"}`)
	} else {
		event.Payload = b
	}
}

// parseSendFundsEvent extracts specific data from the sendFunds event.
// For sendFunds(sender, recipient, bridgeToken, bridgeAmount, payload, revertCFG, txType, signatureData?)
// it JSON-marshals the decoded fields into event.Payload.
//
// Encoding layout in log.Data (non-indexed):
// [0] address  bridgeToken              (32 bytes; right-most 20 significant)
// [1] uint256  bridgeAmount             (32 bytes)
// [2] offset   payload (bytes)          (32 bytes; offset from start of log.Data)
// [3] offset   revertCFG (tuple)        (32 bytes; offset from start of log.Data)
// [4] uint     txType                   (32 bytes)
// [5] offset   signatureData (bytes)   (32 bytes; offset from start of log.Data)   <-- OPTIONAL, dynamic bytes
//
// tuple RevertSettings head (at revertOffset):
// [0] address fundRecipient
// [1] offset  revertMsg (bytes)         (offset from start of the tuple)
// ... tail for revertMsg follows
func (ep *EventParser) parseSendFundsEvent(event *common.GatewayEvent, log *types.Log) {
	if len(log.Topics) < 3 {
		// Not enough indexed fields; nothing to do.
		return
	}

	payload := common.TxWithFundsPayload{
		SourceChain: event.ChainID,
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
		event.Payload = b
		return
	}

	// bridgeToken (address in the right-most 20 bytes of the first word)
	if w := word(0); w != nil {
		payload.BridgeToken = ethcommon.BytesToAddress(w[12:32]).Hex()
	}

	// bridgeAmount (uint256)
	if w := word(1); w != nil {
		amt := new(big.Int).SetBytes(w)
		payload.BridgeAmount = amt.String()
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
				ep.logger.Warn().
					Str("hex_str", hexStr).
					Err(err).
					Msg("failed to decode universal payload")
			} else if up != nil {
				payload.UniversalPayload = *up
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

	// Marshal and store into event.Payload
	if b, err := json.Marshal(payload); err == nil {
		event.Payload = b
	}
}

// GetEventTopics returns the configured event topics
func (ep *EventParser) GetEventTopics() []ethcommon.Hash {
	topics := make([]ethcommon.Hash, 0, len(ep.eventTopics))
	for eventID := range ep.eventTopics {
		topics = append(topics, ethcommon.HexToHash(eventID))
	}
	return topics
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
