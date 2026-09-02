package integrationtest

import (
	"fmt"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatorkeeper "github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// TestExecuteStuckInbound_PendingUnreachable_ThresholdMet_Executes is the
// headline F-2026-18147 case for the execute hatch.
//
// RecomputeBallotQuorum preserves the votes of still-eligible voters and lowers
// the threshold, but returns PENDING without tallying what it just rebuilt. The
// shape reproduced here is the result: every remaining eligible voter has voted
// YES and the YES count already clears the recomputed threshold, so the ballot
// should have passed but AddVote rejects repeat votes and nothing can move it.
//
// The whole live validator set attested this deposit, so the correct resolution
// is to deliver the funds on Push - not to refund on the source chain, which is
// all RevertStuckInbound could do.
func TestExecuteStuckInbound_PendingUnreachable_ThresholdMet_Executes(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		},
		2, // YES (3) already clears the recomputed threshold
	)

	recipient := common.HexToAddress(inbound.Recipient)
	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Sign(),
		"recipient must start with no bridged balance")

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err, "an unreachable PENDING ballot at threshold must be executable")
	require.Equal(t, uexecutortypes.GetInboundUniversalTxKey(*inbound), resp.UtxId)

	// --- UTX assertions ---
	utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.NoError(t, err)
	require.NotNil(t, utx.InboundTx)
	require.Equal(t, inbound.TxHash, utx.InboundTx.TxHash)

	require.Len(t, utx.PcTx, 1)
	require.Equal(t, "SUCCESS", utx.PcTx[0].Status,
		"the execute hatch must run the deposit, not record a failure")

	// The point of this hatch: execution, not refund.
	require.Empty(t, utx.OutboundTx, "an executed inbound must not create a revert outbound")

	// --- The user actually got the funds ---
	amount, ok := new(big.Int).SetString(inbound.Amount, 10)
	require.True(t, ok)
	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Cmp(amount),
		"recipient balance must equal the inbound amount")

	// --- Ballot is terminal, so the hatch cannot be re-entered ---
	ballotKey, err := uexecutortypes.GetInboundBallotKey(*inbound)
	require.NoError(t, err)
	ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, ballot.Status)

	// The pending audit-trail entry must be gone: the terminal hook clears it and
	// the pipeline's RemovePendingInbound is a no-op on the absent key.
	isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
	require.NoError(t, err)
	require.False(t, isPending)
}

// TestExecuteStuckInbound_DuplicateExecute_Rejected verifies a second call
// cannot mint twice.
//
// Two independent barriers stand in the way and the outer one wins here: the
// first call drove the ballot to PASSED, so IsUnreachablePending is already
// false. The UTX barrier underneath it is exercised by
// TestExecuteStuckInbound_AlreadyRevertedInbound_Refused, where the ballot stays
// PENDING-unreachable and only the UTX blocks the second call.
func TestExecuteStuckInbound_DuplicateExecute_Rejected(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		},
		2,
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)

	amount, ok := new(big.Int).SetString(inbound.Amount, 10)
	require.True(t, ok)
	recipient := common.HexToAddress(inbound.Recipient)

	_, err = ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "a second execute must be refused")
	require.Contains(t, err.Error(), "admin execute requires PENDING",
		"the ballot is already PASSED, so the status gate refuses before the UTX gate is reached")

	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Cmp(amount),
		"the refused second execute must not have minted again")
}

// TestExecuteStuckInbound_AlreadyRevertedInbound_Refused pins the same barrier
// against the sibling hatch: once RevertStuckInbound has created its UTX, the
// inbound cannot also be executed. Otherwise an operator could refund the user
// on the source chain and then mint them the same funds on Push.
func TestExecuteStuckInbound_AlreadyRevertedInbound_Refused(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		},
		2,
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)

	_, err = ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "an inbound already reverted must not also be executed")
	require.Contains(t, err.Error(), "already exists")

	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, common.HexToAddress(inbound.Recipient)).Sign(),
		"a refused execute must not mint")
}

// TestExecuteStuckInbound_ExpiredBallot_Refused keeps the two hatches apart. An
// EXPIRED ballot never reached quorum, so there is no attestation to act on and
// the refund is the honest resolution — that is RevertStuckInbound's job.
func TestExecuteStuckInbound_ExpiredBallot_Refused(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		},
		2,
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "EXPIRED belongs to the revert hatch, not the execute hatch")
	require.Contains(t, err.Error(), "admin execute requires PENDING")
	require.Contains(t, err.Error(), "MsgRevertStuckInbound",
		"the refusal must point the operator at the hatch that does apply")

	assertNoUtxOrMint(t, chainApp, ctx, inbound)
}

// TestExecuteStuckInbound_PassedBallot_Refused guards the case where the ballot
// already finalized normally: IsUnreachablePending is false, so re-running the
// pipeline is refused even before the UTX check.
func TestExecuteStuckInbound_PassedBallot_Refused(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		},
		2,
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "admin execute requires PENDING")

	assertNoUtxOrMint(t, chainApp, ctx, inbound)
}

// TestExecuteStuckInbound_PendingWithUnvotedVoter_Refused is the guard against
// widening the hatch too far.
//
// The YES votes already clear the threshold, so this looks exactly like the
// headline case. It is not: one eligible voter still holds a NOT_YET_VOTED slot,
// so a single normal vote finalizes it through the real pipeline. Admin execute
// must not race that — the vote flow decides, not the admin.
func TestExecuteStuckInbound_PendingWithUnvotedVoter_Refused(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
		},
		2, // YES (2) already meets threshold — still refused, it can finalize normally
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "a PENDING ballot with an unvoted eligible voter can still finalize normally")
	require.Contains(t, err.Error(), "admin execute requires PENDING")

	// The ballot must be left untouched so the remaining voter can still finalize it.
	ballotKey, err := uexecutortypes.GetInboundBallotKey(*inbound)
	require.NoError(t, err)
	ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, ballot.Status)

	assertNoUtxOrMint(t, chainApp, ctx, inbound)
}

// TestExecuteStuckInbound_BelowThreshold_Refused covers the second stuck shape:
// every eligible voter has voted, so the ballot is unreachable, but the YES count
// never reached the threshold. There is no supermajority attestation to act on,
// so executing would deliver funds the validator set did not carry. That case
// belongs to the revert hatch, which accepts it.
//
// Unreachable today implies yes == len(EligibleVoters) because both inbound vote
// sites hardcode VOTE_RESULT_SUCCESS. This test seeds the FAILURE vote directly
// so the threshold check is pinned independently of that invariant.
func TestExecuteStuckInbound_BelowThreshold_Refused(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
		},
		3, // YES (2) < 3 → unreachable, but never carried
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "a ballot that never met its threshold must not be executed")
	require.Contains(t, err.Error(), "against a voting threshold of 3")
	require.Contains(t, err.Error(), "MsgRevertStuckInbound")

	assertNoUtxOrMint(t, chainApp, ctx, inbound)
}

func TestExecuteStuckInbound_AdminAuth_RejectsNonAdmin(t *testing.T) {
	chainApp, ctx, inbound, _ := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		},
		2,
	)

	const notAdmin = "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  notAdmin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid admin")

	assertNoUtxOrMint(t, chainApp, ctx, inbound)
}

func TestExecuteStuckInbound_BallotNotFound(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	// no ballot seeded
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ballot for inbound not found")
}

func TestExecuteStuckInbound_NilInbound_Rejected(t *testing.T) {
	chainApp, ctx, _, admin := setupRevertStuckInbound(t)
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbound is required")
}

// TestExecuteStuckInbound_TamperedInbound_Refused pins the security property the
// ballot-key derivation buys: the admin can only execute the exact payload the
// validators voted on. A single changed field derives a different ballot key,
// which has no ballot at all.
func TestExecuteStuckInbound_TamperedInbound_Refused(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		},
		2,
	)

	tampered := *inbound
	tampered.Amount = "999999999"

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: &tampered,
	})
	require.Error(t, err, "a payload the validators never voted on has no ballot")
	require.Contains(t, err.Error(), "ballot for inbound not found")
}

// TestExecuteStuckInbound_RecomputeThenExecute_E2E walks the whole F-2026-18147
// story with real universal validators and real votes:
//
//	4 UVs, threshold 3 → 2 vote YES, ballot stays PENDING → the other 2 leave the
//	set → MsgRecomputeBallotQuorum rebuilds it to the 2 remaining voters with
//	threshold 2 and preserves their YES votes, but returns PENDING without
//	tallying → nothing can ever move the ballot → MsgExecuteStuckInbound
//	finalizes it PASSED and delivers the funds.
func TestExecuteStuckInbound_RecomputeThenExecute_E2E(t *testing.T) {
	chainApp, ctx, universalVals, inbound, coreVals := setupInboundBridgeTest(t, 4)

	const admin = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, uvalidatortypes.Params{Admin: admin}))

	// Two of four vote YES. Threshold is (2*4)/3+1 = 3, so the ballot stays PENDING.
	for i := 0; i < 2; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteInbound(t, ctx, chainApp, universalVals[i],
			sdk.AccAddress(valAddr).String(), inbound))
	}

	// Derive the ballot key the way the keeper does — off the canonical payload.
	canonical := *inbound
	canonical.Canonicalize()
	ballotKey, err := uexecutortypes.GetInboundBallotKey(canonical)
	require.NoError(t, err)

	ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, ballot.Status)
	require.Equal(t, int64(3), ballot.VotingThreshold)

	// The two silent validators leave the universal-validator set.
	for i := 2; i < 4; i++ {
		v := coreVals[i]
		v.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, v))
	}

	// Recompute: 2 eligible, threshold 2, both preserved votes YES. This is the
	// bug — the recomputed ballot already satisfies its own threshold, yet it is
	// returned PENDING and no vote is left to cast.
	uvMs := uvalidatorkeeper.NewMsgServerImpl(chainApp.UvalidatorKeeper)
	recomputeResp, err := uvMs.RecomputeBallotQuorum(sdk.WrapSDKContext(ctx), &uvalidatortypes.MsgRecomputeBallotQuorum{
		Signer:   admin,
		BallotId: ballotKey,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), recomputeResp.NewEligibleCount)
	require.Equal(t, int64(2), recomputeResp.NewVotingThreshold)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, recomputeResp.NewStatus)

	stuck, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.True(t, stuck.IsUnreachablePending(), "no eligible voter is left to cast a vote")
	yes, _ := stuck.CountVotes()
	require.Equal(t, 2, yes, "recompute preserved both YES votes")

	// A third vote cannot rescue it: the remaining voters have already voted, and
	// the departed ones are no longer eligible.
	valAddr, err := sdk.ValAddressFromBech32(coreVals[2].OperatorAddress)
	require.NoError(t, err)
	require.Error(t,
		utils.ExecVoteInbound(t, ctx, chainApp, universalVals[2], sdk.AccAddress(valAddr).String(), inbound),
		"the ballot is genuinely stuck, not merely waiting")

	recipient := common.HexToAddress(inbound.Recipient)
	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Sign())

	// The escape hatch.
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.ExecuteStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)
	require.Equal(t, uexecutortypes.GetInboundUniversalTxKey(canonical), resp.UtxId)

	utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.NoError(t, err)
	require.Len(t, utx.PcTx, 1)
	require.Equal(t, "SUCCESS", utx.PcTx[0].Status)
	require.Empty(t, utx.OutboundTx, "the user is paid on Push, not refunded on the source chain")

	amount, ok := new(big.Int).SetString(inbound.Amount, 10)
	require.True(t, ok)
	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Cmp(amount))

	final, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, final.Status)

	isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, canonical)
	require.NoError(t, err)
	require.False(t, isPending, "the pending audit-trail entry must be cleared")
}

// assertNoUtxOrMint checks a refusal was total: no UniversalTx was written and
// nothing was minted to the recipient.
func assertNoUtxOrMint(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, inbound *uexecutortypes.Inbound) {
	t.Helper()
	utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
	has, err := chainApp.UexecutorKeeper.HasUniversalTx(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, has, fmt.Sprintf("a refused execute must not leave a UniversalTx behind (%s)", utxKey))

	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, common.HexToAddress(inbound.Recipient)).Sign(),
		"a refused execute must not mint")
}
