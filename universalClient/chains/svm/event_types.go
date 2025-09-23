package svm

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/rs/zerolog"
)

const (
	TxWithFundsDiscriminator = "2b1f1f0204ec6bff"
)

const (
	TxTypeFunds   uint8 = 0
	TxTypeMessage uint8 = 1
)

const (
	maxRevertMessageLength = 10000
	maxSignatureLength     = 10000
)

type ParsedEventData struct {
	EventType        string
	Sender           string
	Recipient        string
	BridgeAmount     uint64
	GasAmount        uint64
	BridgeToken      string
	Data             string
	VerificationData string
	RevertRecipient  string
	RevertMessage    string
	TxType           uint8
	LogIndex         uint
}

type EventDecoder struct {
	logger zerolog.Logger
}

func NewEventDecoder(logger zerolog.Logger) *EventDecoder {
	return &EventDecoder{
		logger: logger.With().Str("component", "svm_event_decoder").Logger(),
	}
}

func (ed *EventDecoder) DecodeEventData(data []byte) (*ParsedEventData, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("data too short for discriminator: %d bytes", len(data))
	}

	discriminator := hex.EncodeToString(data[:8])

	ed.logger.Debug().
		Str("discriminator", discriminator).
		Int("data_len", len(data)).
		Msg("decoding event data")

	switch discriminator {
	case TxWithFundsDiscriminator:
		return ed.decodeTxWithFundsEvent(data)
	default:
		return nil, fmt.Errorf("unknown event discriminator: %s", discriminator)
	}
}

func (ed *EventDecoder) decodeTxWithFundsEvent(data []byte) (*ParsedEventData, error) {
	if len(data) < 120 {
		ed.logger.Warn().
			Int("data_len", len(data)).
			Msg("data might be too short for complete TxWithFunds event")
	}

	offset := 8
	result := &ParsedEventData{
		EventType: "TxWithFunds",
		TxType:    TxTypeFunds,
	}

	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for sender")
	}
	sender := solana.PublicKey(data[offset : offset+32])
	result.Sender = sender.String()
	ed.logger.Debug().
		Str("sender", result.Sender).
		Int("offset", offset).
		Str("hex", hex.EncodeToString(data[offset:offset+32])).
		Msg("parsed sender")
	offset += 32

	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for recipient")
	}
	recipient := solana.PublicKey(data[offset : offset+32])
	result.Recipient = recipient.String()
	ed.logger.Debug().
		Str("recipient", result.Recipient).
		Int("offset", offset).
		Str("hex", hex.EncodeToString(data[offset:offset+32])).
		Msg("parsed recipient")
	offset += 32

	if len(data) < offset+8 {
		return nil, fmt.Errorf("not enough data for bridge_amount")
	}
	result.BridgeAmount = binary.LittleEndian.Uint64(data[offset : offset+8])
	ed.logger.Debug().
		Uint64("bridge_amount", result.BridgeAmount).
		Int("offset", offset).
		Str("hex", hex.EncodeToString(data[offset:offset+8])).
		Msg("parsed bridge_amount")
	offset += 8

	if len(data) < offset+8 {
		return nil, fmt.Errorf("not enough data for gas_amount")
	}
	result.GasAmount = binary.LittleEndian.Uint64(data[offset : offset+8])
	ed.logger.Debug().
		Uint64("gas_amount", result.GasAmount).
		Int("offset", offset).
		Str("hex", hex.EncodeToString(data[offset:offset+8])).
		Msg("parsed gas_amount")
	offset += 8

	if len(data) < offset+32 {
		return nil, fmt.Errorf("not enough data for bridge_token")
	}
	bridgeToken := solana.PublicKey(data[offset : offset+32])
	result.BridgeToken = bridgeToken.String()
	ed.logger.Debug().
		Str("bridge_token", result.BridgeToken).
		Int("offset", offset).
		Str("hex", hex.EncodeToString(data[offset:offset+32])).
		Msg("parsed bridge_token")
	offset += 32

	if len(data) < offset+4 {
		ed.logger.Warn().Msg("not enough data for data field length")
		return result, nil
	}
	dataLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	ed.logger.Debug().
		Uint32("data_len", dataLen).
		Int("offset", offset).
		Str("hex", hex.EncodeToString(data[offset:offset+4])).
		Msg("parsed data length")
	offset += 4

	if len(data) < offset+int(dataLen) {
		ed.logger.Warn().
			Uint32("expected_len", dataLen).
			Int("available", len(data)-offset).
			Msg("not enough data for data field")
		return result, nil
	}
	if dataLen > 0 {
		dataField := data[offset : offset+int(dataLen)]
		result.Data = "0x" + hex.EncodeToString(dataField)
		ed.logger.Debug().
			Str("data", result.Data).
			Int("offset", offset).
			Msg("parsed data field")
		offset += int(dataLen)
	} else {
		ed.logger.Debug().Msg("data field is empty (length=0)")
	}

	if len(data) < offset+1 {
		ed.logger.Warn().Msg("not enough data for revert option discriminator")
		return result, nil
	}
	hasRevertRecipient := data[offset] == 1
	ed.logger.Debug().
		Bool("has_revert_recipient", hasRevertRecipient).
		Int("offset", offset).
		Str("hex", hex.EncodeToString(data[offset:offset+1])).
		Msg("parsed revert recipient option")
	offset++

	if hasRevertRecipient {
		if len(data) < offset+32 {
			ed.logger.Warn().Msg("not enough data for revert recipient")
			return result, nil
		}
		revertRecipient := solana.PublicKey(data[offset : offset+32])
		result.RevertRecipient = revertRecipient.String()
		ed.logger.Debug().
			Str("revert_recipient", result.RevertRecipient).
			Int("offset", offset).
			Msg("parsed revert recipient")
		offset += 32
	}

	if len(data) < offset+4 {
		ed.logger.Warn().Msg("not enough data for revert message length")
		return result, nil
	}
	revertMsgLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	remainingForRevert := len(data) - offset
	revertLenValid := int(revertMsgLen) <= remainingForRevert && revertMsgLen <= maxRevertMessageLength
	if !revertLenValid {
		ed.logger.Warn().
			Uint32("revert_msg_len", revertMsgLen).
			Int("available", remainingForRevert).
			Int("total_data_len", len(data)).
			Int("current_offset", offset).
			Msg("revert message length invalid, skipping revert message parsing")
	}

	if revertLenValid && revertMsgLen > 0 {
		if len(data) < offset+int(revertMsgLen) {
			ed.logger.Warn().
				Uint32("expected_len", revertMsgLen).
				Int("available", len(data)-offset).
				Msg("not enough data for revert message")
			return result, nil
		}
		revertMsg := data[offset : offset+int(revertMsgLen)]
		result.RevertMessage = "0x" + hex.EncodeToString(revertMsg)
		offset += int(revertMsgLen)
	}

	txTypeIndex := offset
	if !revertLenValid {
		var ok bool
		txTypeIndex, ok = findTxTypeOffset(data, offset)
		if !ok {
			ed.logger.Warn().
				Int("start_offset", offset).
				Msg("failed to locate tx_type after truncated revert message; defaulting to Funds")
			result.TxType = TxTypeFunds
			return result, nil
		}
	}

	if len(data) <= txTypeIndex {
		ed.logger.Warn().Msg("not enough data for tx_type, defaulting to Funds")
		result.TxType = TxTypeFunds
		return result, nil
	}
	txType := data[txTypeIndex]
	if txType != TxTypeFunds && txType != TxTypeMessage {
		ed.logger.Warn().
			Uint8("candidate", txType).
			Int("offset", txTypeIndex).
			Msg("invalid tx_type value, defaulting to Funds")
		result.TxType = TxTypeFunds
		return result, nil
	}
	result.TxType = txType
	offset = txTypeIndex + 1

	if len(data) < offset+4 {
		ed.logger.Warn().Msg("not enough data for signature length")
		return result, nil
	}
	sigLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	remainingBytes := len(data) - offset
	if int(sigLen) > remainingBytes {
		ed.logger.Warn().
			Uint32("expected_len", sigLen).
			Int("available", remainingBytes).
			Msg("signature data length exceeds available data, skipping")
		return result, nil
	}

	if sigLen > maxSignatureLength {
		ed.logger.Warn().
			Uint32("sig_len", sigLen).
			Int("available", remainingBytes).
			Msg("signature length unreasonably large, skipping")
		return result, nil
	}

	if sigLen > 0 {
		sigData := data[offset : offset+int(sigLen)]
		result.VerificationData = "0x" + hex.EncodeToString(sigData)
		offset += int(sigLen)
	}

	ed.logger.Debug().
		Str("sender", result.Sender).
		Str("recipient", result.Recipient).
		Uint64("bridge_amount", result.BridgeAmount).
		Uint64("gas_amount", result.GasAmount).
		Str("bridge_token", result.BridgeToken).
		Str("data", result.Data).
		Str("verification_data", result.VerificationData).
		Str("revert_recipient", result.RevertRecipient).
		Str("revert_message", result.RevertMessage).
		Uint8("tx_type", result.TxType).
		Int("total_bytes_parsed", offset).
		Msg("decoded TxWithFunds event")

	return result, nil
}

func findTxTypeOffset(data []byte, start int) (int, bool) {
	if start < 0 {
		start = 0
	}

	for idx := start; idx < len(data); idx++ {
		candidate := data[idx]
		if candidate != TxTypeFunds && candidate != TxTypeMessage {
			continue
		}

		if len(data) < idx+1+4 {
			return -1, false
		}

		sigLen := binary.LittleEndian.Uint32(data[idx+1 : idx+5])
		if sigLen > maxSignatureLength {
			continue
		}

		end := idx + 5 + int(sigLen)
		if end > len(data) {
			continue
		}

		return idx, true
	}

	return -1, false
}
