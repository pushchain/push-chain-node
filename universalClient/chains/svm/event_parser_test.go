package svm

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

func nopLogger() zerolog.Logger {
	return zerolog.New(nil).Level(zerolog.Disabled)
}

// buildSendFundsLog constructs a valid "Program data: ..." log for a
// parseSendFundsEvent / decodeUniversalTxEvent call.
//
// Layout (Borsh):
//   discriminator  8  bytes
//   sender        32  bytes (Pubkey)
//   recipient     20  bytes (byte20)
//   bridge_token  32  bytes (Pubkey)
//   bridge_amount  8  bytes (u64 LE)
//   data_len       4  bytes (u32 LE)
//   data           variable
//   revert_recip  32  bytes (Pubkey)
//   tx_type        1  byte
//   sig_len        4  bytes (u32 LE)
//   sig_data       variable
//   fromCEA        1  byte
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

// buildOutboundPayload builds the minimum 80-byte outbound event data.
func buildOutboundPayload(txID [32]byte, universalTxID [32]byte, gasFee uint64) []byte {
	data := make([]byte, 80)
	// discriminator (8 bytes, zeroed is fine)
	copy(data[8:40], txID[:])
	copy(data[40:72], universalTxID[:])
	binary.LittleEndian.PutUint64(data[72:80], gasFee)
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
			input: "1",   // base58 "1" decodes to a single 0x00 byte
			want:  "0x00",
		},
		{
			name:  "known base58 multi-byte",
			input: "2g",  // base58 "2g" decodes to 0x61
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
		event := ParseEvent(outboundLog, sig, 100, 0, EventTypeRevertUniversalTx, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, store.EventTypeOutbound, event.Type)
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
		var utx common.UniversalTx
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
		var utx common.UniversalTx
		require.NoError(t, json.Unmarshal(event.EventData, &utx))
		assert.False(t, utx.FromCEA)
	})

	t.Run("empty payload and sig data are handled", func(t *testing.T) {
		var s, tok, rev [32]byte
		var r [20]byte
		data := buildSendFundsPayload(s, r, tok, 0, nil, rev, 0, nil, false)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		var utx common.UniversalTx
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
		var utx common.UniversalTx
		require.NoError(t, json.Unmarshal(event.EventData, &utx))
		assert.Equal(t, "18446744073709551615", utx.Amount)
	})
}

func TestParseSendFundsEvent_TruncatedData(t *testing.T) {
	logger := nopLogger()
	chainID := "solana:devnet"
	sig := "truncSig"

	t.Run("data too short for sender returns event with nil EventData", func(t *testing.T) {
		// Only discriminator (8 bytes), no sender
		data := make([]byte, 8)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		// Event is created but parseUniversalTxEvent will fail to decode,
		// so EventData may be nil
		assert.Equal(t, store.EventTypeInbound, event.Type)
	})

	t.Run("data truncated after sender still returns event", func(t *testing.T) {
		// 8 disc + 32 sender = 40 bytes, missing recipient
		data := make([]byte, 40)
		event := ParseEvent(wrapAsLog(data), sig, 1, 0, EventTypeSendFunds, chainID, logger)
		require.NotNil(t, event)
		assert.Equal(t, store.EventTypeInbound, event.Type)
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

		var outbound common.OutboundEvent
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, "0x"+hex.EncodeToString(txID[:]), outbound.TxID)
		assert.Equal(t, "0x"+hex.EncodeToString(utxID[:]), outbound.UniversalTxID)
		assert.Equal(t, "5000", outbound.GasFeeUsed)
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
		shortData := make([]byte, 72) // needs 80
		event := ParseEvent(wrapAsLog(shortData), signature, 12345, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("parses minimum valid data (exactly 80 bytes)", func(t *testing.T) {
		data := make([]byte, 80)
		for i := 8; i < 40; i++ {
			data[i] = 0x11
		}
		for i := 40; i < 72; i++ {
			data[i] = 0x22
		}
		binary.LittleEndian.PutUint64(data[72:80], 12345)

		event := ParseEvent(wrapAsLog(data), signature, 100, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundEvent
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Contains(t, outbound.TxID, "0x1111")
		assert.Contains(t, outbound.UniversalTxID, "0x2222")
		assert.Equal(t, "12345", outbound.GasFeeUsed)
	})

	t.Run("handles data longer than 80 bytes", func(t *testing.T) {
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

		var outbound common.OutboundEvent
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

		var outbound common.OutboundEvent
		require.NoError(t, json.Unmarshal(event.EventData, &outbound))
		assert.Equal(t, "0", outbound.GasFeeUsed)
	})

	t.Run("max uint64 gas fee", func(t *testing.T) {
		var txID, utxID [32]byte
		data := buildOutboundPayload(txID, utxID, ^uint64(0))
		event := ParseEvent(wrapAsLog(data), signature, 100, 0, EventTypeFinalizeUniversalTx, chainID, logger)
		require.NotNil(t, event)

		var outbound common.OutboundEvent
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

	t.Run("returns partial result when no data field length", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 = 100, no data_len
		data := make([]byte, 100)
		binary.LittleEndian.PutUint64(data[92:100], 777)
		result, err := decodeUniversalTxEvent(data, logger)
		require.NoError(t, err)
		assert.Equal(t, "777", result.Amount)
	})

	t.Run("returns partial result when data field exceeds available bytes", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 + 4 = 104
		data := make([]byte, 104)
		binary.LittleEndian.PutUint64(data[92:100], 555)
		binary.LittleEndian.PutUint32(data[100:104], 999) // claims 999 bytes of payload
		result, err := decodeUniversalTxEvent(data, logger)
		require.NoError(t, err)
		assert.Equal(t, "555", result.Amount)
		assert.Empty(t, result.RawPayload) // not enough data, so payload is skipped
	})

	t.Run("returns partial result when missing revert recipient", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 + 4(data_len=0) = 104
		data := make([]byte, 104)
		binary.LittleEndian.PutUint32(data[100:104], 0) // 0 length payload
		result, err := decodeUniversalTxEvent(data, logger)
		require.NoError(t, err)
		assert.Empty(t, result.RevertFundRecipient)
	})

	t.Run("returns partial result when missing tx_type", func(t *testing.T) {
		// 8 + 32 + 20 + 32 + 8 + 4(data_len=0) + 32(revert) = 136
		data := make([]byte, 136)
		binary.LittleEndian.PutUint32(data[100:104], 0)
		result, err := decodeUniversalTxEvent(data, logger)
		require.NoError(t, err)
		// tx_type defaults to 0 when missing
		assert.Equal(t, uint(0), result.TxType)
	})
}
