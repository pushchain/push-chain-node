package evm

import (
	"encoding/hex"
	"encoding/json"
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// EventParser handles parsing of EVM gateway events
type EventParser struct {
	gatewayAddr ethcommon.Address
	config      *uregistrytypes.ChainConfig
	eventTopics map[string]ethcommon.Hash
	logger      zerolog.Logger
}

// NewEventParser creates a new event parser
func NewEventParser(
	gatewayAddr ethcommon.Address,
	config *uregistrytypes.ChainConfig,
	logger zerolog.Logger,
) *EventParser {
	// Build event topics from config methods
	eventTopics := make(map[string]ethcommon.Hash)
	logger.Info().
		Int("gateway_methods_count", len(config.GatewayMethods)).
		Str("gateway_address", config.GatewayAddress).
		Msg("building event topics")

	for _, method := range config.GatewayMethods {
		if method.EventIdentifier != "" {
			eventTopics[method.Identifier] = ethcommon.HexToHash(method.EventIdentifier)
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
		logger:      logger.With().Str("component", "evm_event_parser").Logger(),
	}
}

// ParseGatewayEvent parses a log into a GatewayEvent
func (ep *EventParser) ParseGatewayEvent(log *types.Log) *common.GatewayEvent {
	if len(log.Topics) == 0 {
		return nil
	}

	// Find matching method by event topic
	var eventID ethcommon.Hash
	var methodName, confirmationType string
	for id, topic := range ep.eventTopics {
		if log.Topics[0] == topic {
			eventID = topic
			// Find method name and confirmation type from config
			for _, method := range ep.config.GatewayMethods {
				if method.Identifier == id {
					methodName = method.Name
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

	if eventID.String() == "" {
		return nil
	}

	event := &common.GatewayEvent{
		ChainID:          ep.config.Chain,
		TxHash:           log.TxHash.Hex(),
		BlockNumber:      log.BlockNumber,
		Method:           methodName,
		EventID:          eventID.Hex(),
		ConfirmationType: confirmationType,
	}
	// Parse event data based on method
	ep.parseEventData(event, log)

	return event
}

// parseEventData extracts specific data from the event based on method type.
// For TxWithFunds(sender, recipient, bridgeToken, bridgeAmount, payload, revertCFG, txType)
// it JSON-marshals the decoded fields into event.Payload.
//
// Encoding layout in log.Data (non-indexed):
// [0] address  bridgeToken              (32 bytes; right-most 20 significant)
// [1] uint256  bridgeAmount             (32 bytes)
// [2] offset   payload (bytes)          (32 bytes; offset from start of log.Data)
// [3] offset   revertCFG (tuple)        (32 bytes; offset from start of log.Data)
// [4] uint     txType                   (32 bytes)
//
// tuple RevertSettings head (at revertOffset):
// [0] address fundRecipient
// [1] offset  revertMsg (bytes)         (offset from start of the tuple)
// ... tail for revertMsg follows
func (ep *EventParser) parseEventData(event *common.GatewayEvent, log *types.Log) {
	type txWithFundsPayload struct {
		Sender       string `json:"sender"`
		Recipient    string `json:"recipient"`
		BridgeToken  string `json:"bridgeToken"`
		BridgeAmount string `json:"bridgeAmount"` // uint256 as decimal string
		Data         string `json:"data"`         // hex-encoded bytes (0x…)
		RevertCFG    string `json:"revertCFG"`    // raw hex tail starting at tuple offset (0x…)
		// Decoded revert tuple (new; optional)
		RevertFundRecipient string `json:"revertFundRecipient,omitempty"`
		RevertMsg           string `json:"revertMsg,omitempty"` // hex-encoded bytes (0x…)
		TxType              string `json:"txType"`              // enum backing uint as decimal string
	}

	if len(log.Topics) < 3 {
		// Not enough indexed fields; nothing to do.
		return
	}

	payload := txWithFundsPayload{
		Sender:    ethcommon.BytesToAddress(log.Topics[1].Bytes()).Hex(),
		Recipient: ethcommon.BytesToAddress(log.Topics[2].Bytes()).Hex(),
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

	// Need at least 5 words for the head.
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
		payload.TxType = txType.String()
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
			payload.Data = hexStr
		}
	}

	// --- revertCFG (tuple(address fundRecipient, bytes revertMsg)) ---
	// Store the raw ABI tail (from tuple start) for backwards compatibility.
	if revertOffset < uint64(len(log.Data)) {
		payload.RevertCFG = "0x" + hex.EncodeToString(log.Data[revertOffset:])
	}

	// Decode the tuple if we have enough room for its head (2 words).
	// NOTE: Offsets inside a tuple are RELATIVE TO THE START OF THE TUPLE.
	if revertOffset >= uint64(32*5) && revertOffset+64 <= uint64(len(log.Data)) {
		tupleBase := revertOffset

		// fundRecipient @ tupleBase + 0
		if addr, ok := readAddressAt(tupleBase); ok {
			payload.RevertFundRecipient = addr
		}

		// revertMsg offset word @ tupleBase + 32
		// It's an offset from tupleBase.
		offWordStart := tupleBase + 32
		offWordEnd := offWordStart + 32
		revertMsgRelOff := new(big.Int).SetBytes(log.Data[offWordStart:offWordEnd]).Uint64()

		revertMsgAbsOff := tupleBase + revertMsgRelOff
		if hexStr, ok := readBytesAt(revertMsgAbsOff); ok {
			payload.RevertMsg = hexStr
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
	for _, topic := range ep.eventTopics {
		topics = append(topics, topic)
	}
	return topics
}

// HasEvents checks if any event topics are configured
func (ep *EventParser) HasEvents() bool {
	return len(ep.eventTopics) > 0
}
