package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

const stuckOutboundSigner = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"

func newMsgExecuteStuckOutbound(obs *types.OutboundObservation) *types.MsgExecuteStuckOutbound {
	return &types.MsgExecuteStuckOutbound{
		Signer:     stuckOutboundSigner,
		TxId:       "outbound-1",
		UtxId:      "utx-1",
		ObservedTx: obs,
	}
}

func successObservation(gasFeeUsed string) *types.OutboundObservation {
	return &types.OutboundObservation{
		Success:     true,
		TxHash:      "0x" + strings.Repeat("ab", 32),
		BlockHeight: 42,
		GasFeeUsed:  gasFeeUsed,
	}
}

// gas_fee_used feeds both the outbound ballot key and the refund arithmetic, so
// MsgExecuteStuckOutbound has to admit exactly what MsgVoteOutbound admits —
// including the length cap and uint256 range check from F-2026-18798.
func TestMsgExecuteStuckOutbound_ValidateBasic_GasFeeUsed(t *testing.T) {
	cases := []struct {
		name       string
		gasFeeUsed string
		expectErr  string
	}{
		{name: "valid", gasFeeUsed: "1000"},
		{name: "zero is valid", gasFeeUsed: "0"},
		{name: "empty", gasFeeUsed: "", expectErr: "observed_tx.gas_fee_used is required"},
		{name: "non-numeric", gasFeeUsed: "not-a-number", expectErr: "observed_tx.gas_fee_used must be a valid uint256"},
		{name: "negative", gasFeeUsed: "-1", expectErr: "observed_tx.gas_fee_used must be a valid uint256"},
		{name: "over uint256 range", gasFeeUsed: strings.Repeat("9", 78), expectErr: "value exceeds the uint256 range"},
		{name: "over length cap", gasFeeUsed: strings.Repeat("1", 81), expectErr: "exceeds the maximum of 80 characters"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newMsgExecuteStuckOutbound(successObservation(tc.gasFeeUsed)).ValidateBasic()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.expectErr)
		})
	}
}

func TestMsgExecuteStuckOutbound_ValidateBasic_RequiredFields(t *testing.T) {
	require.NoError(t, newMsgExecuteStuckOutbound(successObservation("100")).ValidateBasic())

	bad := newMsgExecuteStuckOutbound(successObservation("100"))
	bad.Signer = "not-bech32"
	require.ErrorContains(t, bad.ValidateBasic(), "invalid signer address")

	bad = newMsgExecuteStuckOutbound(successObservation("100"))
	bad.TxId = "  "
	require.ErrorContains(t, bad.ValidateBasic(), "tx_id cannot be empty")

	bad = newMsgExecuteStuckOutbound(successObservation("100"))
	bad.UtxId = ""
	require.ErrorContains(t, bad.ValidateBasic(), "utx_id cannot be empty")

	require.ErrorContains(t, newMsgExecuteStuckOutbound(nil).ValidateBasic(), "observed_tx cannot be nil")

	// Success requires a tx hash and a block height.
	bad = newMsgExecuteStuckOutbound(successObservation("100"))
	bad.ObservedTx.TxHash = ""
	require.ErrorContains(t, bad.ValidateBasic(), "observed_tx.tx_hash required when success=true")

	bad = newMsgExecuteStuckOutbound(successObservation("100"))
	bad.ObservedTx.BlockHeight = 0
	require.ErrorContains(t, bad.ValidateBasic(), "observed_tx.block_height must be > 0 when success=true")

	// A failed observation may carry no tx hash — but if it does, it needs a height.
	require.NoError(t, newMsgExecuteStuckOutbound(&types.OutboundObservation{
		Success: false, ErrorMsg: "reverted", GasFeeUsed: "100",
	}).ValidateBasic())

	require.ErrorContains(t, newMsgExecuteStuckOutbound(&types.OutboundObservation{
		Success: false, ErrorMsg: "reverted", GasFeeUsed: "100", TxHash: "0xdead",
	}).ValidateBasic(), "observed_tx.block_height must be > 0 when tx_hash is provided")
}

// The admin hatch and the validator vote path must admit the same observations:
// anything the vote path refuses can never have produced a ballot for the hatch
// to settle against.
func TestMsgExecuteStuckOutbound_MatchesVoteOutboundAdmission(t *testing.T) {
	observations := []*types.OutboundObservation{
		successObservation("100"),
		successObservation(""),
		successObservation("not-a-number"),
		successObservation(strings.Repeat("9", 78)),
		{Success: false, ErrorMsg: "reverted", GasFeeUsed: "0"},
		{Success: false, GasFeeUsed: "1", TxHash: "0xdead"},
		{Success: true, GasFeeUsed: "1", BlockHeight: 0, TxHash: "0xdead"},
	}

	for i, obs := range observations {
		voteMsg := &types.MsgVoteOutbound{
			Signer:     stuckOutboundSigner,
			TxId:       "outbound-1",
			UtxId:      "utx-1",
			ObservedTx: obs,
		}
		voteErr := voteMsg.ValidateBasic()
		hatchErr := newMsgExecuteStuckOutbound(obs).ValidateBasic()

		if voteErr == nil {
			require.NoError(t, hatchErr, "observation %d accepted by the vote path must be accepted by the hatch", i)
			continue
		}
		require.Error(t, hatchErr, "observation %d refused by the vote path must be refused by the hatch", i)
		require.Equal(t, voteErr.Error(), hatchErr.Error(), "observation %d must be refused for the same reason", i)
	}
}
