package svm

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

func nopLogger() zerolog.Logger {
	return zerolog.New(nil).Level(zerolog.Disabled)
}

// buildSendFundsLog constructs a valid "Program data: ..." log for a
// parseSendFundsEvent / decodeUniversalTxEvent call.
//
// Layout (Borsh):
//
//	discriminator  8  bytes
//	sender        32  bytes (Pubkey)
//	recipient     20  bytes (byte20)
//	bridge_token  32  bytes (Pubkey)
//	bridge_amount  8  bytes (u64 LE)
//	data_len       4  bytes (u32 LE)
//	data           variable
//	revert_recip  32  bytes (Pubkey)
//	tx_type        1  byte
//	sig_len        4  bytes (u32 LE)
//	sig_data       variable
//	fromCEA        1  byte
func buildSendFundsPayload(
	sender [32]byte,
	recipient [20]byte,
	bridgeToken [32]byte,
	bridgeAmount uint64,
	payload []byte,
	revertRecipient [32]byte,
	txType uint8,
	sigData []byte,
	fromCEA bool,
) []byte {
	buf := make([]byte, 0, 256)
	// discriminator (8 bytes, arbitrary)
	buf = append(buf, make([]byte, 8)...)
	// sender
	buf = append(buf, sender[:]...)
	// recipient
	buf = append(buf, recipient[:]...)
	// bridge_token
	buf = append(buf, bridgeToken[:]...)
	// bridge_amount
	amt := make([]byte, 8)
	binary.LittleEndian.PutUint64(amt, bridgeAmount)
	buf = append(buf, amt...)
	// data length + data
	dlen := make([]byte, 4)
	binary.LittleEndian.PutUint32(dlen, uint32(len(payload)))
	buf = append(buf, dlen...)
	buf = append(buf, payload...)
	// revert_recipient
	buf = append(buf, revertRecipient[:]...)
	// tx_type
	buf = append(buf, txType)
	// sig_len + sig_data
	slen := make([]byte, 4)
	binary.LittleEndian.PutUint32(slen, uint32(len(sigData)))
	buf = append(buf, slen...)
	buf = append(buf, sigData...)
	// fromCEA
	if fromCEA {
		buf = append(buf, 1)
	} else {
		buf = append(buf, 0)
	}
	return buf
}

func wrapAsLog(data []byte) string {
	return "Program data: " + base64.StdEncoding.EncodeToString(data)
}

// buildOutboundPayload builds a UniversalTxFinalized blob in the current layout.
// The finalize event surfaces gas_used (offset 112..120) as the value the parser
// reports as GasFeeUsed; gas_fee (offset 104..112) is the prepaid budget and is
// skipped. wrapper_address (offset 72..104) is left zero here.
func buildOutboundPayload(txID [32]byte, universalTxID [32]byte, gasUsed uint64) []byte {
	return buildFinalizePayloadWithToken(txID, universalTxID, gasUsed, [32]byte{})
}

// buildFinalizePayloadWithToken builds a full UniversalTxFinalized blob including
// the wrapper_address field at offset 72 (the PC20 wrapped mint on export):
//
//	disc(8) sub_tx_id(32) universal_tx_id(32) wrapper_address(32) gas_fee(8)
//	gas_used(8) gas_to_refund(8) ata_created(1) push_account(20) target(32)
//	token(32) amount(8)
func buildFinalizePayloadWithToken(txID, universalTxID [32]byte, gasUsed uint64, wrapper [32]byte) []byte {
	data := make([]byte, 221)
	copy(data[8:40], txID[:])
	copy(data[40:72], universalTxID[:])
	copy(data[72:104], wrapper[:]) // wrapper_address
	// gas_fee at 104..112 (left zero)
	binary.LittleEndian.PutUint64(data[112:120], gasUsed) // gas_used
	return data
}

// buildRevertPayload builds a RevertUniversalTx blob:
//
//	disc(8) sub_tx_id(32) universal_tx_id(32) revert_recipient(32) token(32)
//	amount(8) gas_used(8) revert_instruction...
func buildRevertPayload(txID, universalTxID [32]byte, gasUsed uint64) []byte {
	data := make([]byte, 160)
	copy(data[8:40], txID[:])
	copy(data[40:72], universalTxID[:])
	binary.LittleEndian.PutUint64(data[144:152], gasUsed) // gas_used
	return data
}

// buildRescuePayload builds a FundsRescued blob:
//
//	disc(8) sub_tx_id(32) universal_tx_id(32) token(32) amount(8) gas_used(8)
//	revert_instruction...
func buildRescuePayload(txID, universalTxID [32]byte, gasUsed uint64) []byte {
	data := make([]byte, 128)
	copy(data[8:40], txID[:])
	copy(data[40:72], universalTxID[:])
	binary.LittleEndian.PutUint64(data[112:120], gasUsed) // gas_used
	return data
}

func TestBase58ToHex(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "empty string returns 0x",
			input: "",
			want:  "0x",
		},
		{
			name:  "known base58 value",
			input: "1", // base58 "1" decodes to a single 0x00 byte
			want:  "0x00",
		},
		{
			name:  "known base58 multi-byte",
			input: "2g", // base58 "2g" decodes to 0x61
			want:  "0x61",
		},
		{
			name:    "invalid base58 characters",
			input:   "0OlI", // 0, O, l, I are not in base58 alphabet
			wantErr: true,
		},
		{
			name: "valid Solana pubkey",
			// 11111111111111111111111111111111 is the system program
			input: "11111111111111111111111111111111",
			want:  "0x" + "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := base58ToHex(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseEvent_Routing(t *testing.T) {
	logger := nopLogger()
	chainID := "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	sig := "testSig"

	// Build valid inbound data
	var sender, token, revert [32]byte
	var recipient [20]byte
	for i := range sender {
		sender[i] = 0x01
	}
	for i := range recipient {
		recipient[i] = 0x02
	}
	for i := range token {
		token[i] = 0x03
	}
	inboundData := buildSendFundsPayload(sender, recipient, token, 100, nil, revert, 0, nil, false)
	inboundLog := wrapAsLog(inboundData)

	// Build valid outbound data (80 bytes)
	var txID, utxID [32]byte
	for i := range txID {
		txID[i] = 0xAA
	}
	for i := range utxID {
		utxID[i] = 0xBB
	}
	outboundData := buildOutboundPayload(txID, utxID, 5000)
	outboundLog := wrapAsLog(outboundData)

	t.Run("send_funds routes to inbound parser", func(t *testing.T) {
		event := ParseEvent(inboundLog, sig, 100, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, store.EventTypeInbound, event.Type)
	})

	t.Run("finalize_universal_tx routes to outbound parser", func(t *testing.T) {
		event := ParseEvent(outboundLog, sig, 100, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, store.EventTypeOutbound, event.Type)
	})

	t.Run("revert_universal_tx routes to outbound parser", func(t *testing.T) {
		// Revert puts gas_used further in than finalize does.
		revertLog := wrapAsLog(buildRevertPayload(txID, utxID, 5000))
		event := ParseEvent(revertLog, sig, 100, 0, EventTypeRevertUniversalTx, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, store.EventTypeOutbound, event.Type)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, "5000", outbound.GasFeeUsed)
	})

	t.Run("a payload too short for a revert is refused", func(t *testing.T) {
		short := wrapAsLog(make([]byte, 151)) // revert reads gas_used at 144..152
		assert.Nil(t, ParseEvent(short, sig, 100, 0, EventTypeRevertUniversalTx, chainID, logger),
			"reading a revert at the finalize offset would report the wrong gas_used")
	})

	t.Run("unknown event type returns nil", func(t *testing.T) {
		event := ParseEvent(outboundLog, sig, 100, 0, "some_unknown_type", chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("empty event type returns nil", func(t *testing.T) {
		event := ParseEvent(outboundLog, sig, 100, 0, "", chainID, logger)
		assert.Nil(t, event)
	})
}

// Each outbound event has its own gas_used offset, so "long enough" differs
// by type.
func TestParseOutboundObservationEvent_LengthIsPerEventType(t *testing.T) {
	logger := nopLogger()
	chainID := "solana:devnet"
	sig := "sig"
	var txID, utxID [32]byte

	t.Run("finalize needs 120 bytes", func(t *testing.T) {
		assert.Nil(t, ParseEvent(wrapAsLog(make([]byte, 119)), sig, 1, 0, EventTypeFinalizeUniversalTx, chainID, logger))
		assert.NotNil(t, ParseEvent(wrapAsLog(make([]byte, 120)), sig, 1, 0, EventTypeFinalizeUniversalTx, chainID, logger))
	})

	t.Run("rescue needs 120 bytes", func(t *testing.T) {
		assert.Nil(t, ParseEvent(wrapAsLog(make([]byte, 119)), sig, 1, 0, EventTypeFundsRescued, chainID, logger))
		assert.NotNil(t, ParseEvent(wrapAsLog(make([]byte, 120)), sig, 1, 0, EventTypeFundsRescued, chainID, logger))
	})

	t.Run("revert needs 152 bytes", func(t *testing.T) {
		assert.Nil(t, ParseEvent(wrapAsLog(make([]byte, 151)), sig, 1, 0, EventTypeRevertUniversalTx, chainID, logger))
		assert.NotNil(t, ParseEvent(wrapAsLog(buildRevertPayload(txID, utxID, 1)), sig, 1, 0, EventTypeRevertUniversalTx, chainID, logger))
	})

	t.Run("an unroutable event type is refused", func(t *testing.T) {
		assert.Nil(t, parseOutboundObservationEvent(
			wrapAsLog(buildOutboundPayload(txID, utxID, 1)), sig, 1, 0, "send_funds", logger),
			"only the three outbound events have a known gas_used offset")
	})
}

// Each event type must read gas_used from its own offset.
func TestParseOutboundObservationEvent_ReadsItsOwnOffset(t *testing.T) {
	logger := nopLogger()
	var txID, utxID [32]byte

	for _, tc := range []struct {
		eventType string
		payload   []byte
	}{
		{EventTypeFinalizeUniversalTx, buildOutboundPayload(txID, utxID, 4242)},
		{EventTypeFundsRescued, buildOutboundPayload(txID, utxID, 4242)},
		{EventTypeRevertUniversalTx, buildRevertPayload(txID, utxID, 4242)},
	} {
		t.Run(tc.eventType, func(t *testing.T) {
			event := ParseEvent(wrapAsLog(tc.payload), "sig", 1, 0, tc.eventType, "solana:devnet", logger)
			require.NotNil(t, event)
			var outbound common.OutboundObservation
			require.NoError(t, json.Unmarshal(event.EventData, &outbound))
			assert.Equal(t, "4242", outbound.GasFeeUsed)
		})
	}
}

func TestParseSendFundsEvent(t *testing.T) {
	logger := nopLogger()
	chainID := "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	sig := "inboundSig123"

	t.Run("returns nil for log without Program data prefix", func(t *testing.T) {
		event := ParseEvent("some random log line", sig, 10, 0, EventTypeSendFunds, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("returns nil for empty log", func(t *testing.T) {
		event := ParseEvent("", sig, 10, 0, EventTypeSendFunds, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("returns nil for invalid base64", func(t *testing.T) {
		event := ParseEvent("Program data: !!!not-b64!!!", sig, 10, 0, EventTypeSendFunds, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("returns nil for data shorter than 8 bytes", func(t *testing.T) {
		short := wrapAsLog([]byte{0x01, 0x02, 0x03})
		event := ParseEvent(short, sig, 10, 0, EventTypeSendFunds, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("parses a full valid inbound event", func(t *testing.T) {
		var sender [32]byte
		var recipient [20]byte
		var token [32]byte
		var revert [32]byte
		for i := range sender {
			sender[i] = byte(i + 1)
		}
		for i := range recipient {
			recipient[i] = byte(0xAB)
		}
		for i := range token {
			token[i] = byte(0xCD)
		}
		for i := range revert {
			revert[i] = byte(0xEF)
		}
		rawPayload := []byte("hello world")
		sigData := []byte{0xDE, 0xAD, 0xBE, 0xEF}

		data := buildSendFundsPayload(sender, recipient, token, 42000, rawPayload, revert, 1, sigData, true)
		log := wrapAsLog(data)

		event := ParseEvent(log, sig, 500, 3, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)

		// Check event metadata
		assert.Equal(t, "inboundSig123:3", event.EventID)
		assert.Equal(t, uint64(500), event.BlockHeight)
		assert.Equal(t, store.EventTypeInbound, event.Type)
		assert.Equal(t, store.StatusPending, event.Status)
		assert.Equal(t, uint64(0), event.ExpiryBlockHeight)

		// TxType 1 should give FAST confirmation
		assert.Equal(t, store.ConfirmationFast, event.ConfirmationType)

		// Unmarshal EventData
		var utx common.InboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &utx))

		assert.Equal(t, chainID, utx.SourceChain)
		assert.Equal(t, uint(3), utx.LogIndex)
		assert.Equal(t, "0x"+hex.EncodeToString(recipient[:]), utx.Recipient)
		assert.Equal(t, "42000", utx.Amount)
		assert.Equal(t, "0x"+hex.EncodeToString(rawPayload), utx.RawPayload)
		assert.Equal(t, "0x"+hex.EncodeToString(sigData), utx.VerificationData)
		assert.Equal(t, uint(1), utx.TxType)
		assert.True(t, utx.FromCEA)
	})

	t.Run("txType 0 gives FAST confirmation", func(t *testing.T) {
		var s, tok, rev [32]byte
		var r [20]byte
		data := buildSendFundsPayload(s, r, tok, 0, nil, rev, 0, nil, false)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, store.ConfirmationFast, event.ConfirmationType)
	})

	t.Run("txType 2 gives STANDARD confirmation", func(t *testing.T) {
		var s, tok, rev [32]byte
		var r [20]byte
		data := buildSendFundsPayload(s, r, tok, 0, nil, rev, 2, nil, false)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, store.ConfirmationStandard, event.ConfirmationType)
	})

	t.Run("fromCEA false is parsed correctly", func(t *testing.T) {
		var s, tok, rev [32]byte
		var r [20]byte
		data := buildSendFundsPayload(s, r, tok, 0, nil, rev, 0, nil, false)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		var utx common.InboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &utx))
		assert.False(t, utx.FromCEA)
	})

	t.Run("empty payload and sig data are handled", func(t *testing.T) {
		var s, tok, rev [32]byte
		var r [20]byte
		data := buildSendFundsPayload(s, r, tok, 0, nil, rev, 0, nil, false)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		var utx common.InboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &utx))
		assert.Empty(t, utx.RawPayload)
		assert.Empty(t, utx.VerificationData)
	})

	t.Run("large bridge amount", func(t *testing.T) {
		var s, tok, rev [32]byte
		var r [20]byte
		maxU64 := uint64(18446744073709551615) // max uint64
		data := buildSendFundsPayload(s, r, tok, maxU64, nil, rev, 0, nil, false)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		var utx common.InboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &utx))
		assert.Equal(t, "18446744073709551615", utx.Amount)
	})
}

func TestParseSendFundsEvent_TruncatedData(t *testing.T) {
	logger := nopLogger()
	chainID := "solana:devnet"
	sig := "truncSig"

	// A truncated event is discarded rather than stored. Every field it fails to
	// reach would otherwise be left at its zero value, and tx_type 0 is GAS,
	// which credits the sender UEA instead of depositing to the recipient.
	t.Run("data too short for sender is discarded", func(t *testing.T) {
		// Only discriminator (8 bytes), no sender
		data := make([]byte, 8)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("data truncated after sender is discarded", func(t *testing.T) {
		// 8 disc + 32 sender = 40 bytes, missing recipient
		data := make([]byte, 40)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("truncated one byte before tx_type is discarded", func(t *testing.T) {
		// Everything through revert_recipient, then nothing. This is the exact
		// shape that used to decode as TxType 0 and route to GAS.
		data := make([]byte, 136)
		binary.LittleEndian.PutUint32(data[100:104], 0)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		assert.Nil(t, event)
	})
}

func TestParseOutboundObservationEvent(t *testing.T) {
	logger := nopLogger()
	chainID := "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	signature := "5wHu1qwD7q5xMkZxq6z2S3r4y5N7m8P9kL0jH1gF2dE3cB4aA5b6C7d8E9f0G1h2"

	t.Run("parses valid outbound observation event", func(t *testing.T) {
		var txID, utxID [32]byte
		for i := range txID {
			txID[i] = 0xAA
		}
		for i := range utxID {
			utxID[i] = 0xBB
		}
		data := buildOutboundPayload(txID, utxID, 5000)
		log := wrapAsLog(data)

		event := ParseEvent(log, signature, 12345, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		assert.Contains(t, event.EventID, signature)
		assert.Equal(t, uint64(12345), event.BlockHeight)
		assert.Equal(t, store.EventTypeOutbound, event.Type)
		assert.Equal(t, store.StatusPending, event.Status)
		assert.Equal(t, store.ConfirmationStandard, event.ConfirmationType)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, "0x"+hex.EncodeToString(txID[:]), outbound.TxID)
		assert.Equal(t, "0x"+hex.EncodeToString(utxID[:]), outbound.UniversalTxID)
		assert.Equal(t, "5000", outbound.GasFeeUsed)
	})

	t.Run("finalize event reports pc20 wrapper from wrapper_address field", func(t *testing.T) {
		var txID, utxID, token [32]byte
		for i := range token {
			token[i] = byte(i + 1)
		}
		data := buildFinalizePayloadWithToken(txID, utxID, 5000, token)
		event := ParseEvent(wrapAsLog(data), signature, 1, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, solana.PublicKeyFromBytes(token[:]).String(), outbound.Pc20WrapperAddress)
	})

	t.Run("native SOL finalize (zero token) reports no wrapper", func(t *testing.T) {
		var txID, utxID, zero [32]byte
		data := buildFinalizePayloadWithToken(txID, utxID, 5000, zero)
		event := ParseEvent(wrapAsLog(data), signature, 1, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Empty(t, outbound.Pc20WrapperAddress)
	})

	t.Run("revert event reads gas_used (@144) and no wrapper", func(t *testing.T) {
		var txID, utxID [32]byte
		data := buildRevertPayload(txID, utxID, 7777)
		event := ParseEvent(wrapAsLog(data), signature, 1, 0, EventTypeRevertUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Empty(t, outbound.Pc20WrapperAddress)
		assert.Equal(t, "7777", outbound.GasFeeUsed)
	})

	t.Run("rescue event reads gas_used (@112) and no wrapper", func(t *testing.T) {
		var txID, utxID [32]byte
		data := buildRescuePayload(txID, utxID, 3333)
		event := ParseEvent(wrapAsLog(data), signature, 1, 0, EventTypeFundsRescued, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Empty(t, outbound.Pc20WrapperAddress)
		assert.Equal(t, "3333", outbound.GasFeeUsed)
	})

	t.Run("returns nil for log without Program data prefix", func(t *testing.T) {
		event := ParseEvent("Some other log message", signature, 12345, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("returns nil for empty log", func(t *testing.T) {
		event := ParseEvent("", signature, 12345, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("returns nil for invalid base64", func(t *testing.T) {
		event := ParseEvent("Program data: not-valid-base64!!!", signature, 12345, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("returns nil for data too short", func(t *testing.T) {
		shortData := make([]byte, 119) // gas_used ends at 120
		event := ParseEvent(wrapAsLog(shortData), signature, 12345, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("parses minimum valid finalize data", func(t *testing.T) {
		data := make([]byte, 120)
		for i := 8; i < 40; i++ {
			data[i] = 0x11
		}
		for i := 40; i < 72; i++ {
			data[i] = 0x22
		}
		// wrapper_address at 72..104 and gas_fee at 104..112 left zero; gas_used at 112..120
		binary.LittleEndian.PutUint64(data[112:120], 12345)

		event := ParseEvent(wrapAsLog(data), signature, 100, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Contains(t, outbound.TxID, "0x1111")
		assert.Contains(t, outbound.UniversalTxID, "0x2222")
		assert.Equal(t, "12345", outbound.GasFeeUsed)
	})

	t.Run("handles data longer than the fixed fields", func(t *testing.T) {
		var txID, utxID [32]byte
		for i := range txID {
			txID[i] = 0xAA
		}
		for i := range utxID {
			utxID[i] = 0xBB
		}
		data := buildOutboundPayload(txID, utxID, 9999)
		// Append extra bytes
		data = append(data, make([]byte, 40)...)

		event := ParseEvent(wrapAsLog(data), signature, 100, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, "0x"+hex.EncodeToString(txID[:]), outbound.TxID)
		assert.Equal(t, "0x"+hex.EncodeToString(utxID[:]), outbound.UniversalTxID)
		assert.Equal(t, "9999", outbound.GasFeeUsed)
	})

	t.Run("zero gas fee", func(t *testing.T) {
		var txID, utxID [32]byte
		data := buildOutboundPayload(txID, utxID, 0)
		event := ParseEvent(wrapAsLog(data), signature, 100, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, "0", outbound.GasFeeUsed)
	})

	t.Run("max uint64 gas fee", func(t *testing.T) {
		var txID, utxID [32]byte
		data := buildOutboundPayload(txID, utxID, ^uint64(0))
		event := ParseEvent(wrapAsLog(data), signature, 100, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundObservation
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, "18446744073709551615", outbound.GasFeeUsed)
	})
}

func TestEventIDFormat(t *testing.T) {
	logger := nopLogger()
	chainID := "solana:devnet"

	var txID, utxID [32]byte
	data := buildOutboundPayload(txID, utxID, 0)
	log := wrapAsLog(data)

	tests := []struct {
		name      string
		signature string
		slot      uint64
		logIndex  uint
		wantID    string
	}{
		{
			name:      "format with logIndex 0",
			signature: "abc123",
			slot:      100,
			logIndex:  0,
			wantID:    "abc123:0",
		},
		{
			name:      "format with logIndex 5",
			signature: "def456",
			slot:      200,
			logIndex:  5,
			wantID:    "def456:5",
		},
		{
			name:      "format with large logIndex",
			signature: "ghi789",
			slot:      300,
			logIndex:  999,
			wantID:    "ghi789:999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseEvent(log, tt.signature, tt.slot, tt.logIndex, EventTypeFinalizeUniversalTx, chainID, logger)
			require.NotNil(t, event)
			assert.Equal(t, tt.wantID, event.EventID)
			assert.Equal(t, tt.slot, event.BlockHeight)
		})
	}

	t.Run("inbound event also uses signature:logIndex format", func(t *testing.T) {
		var s, tok, rev [32]byte
		var r [20]byte
		inboundData := buildSendFundsPayload(s, r, tok, 0, nil, rev, 0, nil, false)
		event := ParseEvent(wrapAsLog(inboundData), "mySig", 42, 7, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, "mySig:7", event.EventID)
	})
}

func TestParseEvent_EventTypeConstants(t *testing.T) {
	// Verify the constants have expected values
	assert.Equal(t, "send_funds", EventTypeSendFunds)
	assert.Equal(t, "finalize_universal_tx", EventTypeFinalizeUniversalTx)
	assert.Equal(t, "revert_universal_tx", EventTypeRevertUniversalTx)
}

func TestDecodeUniversalTxEvent_PartialData(t *testing.T) {
	logger := nopLogger()

	t.Run("returns error when not enough data for sender", func(t *testing.T) {
		// only discriminator, no sender bytes
		data := make([]byte, 8)
		_, err := decodeUniversalTxEvent(data, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sender")
	})

	t.Run("returns error when not enough data for recipient", func(t *testing.T) {
		// 8 disc + 32 sender = 40, but recipient needs 20 more
		data := make([]byte, 40)
		_, err := decodeUniversalTxEvent(data, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "recipient")
	})

	t.Run("returns error when not enough data for bridge_token", func(t *testing.T) {
		// 8 + 32 + 20 = 60, bridge_token needs 32 more
		data := make([]byte, 60)
		_, err := decodeUniversalTxEvent(data, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bridge_token")
	})

	t.Run("returns error when not enough data for bridge_amount", func(t *testing.T) {
		// 8 + 32 + 20 + 32 = 92, bridge_amount needs 8 more
		data := make([]byte, 92)
		_, err := decodeUniversalTxEvent(data, logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bridge_amount")
	})

	// Everything through signature_data is required. Returning a partial result
	// leaves tx_type at 0, which is GAS on the wire, so a truncated FUNDS
	// transfer would be credited to the sender UEA instead of the recipient.

	t.Run("returns error when no data field length", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 = 100, no data_len
		data := make([]byte, 100)
		binary.LittleEndian.PutUint64(data[92:100], 777)
		_, err := decodeUniversalTxEvent(data, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "data field length")
	})

	t.Run("returns error when data field exceeds available bytes", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 + 4 = 104
		data := make([]byte, 104)
		binary.LittleEndian.PutUint64(data[92:100], 555)
		binary.LittleEndian.PutUint32(data[100:104], 999) // claims 999 bytes of payload
		_, err := decodeUniversalTxEvent(data, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "claims 999 bytes")
	})

	t.Run("returns error when missing revert recipient", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 + 4(data_len=0) = 104
		data := make([]byte, 104)
		binary.LittleEndian.PutUint32(data[100:104], 0) // 0 length payload
		_, err := decodeUniversalTxEvent(data, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "revert recipient")
	})

	t.Run("returns error when missing tx_type rather than defaulting to GAS", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 + 4(data_len=0) + 32(revert) = 136
		data := make([]byte, 136)
		binary.LittleEndian.PutUint32(data[100:104], 0)
		_, err := decodeUniversalTxEvent(data, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tx_type")
	})

	t.Run("returns error when missing signature length", func(t *testing.T) {
		// 136 + 1(tx_type) = 137, no signature length
		data := make([]byte, 137)
		binary.LittleEndian.PutUint32(data[100:104], 0)
		data[136] = 2 // Funds
		_, err := decodeUniversalTxEvent(data, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signature length")
	})

	t.Run("returns error when signature data exceeds available bytes", func(t *testing.T) {
		data := make([]byte, 141)
		binary.LittleEndian.PutUint32(data[100:104], 0)
		data[136] = 2
		binary.LittleEndian.PutUint32(data[137:141], 500)
		_, err := decodeUniversalTxEvent(data, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "claims 500 bytes")
	})

	t.Run("from_cea stays optional and defaults to false", func(t *testing.T) {
		// 141 bytes: complete through signature_data, no from_cea byte.
		data := make([]byte, 141)
		binary.LittleEndian.PutUint32(data[100:104], 0)
		data[136] = 2 // Funds
		binary.LittleEndian.PutUint32(data[137:141], 0)
		result, err := decodeUniversalTxEvent(data, logger)
		require.NoError(t, err)
		assert.Equal(t, uint(2), result.TxType)
		assert.False(t, result.FromCEA)
	})
}

// Real UniversalTx events captured from the deployed devnet gateway
// CFVSincHYbETh2k7w6u1ENEkjbSLtveRCEBupKidw2VS. They pin the decoder to what
// the chain actually emits rather than to a hand-built fixture.
//
// Layout, matching the gateway on pc20-3rd-iteration:
//
//	disc 8, sender 32, recipient 20, token 32, amount u64,
//	payload (u32 len + bytes), revert_recipient 32, tx_type 1,
//	signature_data (u32 len + bytes), from_cea 1  = 142 bytes when both vecs are empty
var devnetUniversalTxEvents = []struct {
	name           string
	hex            string
	wantAmount     string
	wantTxType     uint
	wantFromCEA    bool
	wantConfirmDep string
}{
	{
		name:           "3000000 lamports, Funds",
		hex:            "6c9ad829b5ea1d7c5824d1bda3f79e54416ae3d2ec8d8a7456ae9a3e8a85e2f43e3bd20f25a39e5107a26674effcfbef4ee6cc6e8a00dc54801d83d90000000000000000000000000000000000000000000000000000000000000000c0c62d0000000000000000005824d1bda3f79e54416ae3d2ec8d8a7456ae9a3e8a85e2f43e3bd20f25a39e51020000000001",
		wantAmount:     "3000000",
		wantTxType:     2,
		wantFromCEA:    true,
		wantConfirmDep: store.ConfirmationStandard,
	},
	{
		name:           "8000 lamports, Funds",
		hex:            "6c9ad829b5ea1d7cdc84c8dd7c695f0ed78f3507fd867827812dcd9ccbba61ca9a16d899b6f5ac665c70c864cf1adfb04a0e107ffa248ba3600eab8dcbcae9e66452fe98abcf0fa51e557fc8d671c7fc6ce83c05f92992a3d2bf1932401f00000000000000000000dc84c8dd7c695f0ed78f3507fd867827812dcd9ccbba61ca9a16d899b6f5ac66020000000001",
		wantAmount:     "8000",
		wantTxType:     2,
		wantFromCEA:    true,
		wantConfirmDep: store.ConfirmationStandard,
	},
	{
		name:           "5000000 lamports, Funds",
		hex:            "6c9ad829b5ea1d7cdc84c8dd7c695f0ed78f3507fd867827812dcd9ccbba61ca9a16d899b6f5ac665c70c864cf1adfb04a0e107ffa248ba3600eab8d0000000000000000000000000000000000000000000000000000000000000000404b4c000000000000000000dc84c8dd7c695f0ed78f3507fd867827812dcd9ccbba61ca9a16d899b6f5ac66020000000001",
		wantAmount:     "5000000",
		wantTxType:     2,
		wantFromCEA:    true,
		wantConfirmDep: store.ConfirmationStandard,
	},
}

func TestDecodeUniversalTxEvent_RealDevnetEvents(t *testing.T) {
	logger := nopLogger()

	for _, tc := range devnetUniversalTxEvents {
		t.Run(tc.name, func(t *testing.T) {
			data, err := hex.DecodeString(tc.hex)
			require.NoError(t, err)
			require.Len(t, data, 142, "captured event is not the deployed layout")

			got, err := decodeUniversalTxEvent(data, logger)
			require.NoError(t, err)

			assert.Equal(t, tc.wantAmount, got.Amount)
			assert.Equal(t, tc.wantTxType, got.TxType, "tx_type must survive decoding, not be defaulted")
			assert.Equal(t, tc.wantFromCEA, got.FromCEA)
			assert.NotEmpty(t, got.Sender)
			assert.NotEmpty(t, got.Recipient)
			assert.NotEmpty(t, got.RevertFundRecipient)
		})
	}
}

// FUNDS must take the slower confirmation path. The old default of 0 selected
// FAST as well as routing to GAS, so a high value transfer lost finality too.
func TestParseSendFundsEvent_RealDevnetEventConfirmation(t *testing.T) {
	logger := nopLogger()

	for _, tc := range devnetUniversalTxEvents {
		t.Run(tc.name, func(t *testing.T) {
			data, err := hex.DecodeString(tc.hex)
			require.NoError(t, err)
			log := "Program data: " + base64.StdEncoding.EncodeToString(data)

			event := ParseEvent(log, "devnetSig", 1, 0, EventTypeSendFunds, "solana:devnet", logger)
			require.NotNil(t, event)
			require.NotNil(t, event.EventData)

			assert.Equal(t, store.EventTypeInbound, event.Type)
			assert.Equal(t, tc.wantConfirmDep, event.ConfirmationType)
		})
	}
}

// Truncating a real event anywhere past bridge_amount must be rejected. Before
// the fix each of these decoded successfully with TxType left at 0.
func TestDecodeUniversalTxEvent_TruncatedRealEventIsRejected(t *testing.T) {
	logger := nopLogger()

	full, err := hex.DecodeString(devnetUniversalTxEvents[0].hex)
	require.NoError(t, err)

	// 100 is the end of bridge_amount; 142 is the whole event. from_cea is the
	// only optional field, so 141 is the shortest valid length.
	for n := 100; n < 141; n++ {
		got, err := decodeUniversalTxEvent(full[:n], logger)
		require.Error(t, err, "%d-byte truncation was accepted", n)
		assert.Nil(t, got)
	}

	// The two valid lengths still decode, and both carry the real tx_type.
	for _, n := range []int{141, 142} {
		got, err := decodeUniversalTxEvent(full[:n], logger)
		require.NoError(t, err, "%d-byte event was rejected", n)
		assert.Equal(t, uint(2), got.TxType)
	}
}
