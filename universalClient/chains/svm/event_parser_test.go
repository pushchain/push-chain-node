package svm

import (
	"encoding/base64"
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

	// Helper to create base64-encoded log data
	createLogData := func(discriminator []byte, txID []byte, universalTxID []byte) string {
		data := make([]byte, 0, 72)
		data = append(data, discriminator...)
		data = append(data, txID...)
		data = append(data, universalTxID...)
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
			log:       createLogData(discriminator, txID, universalTxID),
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				assert.Contains(t, event.EventID, signature)
				assert.Equal(t, uint64(12345), event.BlockHeight)
				assert.Equal(t, common.EventTypeOutbound, event.Type)
				assert.Equal(t, "PENDING", event.Status)
				assert.Equal(t, "STANDARD", event.ConfirmationType)

				// Verify event data contains tx_id and universal_tx_id
				assert.NotNil(t, event.EventData)
				var outboundData map[string]any
				err := json.Unmarshal(event.EventData, &outboundData)
				require.NoError(t, err)

				expectedTxID := "0x" + hex.EncodeToString(txID)
				expectedUniversalTxID := "0x" + hex.EncodeToString(universalTxID)

				assert.Equal(t, expectedTxID, outboundData["tx_id"])
				assert.Equal(t, expectedUniversalTxID, outboundData["universal_tx_id"])
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
			name: "returns nil for data too short (less than 72 bytes)",
			log: func() string {
				// Only 64 bytes (8 discriminator + 32 txID, missing universalTxID)
				shortData := make([]byte, 64)
				copy(shortData[:8], discriminator)
				copy(shortData[8:40], txID)
				return "Program data: " + base64.StdEncoding.EncodeToString(shortData)
			}(),
			wantEvent: false,
		},
		{
			name: "correctly parses minimum valid data (exactly 72 bytes)",
			log: func() string {
				exactData := make([]byte, 72)
				copy(exactData[:8], discriminator)
				for i := 8; i < 40; i++ {
					exactData[i] = 0x11 // txID
				}
				for i := 40; i < 72; i++ {
					exactData[i] = 0x22 // universalTxID
				}
				return "Program data: " + base64.StdEncoding.EncodeToString(exactData)
			}(),
			wantEvent: true,
			validate: func(t *testing.T, event *store.Event) {
				var outboundData map[string]any
				err := json.Unmarshal(event.EventData, &outboundData)
				require.NoError(t, err)

				// Verify the values are correctly parsed
				assert.Contains(t, outboundData["tx_id"], "0x1111")
				assert.Contains(t, outboundData["universal_tx_id"], "0x2222")
			},
		},
		{
			name: "handles data longer than 72 bytes",
			log: func() string {
				// 100 bytes - extra data after the required fields should be ignored
				longData := make([]byte, 100)
				copy(longData[:8], discriminator)
				copy(longData[8:40], txID)
				copy(longData[40:72], universalTxID)
				// Extra bytes at the end
				for i := 72; i < 100; i++ {
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
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseEvent(tt.log, signature, 12345, 0, EventTypeOutboundObservation, chainID, logger)

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

	// Create valid outbound data
	data := make([]byte, 72)
	for i := 0; i < 8; i++ {
		data[i] = byte(i) // discriminator
	}
	for i := 8; i < 72; i++ {
		data[i] = byte(i % 256) // txID and universalTxID
	}
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
			event := ParseEvent(log, tt.signature, tt.slot, tt.logIndex, EventTypeOutboundObservation, chainID, logger)
			require.NotNil(t, event)
			assert.Equal(t, tt.wantID, event.EventID)
			assert.Equal(t, tt.slot, event.BlockHeight)
		})
	}
}
