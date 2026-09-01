package evm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

// Event type constants matching gateway method names in chain config.
const (
	EventTypeSendFunds         = "sendFunds"
	EventTypeRevertUniversalTx = "revertUniversalTx"
)

// Vault event type constants matching vault method names in chain config.
const (
	EventTypeFinalizeUniversalTx = "finalizeUniversalTx"
	EventTypeFundsRescued        = "fundsRescued"
)

// ParseEvent parses a log into a store.Event based on the event type.
// eventType should be one of: sendFunds, revertUniversalTx, finalizeUniversalTx, fundsRescued.
//
// A panic in the decoders is contained here rather than allowed to unwind. Log
// data is supplied by an RPC and the listener runs on a background goroutine, so
// an unrecovered panic would take down every chain and the TSS node with it. A
// log we cannot decode is skipped like any other undecodable one.
func ParseEvent(log *types.Log, eventType string, chainID string, logger zerolog.Logger) (event *store.Event) {
	defer func() {
		if r := recover(); r != nil {
			event = nil
			logger.Error().
				Interface("panic", r).
				Str("event_type", eventType).
				Str("tx_hash", log.TxHash.Hex()).
				Uint("log_index", log.Index).
				Msg("panic while decoding log; skipping it")
		}
	}()
	return parseEvent(log, eventType, chainID, logger)
}

func parseEvent(log *types.Log, eventType string, chainID string, logger zerolog.Logger) *store.Event {
	if len(log.Topics) == 0 {
		return nil
	}

	switch eventType {
	case EventTypeSendFunds:
		return parseSendFundsEvent(log, chainID, logger)
	case EventTypeRevertUniversalTx, EventTypeFinalizeUniversalTx, EventTypeFundsRescued:
		// All share the same topic layout: Topics[1]=txID, Topics[2]=universalTxID.
		return parseOutboundObservationEvent(log, eventType, logger)
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
		Status:            store.StatusPending,
		ExpiryBlockHeight: 0, // 0 means no expiry
	}

	// Parse universal tx event data. A malformed event is dropped rather than
	// stored half-decoded: the zero values it would carry are not neutral.
	if err := parseUniversalTxEvent(event, log, chainID, logger); err != nil {
		logger.Warn().
			Err(err).
			Str("event_id", eventID).
			Msg("discarding malformed UniversalTx event")
		return nil
	}

	return event
}

// parseOutboundObservationEvent parses an outboundObservation event
// Event structure:
//   - Topics[0]: event signature hash
//   - Topics[1]: txID (bytes32)
//   - Topics[2]: universalTxID (bytes32)
//   - Topics[3]: UniversalTxFinalized only — wrapperAddress (address(0) for PRC20).
//     Other observation events put token/recipient there, so it's ignored for them.
func parseOutboundObservationEvent(log *types.Log, eventType string, logger zerolog.Logger) *store.Event {
	// All observation events carry 3 indexed params (sig + subTxId + universalTxId
	// + a 3rd: wrapperAddress on finalize, token/recipient elsewhere) = 4 topics.
	if len(log.Topics) < 4 {
		logger.Warn().
			Str("tx_hash", log.TxHash.Hex()).
			Int("topic_count", len(log.Topics)).
			Msg("not enough indexed fields for outboundObservation event; need at least 4 topics")
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

	// Topics[3] is the deployed PC20 wrapper only on UniversalTxFinalized (address(0)
	// for PRC20). Read it uniformly, then drop it for every other observation event —
	// there Topics[3] is token/recipient, not a wrapper.
	var wrapperAddr string
	if addr := ethcommon.BytesToAddress(log.Topics[3].Bytes()); addr != (ethcommon.Address{}) {
		wrapperAddr = addr.Hex()
	}
	if eventType != EventTypeFinalizeUniversalTx {
		wrapperAddr = ""
	}

	// Create OutboundEvent payload
	payload := common.OutboundObservation{
		TxID:               txID,
		UniversalTxID:      universalTxID,
		Pc20WrapperAddress: wrapperAddr,
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
		Status:            store.StatusPending,
		ConfirmationType:  store.ConfirmationStandard, // Use STANDARD confirmation for outbound events
		ExpiryBlockHeight: 0,                          // 0 means no expiry
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
//
// Static words through txType are required. A truncated log is rejected rather
// than returned half-filled, since the zero values are not neutral: txType 0 is
// GAS, which routes funds to a different account than FUNDS does.
func parseUniversalTxEvent(event *store.Event, log *types.Log, chainID string, logger zerolog.Logger) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("need 3 indexed fields, got %d", len(log.Topics))
	}

	payload := common.InboundObservation{
		SourceChain: chainID,
		Sender:      ethcommon.BytesToAddress(log.Topics[1].Bytes()).Hex(),
		Recipient:   ethcommon.BytesToAddress(log.Topics[2].Bytes()).Hex(),
		LogIndex:    log.Index,
	}

	if len(log.Data) < 32*5 {
		return fmt.Errorf("log data has %d bytes, need at least %d for the static words", len(log.Data), 32*5)
	}

	// Parse common static fields: token (Word 0), amount (Word 1)
	payload.Token = ethcommon.BytesToAddress(log.Data[0*32+12 : 0*32+32]).Hex()
	payload.Amount = new(big.Int).SetBytes(log.Data[1*32 : 2*32]).String()

	dataOffset := new(big.Int).SetBytes(log.Data[2*32 : 3*32]).Uint64()
	return parseUniversalTx(event, log, dataOffset, &payload, logger)
}

// readDynamicBytes decodes ABI-encoded dynamic bytes at the given absolute offset in data.
//
// Both absOff and the length word are attacker-controlled: they come from the
// log data an RPC returns. Bounds are therefore checked by subtracting from the
// buffer length rather than adding to the offset — absOff+32 and dataStart+byteLen
// each wrap on a near-2^64 word and would pass an additive guard, then panic on
// the slice.
func readDynamicBytes(data []byte, absOff uint64) (string, bool) {
	n := uint64(len(data))
	if absOff > n || n-absOff < 32 {
		return "", false
	}
	byteLen := new(big.Int).SetBytes(data[absOff : absOff+32]).Uint64()

	dataStart := absOff + 32 // safe: absOff+32 <= n was just established
	if n-dataStart < byteLen {
		return "", false
	}
	return "0x" + hex.EncodeToString(data[dataStart:dataStart+byteLen]), true
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

// decodePayload reads the raw payload bytes at the given offset and stores the hex string.
// The core validator will decode the universal payload from these raw bytes.
func decodePayload(data []byte, dataOffset uint64, payload *common.InboundObservation, logger zerolog.Logger) {
	if dataOffset < uint64(32*5) {
		return
	}
	hexStr, ok := readDynamicBytes(data, dataOffset)
	if !ok {
		return
	}
	payload.RawPayload = hexStr
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
func finalizeEvent(event *store.Event, payload *common.InboundObservation, logger zerolog.Logger) {
	if b, err := json.Marshal(payload); err == nil {
		event.EventData = b
	} else {
		logger.Warn().Err(err).Msg("failed to marshal universal tx payload")
	}

	if payload.TxType == 0 || payload.TxType == 1 {
		event.ConfirmationType = store.ConfirmationFast
	} else {
		event.ConfirmationType = store.ConfirmationStandard
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
func parseUniversalTx(event *store.Event, log *types.Log, dataOffset uint64, payload *common.InboundObservation, logger zerolog.Logger) error {
	data := log.Data

	decodePayload(data, dataOffset, payload, logger)

	// revertRecipient (plain address at Word 3)
	if w := readWord(data, 3); w != nil {
		payload.RevertFundRecipient = ethcommon.BytesToAddress(w[12:32]).Hex()
	}

	// txType (Word 4). Always present: the caller rejects anything shorter than
	// five words, which is what stops this being left at 0 and read as GAS.
	payload.TxType = uint(new(big.Int).SetBytes(readWord(data, 4)).Uint64())

	// signatureData (Word 5 offset)
	if w := readWord(data, 5); w != nil {
		payload.VerificationData = decodeSignatureData(data, w, uint64(32*7))
	}

	// fromCEA (Word 6)
	if w := readWord(data, 6); w != nil {
		payload.FromCEA = new(big.Int).SetBytes(w).Uint64() != 0
	}

	finalizeEvent(event, payload, logger)
	return nil
}
