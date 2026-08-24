package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// The wire values the gateways emit are 0-indexed (Gas, GasAndPayload, Funds,
// FundsAndPayload) while the chain enum reserves 0 for UNSPECIFIED, so the
// mapping is shifted by one. A decoder that leaves TxType unset therefore does
// not produce "unknown", it produces GAS.
func TestConstructInbound_TxTypeMapping(t *testing.T) {
	processor := &EventProcessor{}

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
		data, err := json.Marshal(UniversalTx{
			SourceChain: "solana:devnet",
			Sender:      "0xabc",
			Recipient:   "0xdef",
			Amount:      "5000000",
			TxType:      tc.wire,
		})
		require.NoError(t, err)

		inbound, err := processor.constructInbound(&store.Event{
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
// party. This is the end to end assertion the finding asks for.
func TestConstructInbound_FundsNeverBecomesGas(t *testing.T) {
	processor := &EventProcessor{}

	data, err := json.Marshal(UniversalTx{
		SourceChain: "solana:devnet",
		Sender:      "0xabc",
		Recipient:   "0xdef",
		Amount:      "5000000",
		TxType:      2, // Funds, as the real devnet events carry
	})
	require.NoError(t, err)

	inbound, err := processor.constructInbound(&store.Event{
		EventID:   "sig:0",
		EventData: data,
	})
	require.NoError(t, err)

	assert.Equal(t, uexecutortypes.TxType_FUNDS, inbound.TxType)
	assert.NotEqual(t, uexecutortypes.TxType_GAS, inbound.TxType,
		"a FUNDS transfer routed to GAS credits the sender instead of the recipient")
}

// An event whose data never made it past the decoder must be refused outright
// rather than defaulted. The parsers now discard such events, so this is the
// backstop if one ever reaches the store.
func TestConstructInbound_RejectsEventWithoutData(t *testing.T) {
	processor := &EventProcessor{}

	_, err := processor.constructInbound(&store.Event{EventID: "sig:0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event data is missing")
}
