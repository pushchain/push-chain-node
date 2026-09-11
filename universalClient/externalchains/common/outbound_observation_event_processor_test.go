package common

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

func TestOutboundParseOutboundEventData(t *testing.T) {
	processor := NewOutboundObservationEventProcessor(nil, nil, zerolog.Nop())

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
		eventData := OutboundObservation{
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
		eventData := OutboundObservation{
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
		eventData := OutboundObservation{
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

func TestOutboundBuildOutboundObservation(t *testing.T) {
	processor := NewOutboundObservationEventProcessor(nil, nil, zerolog.Nop())

	t.Run("builds observation with gas fee from parsed data", func(t *testing.T) {
		outboundData := &OutboundObservation{
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
		outboundData := &OutboundObservation{
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
		outboundData := &OutboundObservation{
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
		assert.Equal(t, "0x", obs.TxHash[:2])
	})
}

func TestOutboundHandleEvent(t *testing.T) {
	ctx := context.Background()

	t.Run("parse failure returns error", func(t *testing.T) {
		database := newTestDB(t)
		processor := NewOutboundObservationEventProcessor(&fakeVoteSigner{txHash: "0xvote"}, database, zerolog.Nop())

		err := processor.HandleEvent(ctx, &store.Event{EventID: "0xbad:0", EventData: []byte("not json")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse outbound event data")
	})

	t.Run("vote failure returns error", func(t *testing.T) {
		database := newTestDB(t)
		processor := NewOutboundObservationEventProcessor(&fakeVoteSigner{err: fmt.Errorf("broadcast failed")}, database, zerolog.Nop())
		eventData, _ := json.Marshal(OutboundObservation{TxID: "0xtxid", UniversalTxID: "0xutxid"})

		err := processor.HandleEvent(ctx, &store.Event{EventID: "0xout:0", EventData: eventData})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to vote on outbound")
	})

	t.Run("successful vote marks event completed", func(t *testing.T) {
		database := newTestDB(t)
		signer := &fakeVoteSigner{txHash: "0xvote"}
		processor := NewOutboundObservationEventProcessor(signer, database, zerolog.Nop())
		eventData, _ := json.Marshal(OutboundObservation{TxID: "0xtxid", UniversalTxID: "0xutxid"})
		seedConfirmedEvent(t, database, "0xout:0", store.EventTypeOutbound, eventData)

		err := processor.HandleEvent(ctx, &store.Event{EventID: "0xout:0", Type: store.EventTypeOutbound, EventData: eventData})
		require.NoError(t, err)
		assert.Equal(t, 1, signer.outboundVotes)

		rows, err := NewChainStore(database).UpdateEventStatus("0xout:0", store.StatusCompleted, store.StatusCompleted)
		require.NoError(t, err)
		assert.Equal(t, int64(1), rows)
	})
}

// A revert observation carries no gas fee from the chain: the gateway dropped
// gas_used from RevertUniversalTx. The vote must still say "0" rather than empty,
// because core rejects an empty gas_fee_used outright and the value is part of
// the outbound ballot key, so every validator has to produce the same one.
func TestBuildOutboundObservation_EmptyGasFeeUsedBecomesZero(t *testing.T) {
	processor := NewOutboundObservationEventProcessor(nil, nil, zerolog.Nop())
	event := &store.Event{EventID: "sig:0", BlockHeight: 100}

	obs, err := processor.buildOutboundObservation(event, &OutboundObservation{
		TxID:          "0x1234",
		UniversalTxID: "0xabcd",
		// GasFeeUsed intentionally unset, as a revert leaves it.
	})
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.Equal(t, "0", obs.GasFeeUsed, "an empty gas fee must not reach the vote")

	// Same input twice must give the same value: it feeds the ballot key, so a
	// non-deterministic default would split validators across ballots.
	again, err := processor.buildOutboundObservation(event, &OutboundObservation{
		TxID: "0x1234", UniversalTxID: "0xabcd",
	})
	require.NoError(t, err)
	assert.Equal(t, obs.GasFeeUsed, again.GasFeeUsed)
}
