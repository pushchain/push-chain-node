package integrationtest

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	ucallbackkeeper "github.com/pushchain/push-chain-node/x/ucallback/keeper"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// A vote from a bonded universal validator must reach the ballot and, at quorum,
// drive the read to a terminal state through the real contract. With one validator
// the first vote is already quorum, so this covers vote -> ballot -> hook -> settle
// in a single call — the wiring the unit tests stub out at every seam.
func TestVoteReadResult_SingleValidatorReachesQuorumAndSettles(t *testing.T) {
	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 1)
	k := chainApp.UcallbackKeeper
	require.Len(t, validators, 1)
	valOperator := validators[0].OperatorAddress

	require.NoError(t, chainApp.UvalidatorKeeper.AddUniversalValidator(ctx, valOperator,
		uvalidatortypes.NetworkInfo{}))

	f := newReadFixture(t, chainApp, ctx, 0x31, big.NewInt(3_000_000_000_000_000), 200_000,
		uint64(ctx.BlockHeight())+1000)

	valAddr, err := sdk.ValAddressFromBech32(valOperator)
	require.NoError(t, err)
	signer := sdk.AccAddress(valAddr).String()

	supplyBefore := chainApp.BankKeeper.GetSupply(ctx, pchaintypes.BaseDenom).Amount

	_, err = ucallbackkeeper.NewMsgServerImpl(k).VoteReadResult(ctx, &ucallbacktypes.MsgVoteReadResult{
		Signer:    signer,
		RequestId: f.idHex,
		Result:    okResult(),
	})
	require.NoError(t, err, "a bonded universal validator must be allowed to vote")

	got, ok := k.GetUniversalRead(ctx, f.idHex)
	require.True(t, ok)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, got.Status,
		"quorum must carry the read all the way to FULFILLED; err=%q", got.ErrorMsg)

	// the ballot hook must have reached the real contract, not just our own state
	require.Equal(t, uint8(3), // SETTLED
		staticCall(t, chainApp, ctx, loadViewABI(t), f.contract, "statusOf", f.id)[0].(uint8),
		"AfterBallotTerminal must drive the contract to SETTLED")

	require.True(t,
		chainApp.BankKeeper.GetSupply(ctx, pchaintypes.BaseDenom).Amount.LT(supplyBefore),
		"settling through a vote must burn the consumed budget")
}

// A vote from an address that is not a universal validator must be refused.
func TestVoteReadResult_RejectsNonValidator(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	f := newReadFixture(t, chainApp, ctx, 0x32, big.NewInt(3_000_000_000_000_000), 200_000,
		uint64(ctx.BlockHeight())+1000)

	_, err := ucallbackkeeper.NewMsgServerImpl(k).VoteReadResult(ctx, &ucallbacktypes.MsgVoteReadResult{
		Signer:    sdk.AccAddress([]byte("not-a-validator-addr")).String(),
		RequestId: f.idHex,
		Result:    okResult(),
	})
	require.Error(t, err, "an unbonded address must not be able to vote on a read")
}

// UpdateParams must be gated on the governance authority.
func TestUpdateParams_AuthorityGated(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	ms := ucallbackkeeper.NewMsgServerImpl(chainApp.UcallbackKeeper)

	_, err := ms.UpdateParams(ctx, &ucallbacktypes.MsgUpdateParams{
		Authority: sdk.AccAddress([]byte("definitely-not-gov")).String(),
		Params:    ucallbacktypes.DefaultParams(),
	})
	require.Error(t, err, "only the gov authority may update params")
}

// The deciding vote must settle the read even when the immediately preceding vote
// landed on a DIFFERENT observation.
//
// This is the case that made the ordering bug more than a single-validator quirk:
// the record tracks whichever ballot the last vote touched, so if that was a losing
// observation, the terminal hook used to look up the winning ballot and find
// nothing — quorum reached, read abandoned to the sweeper.
func TestVoteReadResult_DecidingVoteAfterALosingObservation(t *testing.T) {
	const numVals = 4 // votesNeeded = (2*4)/3 + 1 = 3
	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)
	k := chainApp.UcallbackKeeper

	signers := make([]string, numVals)
	for i, v := range validators {
		require.NoError(t, chainApp.UvalidatorKeeper.AddUniversalValidator(ctx,
			v.OperatorAddress, uvalidatortypes.NetworkInfo{}))
		valAddr, err := sdk.ValAddressFromBech32(v.OperatorAddress)
		require.NoError(t, err)
		signers[i] = sdk.AccAddress(valAddr).String()
	}

	f := newReadFixture(t, chainApp, ctx, 0x33, big.NewInt(3_000_000_000_000_000), 200_000,
		uint64(ctx.BlockHeight())+1000)

	ms := ucallbackkeeper.NewMsgServerImpl(k)
	winning := okResult()
	losing := okResult()
	losing.ResultData = []byte{0xff, 0xee} // a different observation => different ballot

	vote := func(signer string, r *ucallbacktypes.ReadResult) {
		t.Helper()
		_, err := ms.VoteReadResult(ctx, &ucallbacktypes.MsgVoteReadResult{
			Signer: signer, RequestId: f.idHex, Result: r,
		})
		require.NoError(t, err)
	}

	vote(signers[0], winning) // 1 of 3 on the winner
	vote(signers[1], winning) // 2 of 3
	vote(signers[2], losing)  // record now points at the LOSING ballot
	vote(signers[3], winning) // 3 of 3 -- this must still settle

	got, ok := k.GetUniversalRead(ctx, f.idHex)
	require.True(t, ok)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, got.Status,
		"the deciding vote must settle the read regardless of what was voted before it; err=%q",
		got.ErrorMsg)
	require.Equal(t, winning.ResultData, got.Result.ResultData,
		"the quorum observation must be the one recorded, not the last one voted")

	require.Equal(t, uint8(3), // SETTLED
		staticCall(t, chainApp, ctx, loadViewABI(t), f.contract, "statusOf", f.id)[0].(uint8))
}
