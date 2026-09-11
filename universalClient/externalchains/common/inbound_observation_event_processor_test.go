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
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestInboundBuildInboundObservation(t *testing.T) {
	processor := NewInboundObservationEventProcessor(nil, nil, zerolog.Nop())

	t.Run("nil event returns error", func(t *testing.T) {
		inbound, err := processor.buildInboundObservation(nil)
		require.Error(t, err)
		assert.Nil(t, inbound)
		assert.Contains(t, err.Error(), "event is nil")
	})

	t.Run("nil event data returns error", func(t *testing.T) {
		event := &store.Event{
			EventID:   "0x123:0",
			EventData: nil,
		}
		inbound, err := processor.buildInboundObservation(event)
		require.Error(t, err)
		assert.Nil(t, inbound)
		assert.Contains(t, err.Error(), "event data is missing")
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		event := &store.Event{
			EventID:   "0x123:0",
			EventData: []byte("invalid json"),
		}
		inbound, err := processor.buildInboundObservation(event)
		require.Error(t, err)
		assert.Nil(t, inbound)
	})

	t.Run("valid event data constructs inbound", func(t *testing.T) {
		eventData := InboundObservation{
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

		inbound, err := processor.buildInboundObservation(event)
		require.NoError(t, err)
		require.NotNil(t, inbound)
		assert.Equal(t, "eip155:1", inbound.SourceChain)
		assert.Equal(t, "0xsender123", inbound.Sender)
		assert.Equal(t, "1000000", inbound.Amount)
		assert.Equal(t, "0xabc123", inbound.TxHash)
		assert.Equal(t, uexecutortypes.TxType_FUNDS, inbound.TxType)
	})

	t.Run("passes all fields unconditionally to inbound", func(t *testing.T) {
		eventData := InboundObservation{
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

		inbound, err := processor.buildInboundObservation(event)
		require.NoError(t, err)
		require.NotNil(t, inbound)
		assert.Equal(t, "0xrecipient", inbound.Recipient)
		assert.Equal(t, "0xdeadbeef", inbound.RawPayload)
		assert.Equal(t, "0xsigdata", inbound.VerificationData)
		assert.True(t, inbound.IsCEA)
		require.NotNil(t, inbound.RevertInstructions)
		assert.Equal(t, "0xrevert", inbound.RevertInstructions.FundRecipient)
	})

	t.Run("no revert instructions when revert recipient is empty", func(t *testing.T) {
		eventData := InboundObservation{
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

		inbound, err := processor.buildInboundObservation(event)
		require.NoError(t, err)
		assert.Nil(t, inbound.RevertInstructions)
	})

	t.Run("falls back verification data to tx hash", func(t *testing.T) {
		eventData := InboundObservation{
			SourceChain:      "eip155:1",
			VerificationData: "",
			TxType:           0,
		}
		eventDataBytes, _ := json.Marshal(eventData)

		event := &store.Event{
			EventID:   "0xhash:0",
			EventData: eventDataBytes,
		}

		inbound, err := processor.buildInboundObservation(event)
		require.NoError(t, err)
		assert.Equal(t, "0xhash", inbound.VerificationData)
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
			eventData := InboundObservation{
				SourceChain: "eip155:1",
				TxType:      tc.txType,
			}
			eventDataBytes, _ := json.Marshal(eventData)

			event := &store.Event{
				EventID:   "0xabc:0",
				EventData: eventDataBytes,
			}

			inbound, err := processor.buildInboundObservation(event)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, inbound.TxType, "TxType %d should map to %v", tc.txType, tc.expected)
		}
	})
}

func TestInboundHandleEvent(t *testing.T) {
	ctx := context.Background()

	t.Run("construct failure returns error, event stays CONFIRMED", func(t *testing.T) {
		database := newTestDB(t)
		processor := NewInboundObservationEventProcessor(&fakeVoteSigner{txHash: "0xvote"}, database, zerolog.Nop())
		seedConfirmedEvent(t, database, "0xbad:0", store.EventTypeInbound, []byte("not json"))

		err := processor.HandleEvent(ctx, &store.Event{EventID: "0xbad:0", EventData: []byte("not json")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to build inbound observation")
	})

	t.Run("vote failure returns error", func(t *testing.T) {
		database := newTestDB(t)
		processor := NewInboundObservationEventProcessor(&fakeVoteSigner{err: fmt.Errorf("broadcast failed")}, database, zerolog.Nop())
		eventData, _ := json.Marshal(InboundObservation{SourceChain: "eip155:1", TxType: 0})

		err := processor.HandleEvent(ctx, &store.Event{EventID: "0xin:0", EventData: eventData})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to vote on inbound")
	})

	t.Run("successful vote marks event completed", func(t *testing.T) {
		database := newTestDB(t)
		signer := &fakeVoteSigner{txHash: "0xvote"}
		processor := NewInboundObservationEventProcessor(signer, database, zerolog.Nop())
		eventData, _ := json.Marshal(InboundObservation{SourceChain: "eip155:1", TxType: 0})
		seedConfirmedEvent(t, database, "0xin:0", store.EventTypeInbound, eventData)

		err := processor.HandleEvent(ctx, &store.Event{EventID: "0xin:0", Type: store.EventTypeInbound, EventData: eventData})
		require.NoError(t, err)
		assert.Equal(t, 1, signer.inboundVotes)

		rows, err := NewChainStore(database).UpdateEventStatus("0xin:0", store.StatusCompleted, store.StatusCompleted)
		require.NoError(t, err)
		assert.Equal(t, int64(1), rows)
	})
}

// The wire values the gateways emit are 0-indexed (Gas, GasAndPayload, Funds,
// FundsAndPayload) while the chain enum reserves 0 for UNSPECIFIED, so the
// mapping is shifted by one. A decoder that leaves TxType unset therefore does
// not produce "unknown", it produces GAS.
func TestBuildInboundObservation_TxTypeMapping(t *testing.T) {
	processor := NewInboundObservationEventProcessor(nil, nil, zerolog.Nop())

	for _, tc := range []struct {
		wire uint
		want uexecutortypes.TxType
	}{
		{0, uexecutortypes.TxType_GAS},
		{1, uexecutortypes.TxType_GAS_AND_PAYLOAD},
		{2, uexecutortypes.TxType_FUNDS},
		{3, uexecutortypes.TxType_FUNDS_AND_PAYLOAD},
		{4, uexecutortypes.TxType_UNSPECIFIED_TX},
		{99, uexecutortypes.TxType_UNSPECIFIED_TX},
	} {
		data, err := json.Marshal(InboundObservation{
			SourceChain: "solana:devnet",
			Sender:      "0xabc",
			Recipient:   "0xdef",
			Amount:      "5000000",
			TxType:      tc.wire,
		})
		require.NoError(t, err)

		inbound, err := processor.buildInboundObservation(&store.Event{
			EventID:   "sig:0",
			EventData: data,
		})
		require.NoError(t, err)
		assert.Equal(t, tc.want, inbound.TxType, "wire value %d", tc.wire)
	}
}

// A FUNDS transfer must never reach the keeper as GAS. The two dispatch to
// different handlers: GAS mints and autoswaps into the sender UEA, FUNDS
// deposits PRC20 to the recipient, so the same amount lands with a different
// party.
func TestBuildInboundObservation_FundsNeverBecomesGas(t *testing.T) {
	processor := NewInboundObservationEventProcessor(nil, nil, zerolog.Nop())

	data, err := json.Marshal(InboundObservation{
		SourceChain: "solana:devnet",
		Sender:      "0xabc",
		Recipient:   "0xdef",
		Amount:      "5000000",
		TxType:      2, // Funds, as the real devnet events carry
	})
	require.NoError(t, err)

	inbound, err := processor.buildInboundObservation(&store.Event{
		EventID:   "sig:0",
		EventData: data,
	})
	require.NoError(t, err)

	assert.Equal(t, uexecutortypes.TxType_FUNDS, inbound.TxType)
	assert.NotEqual(t, uexecutortypes.TxType_GAS, inbound.TxType,
		"a FUNDS transfer routed to GAS credits the sender instead of the recipient")
}

// An event whose data never made it past the decoder must be refused outright
// rather than defaulted.
func TestBuildInboundObservation_RejectsEventWithoutData(t *testing.T) {
	processor := NewInboundObservationEventProcessor(nil, nil, zerolog.Nop())

	_, err := processor.buildInboundObservation(&store.Event{EventID: "sig:0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event data is missing")
}
