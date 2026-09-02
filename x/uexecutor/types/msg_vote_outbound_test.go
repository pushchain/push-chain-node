package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// gas_fee_used is required on every vote and feeds the outbound ballot key, so a
// path that cannot read one from the chain must still report "0". The SVM revert
// event carries no gas_used at all, which is exactly that case.
func TestMsgVoteOutbound_GasFeeUsedIsRequired(t *testing.T) {
	newMsg := func(gasFeeUsed string) *types.MsgVoteOutbound {
		return &types.MsgVoteOutbound{
			Signer: "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9",
			TxId:   "0xabc",
			UtxId:  "0xdef",
			ObservedTx: &types.OutboundObservation{
				Success:     true,
				BlockHeight: 100,
				TxHash:      "0xdeadbeef",
				GasFeeUsed:  gasFeeUsed,
			},
		}
	}

	t.Run("zero is accepted", func(t *testing.T) {
		require.NoError(t, newMsg("0").ValidateBasic(),
			`a revert reports "0"; rejecting it would make the outbound unvotable`)
	})

	t.Run("empty is rejected", func(t *testing.T) {
		err := newMsg("").ValidateBasic()
		require.Error(t, err)
		require.Contains(t, err.Error(), "gas_fee_used is required")
	})

	t.Run("whitespace is rejected", func(t *testing.T) {
		require.Error(t, newMsg("   ").ValidateBasic())
	})
}
