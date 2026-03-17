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

func TestParseOutboundObservationEvent(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	chainID := "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	signature := "5wHu1qwD7q5xMkZxq6z2S3r4y5N7m8P9kL0jH1gF2dE3cB4aA5b6C7d8E9f0G1h2"

	// Helper to create base64-encoded log data with gas_fee
	createLogData := func(discriminator []byte, txID []byte, universalTxID []byte, gasFee uint64) string {
		data := make([]byte, 0, 80)
		data = append(data, discriminator...)
		data = append(data, txID...)
		data = append(data, universalTxID...)
		gasFeeBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(gasFeeBytes, gasFee)
		data = append(data, gasFeeBytes...)
		return "Program data: " + base64.StdEncoding.EncodeToString(data)
	}

	// Example discriminator (8 bytes)
	discriminator := make([]byte, 8)
	for i := range discriminator {
		discriminator[i] = byte(i + 1) // 0x01, 0x02, ..., 0x08
	}

	// Example txID (32 bytes)
	txID := make([]byte, 32)
	for i := range txID {
		txID[i] = byte(0xAA)
	}

	// Example universalTxID (32 bytes)
	universalTxID := make([]byte, 32)
	for i := range universalTxID {
		universalTxID[i] = byte(0xBB)
	}

	tests := []struct {
		name      string
		log       string
		wantEvent bool
		validate  func(*testing.T, *store.Event)
	}{
		{
			name:      "parses valid outbound observation event",
			log:       createLogData(discriminator, txID, universalTxID, 5000),
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				assert.Contains(t, event.EventID, signature)
				assert.Equal(t, uint64(12345), event.BlockHeight)
				assert.Equal(t, common.EventTypeOutbound, event.Type)
				assert.Equal(t, "PENDING", event.Status)
				assert.Equal(t, "STANDARD", event.ConfirmationType)

				// Verify event data contains tx_id, universal_tx_id, and gas_fee_used
				assert.NotNil(t, event.EventData)
				var outboundData map[string]any
				err := json.Unmarshal(event.EventData, &outboundData)
				require.NoError(t, err)

				expectedTxID := "0x" + hex.EncodeToString(txID)
				expectedUniversalTxID := "0x" + hex.EncodeToString(universalTxID)

				assert.Equal(t, expectedTxID, outboundData["tx_id"])
				assert.Equal(t, expectedUniversalTxID, outboundData["universal_tx_id"])
				assert.Equal(t, "5000", outboundData["gas_fee_used"])
			},
		},
		{
			name:      "returns nil for log without Program data prefix",
			log:       "Some other log message",
			wantEvent: false,
		},
		{
			name:      "returns nil for empty log",
			log:       "",
			wantEvent: false,
		},
		{
			name:      "returns nil for invalid base64",
			log:       "Program data: not-valid-base64!!!",
			wantEvent: false,
		},
		{
			name: "returns nil for data too short (less than 80 bytes)",
			log: func() string {
				// Only 72 bytes (8 discriminator + 32 txID + 32 universalTxID, missing gas_fee)
				shortData := make([]byte, 72)
				copy(shortData[:8], discriminator)
				copy(shortData[8:40], txID)
				copy(shortData[40:72], universalTxID)
				return "Program data: " + base64.StdEncoding.EncodeToString(shortData)
			}(),
			wantEvent: false,
		},
		{
			name: "correctly parses minimum valid data (exactly 80 bytes)",
			log: func() string {
				exactData := make([]byte, 80)
				copy(exactData[:8], discriminator)
				for i := 8; i < 40; i++ {
					exactData[i] = 0x11 // txID
				}
				for i := 40; i < 72; i++ {
					exactData[i] = 0x22 // universalTxID
				}
				binary.LittleEndian.PutUint64(exactData[72:80], 12345) // gas_fee
				return "Program data: " + base64.StdEncoding.EncodeToString(exactData)
			}(),
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				var outboundData map[string]any
				err := json.Unmarshal(event.EventData, &outboundData)
				require.NoError(t, err)

				assert.Contains(t, outboundData["tx_id"], "0x1111")
				assert.Contains(t, outboundData["universal_tx_id"], "0x2222")
				assert.Equal(t, "12345", outboundData["gas_fee_used"])
			},
		},
		{
			name: "handles data longer than 80 bytes",
			log: func() string {
				// 120 bytes - extra data after the required fields should be ignored
				longData := make([]byte, 120)
				copy(longData[:8], discriminator)
				copy(longData[8:40], txID)
				copy(longData[40:72], universalTxID)
				binary.LittleEndian.PutUint64(longData[72:80], 9999) // gas_fee
				// Extra bytes at the end
				for i := 80; i < 120; i++ {
					longData[i] = 0xFF
				}
				return "Program data: " + base64.StdEncoding.EncodeToString(longData)
			}(),
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				var outboundData map[string]any
				err := json.Unmarshal(event.EventData, &outboundData)
				require.NoError(t, err)

				expectedTxID := "0x" + hex.EncodeToString(txID)
				expectedUniversalTxID := "0x" + hex.EncodeToString(universalTxID)

				assert.Equal(t, expectedTxID, outboundData["tx_id"])
				assert.Equal(t, expectedUniversalTxID, outboundData["universal_tx_id"])
				assert.Equal(t, "9999", outboundData["gas_fee_used"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseEvent(tt.log, signature, 12345, 0, EventTypeFinalizeUniversalTx, chainID, logger)

			if tt.wantEvent {
				require.NotNil(t, event)
				if tt.validate != nil {
					tt.validate(t, event)
				}
			} else {
				assert.Nil(t, event)
			}
		})
	}
}

func TestParseEvent_EventTypes(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	chainID := "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	signature := "testSignature123"

	t.Run("returns nil for unknown event type", func(t *testing.T) {
		log := "Program data: " + base64.StdEncoding.EncodeToString(make([]byte, 100))
		event := ParseEvent(log, signature, 12345, 0, "unknownEventType", chainID, logger)
		assert.Nil(t, event)
	})

	t.Run("returns nil for empty event type", func(t *testing.T) {
		log := "Program data: " + base64.StdEncoding.EncodeToString(make([]byte, 100))
		event := ParseEvent(log, signature, 12345, 0, "", chainID, logger)
		assert.Nil(t, event)
	})
}

func TestParseOutboundObservationEvent_EventIDFormat(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	chainID := "solana:devnet"

	// Create valid outbound data (80 bytes minimum: 8 disc + 32 txID + 32 utxID + 8 gas_fee)
	data := make([]byte, 80)
	for i := 0; i < 8; i++ {
		data[i] = byte(i) // discriminator
	}
	for i := 8; i < 72; i++ {
		data[i] = byte(i % 256) // txID and universalTxID
	}
	binary.LittleEndian.PutUint64(data[72:80], 0) // gas_fee
	log := "Program data: " + base64.StdEncoding.EncodeToString(data)

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
}
