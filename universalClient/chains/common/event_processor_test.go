package common

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ucdb "github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestNewEventProcessor(t *testing.T) {
	t.Run("creates event processor with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "eip155:1"

		processor := NewEventProcessor(nil, nil, chainID, true, true, logger)

		require.NotNil(t, processor)
		assert.Equal(t, chainID, processor.chainID)
		assert.False(t, processor.running)
		assert.NotNil(t, processor.stopCh)
		assert.NotNil(t, processor.chainStore)
	})
}

func TestEventProcessorIsRunning(t *testing.T) {
	t.Run("returns false when not running", func(t *testing.T) {
		processor := &EventProcessor{running: false}
		assert.False(t, processor.IsRunning())
	})

	t.Run("returns true when running", func(t *testing.T) {
		processor := &EventProcessor{running: true}
		assert.True(t, processor.IsRunning())
	})
}

func TestEventProcessorStop(t *testing.T) {
	t.Run("stop when not running returns nil", func(t *testing.T) {
		processor := &EventProcessor{running: false}
		err := processor.Stop()
		assert.NoError(t, err)
	})
}

func TestEventProcessorBase58ToHex(t *testing.T) {
	logger := zerolog.Nop()
	processor := NewEventProcessor(nil, nil, "test-chain", true, true, logger)

	t.Run("empty string returns 0x", func(t *testing.T) {
		result, err := processor.base58ToHex("")
		require.NoError(t, err)
		assert.Equal(t, "0x", result)
	})

	t.Run("already hex returns as is", func(t *testing.T) {
		input := "0xabcdef1234567890"
		result, err := processor.base58ToHex(input)
		require.NoError(t, err)
		assert.Equal(t, input, result)
	})

	t.Run("valid base58 converts to hex", func(t *testing.T) {
		// "3yZe7d" is base58 for bytes [1, 2, 3, 4]
		input := "2VfUX"
		result, err := processor.base58ToHex(input)
		require.NoError(t, err)
		assert.True(t, len(result) > 2)
		assert.Equal(t, "0x", result[:2])
	})

	t.Run("invalid base58 returns error", func(t *testing.T) {
		// Base58 doesn't include 0, O, I, l
		input := "0OIl"
		_, err := processor.base58ToHex(input)
		require.Error(t, err)
	})
}

func TestEventProcessorConstructInbound(t *testing.T) {
	logger := zerolog.Nop()
	processor := NewEventProcessor(nil, nil, "eip155:1", true, true, logger)

	t.Run("nil event returns error", func(t *testing.T) {
		inbound, err := processor.constructInbound(nil)
		require.Error(t, err)
		assert.Nil(t, inbound)
		assert.Contains(t, err.Error(), "event is nil")
	})

	t.Run("nil event data returns error", func(t *testing.T) {
		event := &store.Event{
			EventID:   "0x123:0",
			EventData: nil,
		}
		inbound, err := processor.constructInbound(event)
		require.Error(t, err)
		assert.Nil(t, inbound)
		assert.Contains(t, err.Error(), "event data is missing")
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		event := &store.Event{
			EventID:   "0x123:0",
			EventData: []byte("invalid json"),
		}
		inbound, err := processor.constructInbound(event)
		require.Error(t, err)
		assert.Nil(t, inbound)
	})

	t.Run("valid event data constructs inbound", func(t *testing.T) {
		eventData := UniversalTx{
			SourceChain: "eip155:1",
			LogIndex:    5,
			Sender:      "0xsender123",
			Recipient:   "push1recipient",
			Token:       "0xtoken",
			Amount:      "1000000",
			TxType:      2, // FUNDS
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "0xabc123:5",
			EventData: eventDataBytes,
		}

		inbound, err := processor.constructInbound(event)
		require.NoError(t, err)
		require.NotNil(t, inbound)
		assert.Equal(t, "eip155:1", inbound.SourceChain)
		assert.Equal(t, "0xsender123", inbound.Sender)
		assert.Equal(t, "1000000", inbound.Amount)
		assert.Equal(t, uexecutortypes.TxType_FUNDS, inbound.TxType)
	})

	t.Run("passes all fields unconditionally to inbound", func(t *testing.T) {
		eventData := UniversalTx{
			SourceChain:         "eip155:1",
			LogIndex:            3,
			Sender:              "0xsender",
			Recipient:           "0xrecipient",
			Token:               "0xtoken",
			Amount:              "500",
			RawPayload:          "0xdeadbeef",
			VerificationData:    "0xsigdata",
			RevertFundRecipient: "0xrevert",
			TxType:              3, // FUNDS_AND_PAYLOAD
			FromCEA:             true,
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "0xtxhash:3",
			EventData: eventDataBytes,
		}

		inbound, err := processor.constructInbound(event)
		require.NoError(t, err)
		require.NotNil(t, inbound)
		assert.Equal(t, "0xrecipient", inbound.Recipient)
		assert.Equal(t, "0xdeadbeef", inbound.RawPayload)
		assert.Equal(t, "0xsigdata", inbound.VerificationData)
		assert.True(t, inbound.IsCEA)
		require.NotNil(t, inbound.RevertInstructions)
		assert.Equal(t, "0xrevert", inbound.RevertInstructions.FundRecipient)
	})

	t.Run("passes raw payload and verification data for non-payload tx types", func(t *testing.T) {
		// Core will strip these — UV just passes everything through
		eventData := UniversalTx{
			SourceChain:      "eip155:1",
			Sender:           "0xsender",
			Recipient:        "0xrecipient",
			Amount:           "1000",
			RawPayload:       "0xcafe",
			VerificationData: "0xsig",
			TxType:           2, // FUNDS (non-payload type)
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "0xhash:0",
			EventData: eventDataBytes,
		}

		inbound, err := processor.constructInbound(event)
		require.NoError(t, err)
		assert.Equal(t, "0xrecipient", inbound.Recipient)
		assert.Equal(t, "0xcafe", inbound.RawPayload)
		assert.Equal(t, "0xsig", inbound.VerificationData)
	})

	t.Run("no revert instructions when revert recipient is empty", func(t *testing.T) {
		eventData := UniversalTx{
			SourceChain:         "eip155:1",
			Sender:              "0xsender",
			Amount:              "100",
			TxType:              0, // GAS
			RevertFundRecipient: "",
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "0xhash:0",
			EventData: eventDataBytes,
		}

		inbound, err := processor.constructInbound(event)
		require.NoError(t, err)
		assert.Nil(t, inbound.RevertInstructions)
	})

	t.Run("tx type mapping", func(t *testing.T) {
		testCases := []struct {
			txType   uint
			expected uexecutortypes.TxType
		}{
			{0, uexecutortypes.TxType_GAS},
			{1, uexecutortypes.TxType_GAS_AND_PAYLOAD},
			{2, uexecutortypes.TxType_FUNDS},
			{3, uexecutortypes.TxType_FUNDS_AND_PAYLOAD},
			{99, uexecutortypes.TxType_UNSPECIFIED_TX}, // Unknown defaults to unspecified
		}

		for _, tc := range testCases {
			eventData := UniversalTx{
				SourceChain: "eip155:1",
				TxType:      tc.txType,
			}
			eventDataBytes, _ := json.Marshal(eventData)

			event := &store.Event{
				EventID:   "0xabc:0",
				EventData: eventDataBytes,
			}

			inbound, err := processor.constructInbound(event)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, inbound.TxType, "TxType %d should map to %v", tc.txType, tc.expected)
		}
	})
}

func TestEventProcessorParseOutboundEventData(t *testing.T) {
	logger := zerolog.Nop()
	processor := NewEventProcessor(nil, nil, "eip155:1", true, true, logger)

	t.Run("nil event returns error", func(t *testing.T) {
		data, err := processor.parseOutboundEventData(nil)
		require.Error(t, err)
		assert.Nil(t, data)
		assert.Contains(t, err.Error(), "event is nil")
	})

	t.Run("empty event data returns error", func(t *testing.T) {
		event := &store.Event{
			EventID:   "test",
			EventData: []byte{},
		}
		data, err := processor.parseOutboundEventData(event)
		require.Error(t, err)
		assert.Nil(t, data)
		assert.Contains(t, err.Error(), "event data is empty")
	})

	t.Run("valid outbound event extracts IDs and gas fee", func(t *testing.T) {
		eventData := OutboundEvent{
			TxID:          "0x1234",
			UniversalTxID: "0xabcd",
			GasFeeUsed:    "42000000000000",
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "test",
			EventData: eventDataBytes,
		}

		data, err := processor.parseOutboundEventData(event)
		require.NoError(t, err)
		assert.Equal(t, "0x1234", data.TxID)
		assert.Equal(t, "0xabcd", data.UniversalTxID)
		assert.Equal(t, "42000000000000", data.GasFeeUsed)
	})

	t.Run("missing tx_id returns error", func(t *testing.T) {
		eventData := OutboundEvent{
			TxID:          "",
			UniversalTxID: "0xabcd",
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "test",
			EventData: eventDataBytes,
		}

		data, err := processor.parseOutboundEventData(event)
		require.Error(t, err)
		assert.Nil(t, data)
		assert.Contains(t, err.Error(), "tx_id not found")
	})

	t.Run("missing universal_tx_id returns error", func(t *testing.T) {
		eventData := OutboundEvent{
			TxID:          "0x1234",
			UniversalTxID: "",
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "test",
			EventData: eventDataBytes,
		}

		data, err := processor.parseOutboundEventData(event)
		require.Error(t, err)
		assert.Nil(t, data)
		assert.Contains(t, err.Error(), "universal_tx_id not found")
	})
}

func TestEventProcessorBuildOutboundObservation(t *testing.T) {
	logger := zerolog.Nop()
	processor := NewEventProcessor(nil, nil, "eip155:1", true, true, logger)

	t.Run("builds observation with gas fee from parsed data", func(t *testing.T) {
		outboundData := &OutboundEvent{
			TxID:          "0x1234",
			UniversalTxID: "0xabcd",
			GasFeeUsed:    "42000000000000",
		}

		event := &store.Event{
			EventID:     "0xabc123:5",
			BlockHeight: 12345,
		}

		obs, err := processor.buildOutboundObservation(event, outboundData)
		require.NoError(t, err)
		require.NotNil(t, obs)
		assert.True(t, obs.Success)
		assert.Equal(t, uint64(12345), obs.BlockHeight)
		assert.Equal(t, "0xabc123", obs.TxHash)
		assert.Equal(t, "42000000000000", obs.GasFeeUsed)
	})

	t.Run("missing gas fee defaults to 0", func(t *testing.T) {
		outboundData := &OutboundEvent{
			TxID:          "0x1234",
			UniversalTxID: "0xabcd",
		}

		event := &store.Event{
			EventID:     "0xabc123:5",
			BlockHeight: 12345,
		}

		obs, err := processor.buildOutboundObservation(event, outboundData)
		require.NoError(t, err)
		require.NotNil(t, obs)
		assert.Equal(t, "0", obs.GasFeeUsed)
	})

	t.Run("handles base58 tx hash", func(t *testing.T) {
		outboundData := &OutboundEvent{
			TxID:          "0x1234",
			UniversalTxID: "0xabcd",
		}

		event := &store.Event{
			EventID:     "2VfUX:0", // Base58 encoded
			BlockHeight: 100,
		}

		obs, err := processor.buildOutboundObservation(event, outboundData)
		require.NoError(t, err)
		require.NotNil(t, obs)
		assert.True(t, len(obs.TxHash) >= 2)
	})
}

func TestProcessOutboundEvent(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	setupDB := func(t *testing.T) *ucdb.DB {
		t.Helper()
		database, err := ucdb.OpenInMemoryDB(true)
		require.NoError(t, err)
		return database
	}

	t.Run("nil event data returns parse error", func(t *testing.T) {
		database := setupDB(t)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		event := &store.Event{
			EventID:   "0xabc:0",
			EventData: nil,
		}
		err := ep.processOutboundEvent(ctx, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse outbound event data")
	})

	t.Run("empty event data returns parse error", func(t *testing.T) {
		database := setupDB(t)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		event := &store.Event{
			EventID:   "0xabc:0",
			EventData: []byte{},
		}
		err := ep.processOutboundEvent(ctx, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse outbound event data")
	})

	t.Run("invalid JSON event data returns parse error", func(t *testing.T) {
		database := setupDB(t)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		event := &store.Event{
			EventID:   "0xabc:0",
			EventData: []byte("not json"),
		}
		err := ep.processOutboundEvent(ctx, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse outbound event data")
	})

	t.Run("missing tx_id returns parse error", func(t *testing.T) {
		database := setupDB(t)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		eventData, _ := json.Marshal(OutboundEvent{
			TxID:          "",
			UniversalTxID: "0xutxid",
		})
		event := &store.Event{
			EventID:   "0xabc:0",
			EventData: eventData,
		}
		err := ep.processOutboundEvent(ctx, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse outbound event data")
	})

	t.Run("missing universal_tx_id returns parse error", func(t *testing.T) {
		database := setupDB(t)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		eventData, _ := json.Marshal(OutboundEvent{
			TxID:          "0xtxid",
			UniversalTxID: "",
		})
		event := &store.Event{
			EventID:   "0xabc:0",
			EventData: eventData,
		}
		err := ep.processOutboundEvent(ctx, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse outbound event data")
	})
}

func TestProcessInboundEvent(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	setupDB := func(t *testing.T) *ucdb.DB {
		t.Helper()
		database, err := ucdb.OpenInMemoryDB(true)
		require.NoError(t, err)
		return database
	}

	t.Run("nil event data returns construct error", func(t *testing.T) {
		database := setupDB(t)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		event := &store.Event{
			EventID:   "0xabc:0",
			EventData: nil,
		}
		err := ep.processInboundEvent(ctx, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to construct inbound")
	})

	t.Run("invalid JSON event data returns construct error", func(t *testing.T) {
		database := setupDB(t)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		event := &store.Event{
			EventID:   "0xabc:0",
			EventData: []byte("{not valid json}"),
		}
		err := ep.processInboundEvent(ctx, event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to construct inbound")
	})
}

func TestProcessConfirmedEventsRouting(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	setupDB := func(t *testing.T, events []store.Event) *ucdb.DB {
		t.Helper()
		database, err := ucdb.OpenInMemoryDB(true)
		require.NoError(t, err)
		for _, e := range events {
			result := database.Client().Create(&e)
			require.NoError(t, result.Error)
		}
		return database
	}

	t.Run("no confirmed events returns nil", func(t *testing.T) {
		database := setupDB(t, nil)
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)
	})

	t.Run("only pending events are ignored", func(t *testing.T) {
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xpending:0",
				Status:    store.StatusPending,
				Type:      store.EventTypeInbound,
				EventData: []byte("{}"),
			},
		})
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Event should remain PENDING (not picked up)
		var evt store.Event
		database.Client().Where("event_id = ?", "0xpending:0").First(&evt)
		assert.Equal(t, store.StatusPending, evt.Status)
	})

	t.Run("inbound with bad data fails gracefully and continues to next event", func(t *testing.T) {
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xbad_inbound:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeInbound,
				EventData: []byte("not json"),
			},
			{
				EventID:   "0xbad_inbound2:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeInbound,
				EventData: nil,
			},
		})
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		// Should not return error - errors on individual events are logged and skipped
		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Both events should remain CONFIRMED (failed to process, not updated)
		var evt1, evt2 store.Event
		database.Client().Where("event_id = ?", "0xbad_inbound:0").First(&evt1)
		assert.Equal(t, store.StatusConfirmed, evt1.Status)
		database.Client().Where("event_id = ?", "0xbad_inbound2:0").First(&evt2)
		assert.Equal(t, store.StatusConfirmed, evt2.Status)
	})

	t.Run("outbound with bad data fails gracefully and continues to next event", func(t *testing.T) {
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xbad_outbound:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeOutbound,
				EventData: []byte("not json"),
			},
			{
				EventID:   "0xbad_outbound2:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeOutbound,
				EventData: []byte{},
			},
		})
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Both events should remain CONFIRMED
		var evt1, evt2 store.Event
		database.Client().Where("event_id = ?", "0xbad_outbound:0").First(&evt1)
		assert.Equal(t, store.StatusConfirmed, evt1.Status)
		database.Client().Where("event_id = ?", "0xbad_outbound2:0").First(&evt2)
		assert.Equal(t, store.StatusConfirmed, evt2.Status)
	})

	t.Run("mixed inbound and outbound with bad data both fail gracefully", func(t *testing.T) {
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xin:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeInbound,
				EventData: []byte("bad"),
			},
			{
				EventID:   "0xout:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeOutbound,
				EventData: []byte("bad"),
			},
		})
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		var inEvt, outEvt store.Event
		database.Client().Where("event_id = ?", "0xin:0").First(&inEvt)
		assert.Equal(t, store.StatusConfirmed, inEvt.Status)
		database.Client().Where("event_id = ?", "0xout:0").First(&outEvt)
		assert.Equal(t, store.StatusConfirmed, outEvt.Status)
	})

	t.Run("outbound missing tx_id in valid JSON stays CONFIRMED", func(t *testing.T) {
		eventData, _ := json.Marshal(OutboundEvent{
			TxID:          "",
			UniversalTxID: "0xutxid",
		})
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xno_txid:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeOutbound,
				EventData: eventData,
			},
		})
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		var evt store.Event
		database.Client().Where("event_id = ?", "0xno_txid:0").First(&evt)
		assert.Equal(t, store.StatusConfirmed, evt.Status)
	})

	t.Run("unknown event type is silently skipped", func(t *testing.T) {
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xunknown:0",
				Status:    store.StatusConfirmed,
				Type:      "UNKNOWN_TYPE",
				EventData: []byte("{}"),
			},
		})
		defer database.Close()
		ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Event should remain CONFIRMED (no handler for this type)
		var evt store.Event
		database.Client().Where("event_id = ?", "0xunknown:0").First(&evt)
		assert.Equal(t, store.StatusConfirmed, evt.Status)
	})
}

func TestProcessLoopContextCancellation(t *testing.T) {
	logger := zerolog.Nop()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

	t.Run("processLoop exits promptly on context cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		err := ep.Start(ctx)
		require.NoError(t, err)
		assert.True(t, ep.IsRunning())

		// Cancel context and wait for stop
		cancel()

		// The wg.Wait inside Stop() will block until processLoop exits
		done := make(chan struct{})
		go func() {
			ep.Stop()
			close(done)
		}()

		select {
		case <-done:
			// processLoop exited within reasonable time
		case <-time.After(10 * time.Second):
			t.Fatal("processLoop did not exit within 10 seconds after context cancellation")
		}

		assert.False(t, ep.IsRunning())
	})
}

func TestProcessLoopStopChannel(t *testing.T) {
	logger := zerolog.Nop()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

	t.Run("processLoop exits promptly on stop signal", func(t *testing.T) {
		ctx := context.Background()

		err := ep.Start(ctx)
		require.NoError(t, err)
		assert.True(t, ep.IsRunning())

		done := make(chan struct{})
		go func() {
			ep.Stop()
			close(done)
		}()

		select {
		case <-done:
			// processLoop exited promptly
		case <-time.After(10 * time.Second):
			t.Fatal("processLoop did not exit within 10 seconds after stop signal")
		}

		assert.False(t, ep.IsRunning())
	})
}

func TestProcessConfirmedEventsDBError(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("nil database returns error", func(t *testing.T) {
		ep := &EventProcessor{
			chainStore:      NewChainStore(nil),
			logger:          logger,
			chainID:         "eip155:1",
			inboundEnabled:  true,
			outboundEnabled: true,
		}

		err := ep.processConfirmedEvents(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get confirmed events")
	})
}

func TestEventProcessorStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		ep := &EventProcessor{}
		assert.Nil(t, ep.signer)
		assert.Nil(t, ep.chainStore)
		assert.Empty(t, ep.chainID)
		assert.False(t, ep.running)
		assert.Nil(t, ep.stopCh)
		assert.False(t, ep.inboundEnabled)
		assert.False(t, ep.outboundEnabled)
	})
}

func TestNewEventProcessorEnabledFlags(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("both enabled", func(t *testing.T) {
		ep := NewEventProcessor(nil, nil, "eip155:1", true, true, logger)
		assert.True(t, ep.inboundEnabled)
		assert.True(t, ep.outboundEnabled)
	})

	t.Run("inbound only", func(t *testing.T) {
		ep := NewEventProcessor(nil, nil, "eip155:1", true, false, logger)
		assert.True(t, ep.inboundEnabled)
		assert.False(t, ep.outboundEnabled)
	})

	t.Run("outbound only", func(t *testing.T) {
		ep := NewEventProcessor(nil, nil, "eip155:1", false, true, logger)
		assert.False(t, ep.inboundEnabled)
		assert.True(t, ep.outboundEnabled)
	})

	t.Run("both disabled", func(t *testing.T) {
		ep := NewEventProcessor(nil, nil, "eip155:1", false, false, logger)
		assert.False(t, ep.inboundEnabled)
		assert.False(t, ep.outboundEnabled)
	})
}

func TestEventProcessorStartDoubleStart(t *testing.T) {
	logger := zerolog.Nop()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First start should succeed
	err = ep.Start(ctx)
	require.NoError(t, err)
	assert.True(t, ep.IsRunning())

	// Second start should be rejected
	err = ep.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
	assert.True(t, ep.IsRunning())

	// Clean up
	err = ep.Stop()
	require.NoError(t, err)
	assert.False(t, ep.IsRunning())
}

func TestEventProcessorStopIdempotent(t *testing.T) {
	logger := zerolog.Nop()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the processor
	err = ep.Start(ctx)
	require.NoError(t, err)
	assert.True(t, ep.IsRunning())

	// First stop
	err = ep.Stop()
	require.NoError(t, err)
	assert.False(t, ep.IsRunning())

	// Second stop should be idempotent (no error, no panic)
	err = ep.Stop()
	require.NoError(t, err)
	assert.False(t, ep.IsRunning())

	// Third stop also fine
	err = ep.Stop()
	require.NoError(t, err)
}

func TestEventProcessorIsRunningStateTransitions(t *testing.T) {
	logger := zerolog.Nop()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

	// Initial state: not running
	assert.False(t, ep.IsRunning())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// After start: running
	err = ep.Start(ctx)
	require.NoError(t, err)
	assert.True(t, ep.IsRunning())

	// After stop: not running
	err = ep.Stop()
	require.NoError(t, err)
	assert.False(t, ep.IsRunning())

	// Can restart after stop
	err = ep.Start(ctx)
	require.NoError(t, err)
	assert.True(t, ep.IsRunning())

	// Clean up
	err = ep.Stop()
	require.NoError(t, err)
	assert.False(t, ep.IsRunning())
}

func TestEventProcessorStopViaContextCancel(t *testing.T) {
	logger := zerolog.Nop()
	database, err := ucdb.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer database.Close()

	ep := NewEventProcessor(nil, database, "eip155:1", true, true, logger)

	ctx, cancel := context.WithCancel(context.Background())

	err = ep.Start(ctx)
	require.NoError(t, err)
	assert.True(t, ep.IsRunning())

	// Cancel context - the processLoop should exit
	cancel()

	// Stop should still work cleanly after context cancellation
	err = ep.Stop()
	require.NoError(t, err)
	assert.False(t, ep.IsRunning())
}

func TestProcessConfirmedEventsEnabledFlags(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	// Helper to create an in-memory DB and seed confirmed events
	setupDB := func(t *testing.T, events []store.Event) *ucdb.DB {
		t.Helper()
		database, err := ucdb.OpenInMemoryDB(true)
		require.NoError(t, err)
		for _, e := range events {
			result := database.Client().Create(&e)
			require.NoError(t, result.Error)
		}
		return database
	}

	inboundEventData, _ := json.Marshal(UniversalTx{
		SourceChain: "eip155:1",
		Sender:      "0xsender",
		Amount:      "1000",
		TxType:      2,
	})

	outboundEventData, _ := json.Marshal(OutboundEvent{
		TxID:          "0xtxid",
		UniversalTxID: "0xutxid",
	})

	makeEvents := func() []store.Event {
		return []store.Event{
			{
				EventID:   "0xaaa:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeInbound,
				EventData: inboundEventData,
			},
			{
				EventID:   "0xbbb:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeOutbound,
				EventData: outboundEventData,
			},
		}
	}

	t.Run("inbound disabled skips inbound events, leaves them CONFIRMED", func(t *testing.T) {
		database := setupDB(t, makeEvents())
		// inbound=false, outbound=false (no signer so outbound will also fail to vote, but that's ok)
		ep := NewEventProcessor(nil, database, "eip155:1", false, false, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Inbound event should still be CONFIRMED (skipped, not processed)
		var inboundEvt store.Event
		database.Client().Where("event_id = ?", "0xaaa:0").First(&inboundEvt)
		assert.Equal(t, store.StatusConfirmed, inboundEvt.Status)
	})

	t.Run("outbound disabled skips outbound events, leaves them CONFIRMED", func(t *testing.T) {
		database := setupDB(t, makeEvents())
		ep := NewEventProcessor(nil, database, "eip155:1", false, false, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Outbound event should still be CONFIRMED (skipped, not processed)
		var outboundEvt store.Event
		database.Client().Where("event_id = ?", "0xbbb:0").First(&outboundEvt)
		assert.Equal(t, store.StatusConfirmed, outboundEvt.Status)
	})

	t.Run("inbound enabled but outbound disabled skips only outbound", func(t *testing.T) {
		// Seed only outbound events so we don't hit nil signer panic on inbound
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xbbb:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeOutbound,
				EventData: outboundEventData,
			},
		})
		ep := NewEventProcessor(nil, database, "eip155:1", true, false, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Outbound event should still be CONFIRMED (skipped due to outbound disabled)
		var outboundEvt store.Event
		database.Client().Where("event_id = ?", "0xbbb:0").First(&outboundEvt)
		assert.Equal(t, store.StatusConfirmed, outboundEvt.Status)
	})

	t.Run("outbound enabled but inbound disabled skips only inbound", func(t *testing.T) {
		// Seed only inbound events so we don't hit nil signer panic on outbound
		database := setupDB(t, []store.Event{
			{
				EventID:   "0xaaa:0",
				Status:    store.StatusConfirmed,
				Type:      store.EventTypeInbound,
				EventData: inboundEventData,
			},
		})
		ep := NewEventProcessor(nil, database, "eip155:1", false, true, logger)

		err := ep.processConfirmedEvents(ctx)
		require.NoError(t, err)

		// Inbound event should still be CONFIRMED (skipped due to inbound disabled)
		var inboundEvt store.Event
		database.Client().Where("event_id = ?", "0xaaa:0").First(&inboundEvt)
		assert.Equal(t, store.StatusConfirmed, inboundEvt.Status)
	})
}
