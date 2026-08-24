package svm

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

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
