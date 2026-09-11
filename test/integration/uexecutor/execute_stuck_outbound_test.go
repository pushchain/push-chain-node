package integrationtest

import (
	"math/big"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	chainutils "github.com/pushchain/push-chain-node/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const stuckOutboundAdmin = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"

// observedTxHash is deliberately mixed-case: the vote path lowercases an EVM tx
// hash before it hashes the observation into a ballot key, so a hatch that
// canonicalized later (or not at all) would derive a key no ballot sits under.
const observedTxHash = "0xAABBCCDDEEFF00112233445566778899AABBCCDDEEFF00112233445566778899"

// setupStuckOutbound builds a chain app carrying one PENDING outbound (created
// by a real inbound-initiated withdraw) and sets the uvalidator admin.
func setupStuckOutbound(t *testing.T) (
	chainApp *app.ChainApp,
	ctx sdk.Context,
	utxId string,
	outbound *uexecutortypes.OutboundTx,
	admin string,
) {
	t.Helper()
	chainApp, ctx, _, utxId, outbound, _ = setupOutboundVotingTest(t, 4)

	admin = stuckOutboundAdmin
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, uvalidatortypes.Params{Admin: admin}))

	require.Equal(t, uexecutortypes.Status_PENDING, outbound.OutboundStatus)
	return chainApp, ctx, utxId, outbound, admin
}

// stuckObservation is the destination-chain observation the validators voted on.
func stuckObservation(success bool, errorMsg, gasFeeUsed string) uexecutortypes.OutboundObservation {
	return uexecutortypes.OutboundObservation{
		Success:     success,
		ErrorMsg:    errorMsg,
		TxHash:      observedTxHash,
		BlockHeight: 42,
		GasFeeUsed:  gasFeeUsed,
	}
}

// outboundBallotKeyFor derives the ballot key the keeper will derive, i.e. over
// the canonicalized observation.
func outboundBallotKeyFor(t *testing.T, utxId string, outbound *uexecutortypes.OutboundTx, obs uexecutortypes.OutboundObservation) string {
	t.Helper()
	obs.TxHash = chainutils.LenientCanonicalizeTxHash(outbound.DestinationChain, obs.TxHash)
	obs.GasFeeUsed = strings.TrimSpace(obs.GasFeeUsed)
	obs.ErrorMsg = strings.TrimSpace(obs.ErrorMsg)
	key, err := uexecutortypes.GetOutboundBallotKey(utxId, outbound.Id, obs)
	require.NoError(t, err)
	return key
}

// seedOutboundBallot stores an outbound ballot under exactly that key, with a
// real eligible-voter list and per-voter vote slots — the F-2026-18147 shapes
// all turn on whether any eligible voter still holds a NOT_YET_VOTED slot.
func seedOutboundBallot(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	utxId string,
	outbound *uexecutortypes.OutboundTx,
	obs uexecutortypes.OutboundObservation,
	status uvalidatortypes.BallotStatus,
	voters []string,
	votes []uvalidatortypes.VoteResult,
	threshold int64,
) string {
	t.Helper()
	require.Len(t, votes, len(voters), "each eligible voter needs exactly one vote slot")

	ballotKey := outboundBallotKeyFor(t, utxId, outbound, obs)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballotKey, uvalidatortypes.Ballot{
		Id:                 ballotKey,
		BallotType:         uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		EligibleVoters:     voters,
		Votes:              votes,
		VotingThreshold:    threshold,
		Status:             status,
		BlockHeightCreated: 1,
		BlockHeightExpiry:  100_000_000,
	}))
	return ballotKey
}

func allVotedYes() []uvalidatortypes.VoteResult {
	return []uvalidatortypes.VoteResult{
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
		uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
	}
}

// revertRecipientOf mirrors handleFailedOutbound's choice of who gets the
// bridged tokens back.
func revertRecipientOf(outbound *uexecutortypes.OutboundTx) common.Address {
	if outbound.RevertInstructions != nil && outbound.RevertInstructions.FundRecipient != "" {
		return common.HexToAddress(outbound.RevertInstructions.FundRecipient)
	}
	return common.HexToAddress(outbound.Sender)
}

func executeStuckOutbound(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	signer, utxId, outboundId string,
	obs uexecutortypes.OutboundObservation,
) (*uexecutortypes.MsgExecuteStuckOutboundResponse, error) {
	t.Helper()
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	return ms.ExecuteStuckOutbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckOutbound{
		Signer:     signer,
		TxId:       outboundId,
		UtxId:      utxId,
		ObservedTx: &obs,
	})
}

func loadOutbound(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, utxId, outboundId string) *uexecutortypes.OutboundTx {
	t.Helper()
	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found)
	for _, ob := range utx.OutboundTx {
		if ob.Id == outboundId {
			return ob
		}
	}
	t.Fatalf("outbound %s not found in utx %s", outboundId, utxId)
	return nil
}

// TestExecuteStuckOutbound_ExpiredBallot_Success_Settles is the plain expiry
// case: quorum never formed, so the outbound sat PENDING forever even though the
// destination-chain tx landed. The hatch settles it against the observation.
func TestExecuteStuckOutbound_ExpiredBallot_Success_Settles(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	// gas_fee_used == GasFee → no excess, so nothing to refund.
	obs := stuckObservation(true, "", ob.GasFee)
	ballotKey := seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, threeVoters(), allVotedYes(), 3)

	recipient := revertRecipientOf(ob)
	before := prc20BalanceOf(t, chainApp, ctx, recipient)

	resp, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, obs)
	require.NoError(t, err, "an EXPIRED outbound ballot must be settleable")
	require.Equal(t, ob.Id, resp.OutboundId)

	settled := loadOutbound(t, chainApp, ctx, utxId, ob.Id)
	require.Equal(t, uexecutortypes.Status_OBSERVED, settled.OutboundStatus)
	require.NotNil(t, settled.ObservedTx)
	require.True(t, settled.ObservedTx.Success)
	require.Equal(t, strings.ToLower(observedTxHash), settled.ObservedTx.TxHash,
		"the stored observation must be the canonical one that was hashed into the ballot key")

	// A successful outbound mints nothing back and refunds no gas.
	require.Nil(t, settled.PcRevertExecution, "a successful settlement must not re-mint")
	require.Nil(t, settled.PcRefundExecution, "no excess gas, so no refund")
	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Cmp(before),
		"a successful settlement must not move the recipient's balance")

	// Pending index cleared, so the outbound leaves the signing queue.
	has, err := chainApp.UexecutorKeeper.PendingOutbounds.Has(ctx, ob.Id)
	require.NoError(t, err)
	require.False(t, has)

	// EXPIRED is terminal already; MarkBallotFinalized only accepts
	// PASSED/REJECTED, so the record is deliberately left alone.
	ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, ballot.Status)
}

// TestExecuteStuckOutbound_ExpiredBallot_Failure_RevertsAndRefunds pins the
// other half of the same message: the outcome follows observed_tx.success, so a
// failed observation mints the bridged tokens back and refunds the excess gas —
// no separate revert message is needed.
func TestExecuteStuckOutbound_ExpiredBallot_Failure_RevertsAndRefunds(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	// gas_fee_used (50) < GasFee (111) → excess gas must be refunded too.
	obs := stuckObservation(false, "execution reverted", "50")
	seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, threeVoters(), allVotedYes(), 3)

	recipient := revertRecipientOf(ob)
	before := prc20BalanceOf(t, chainApp, ctx, recipient)

	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, obs)
	require.NoError(t, err)

	settled := loadOutbound(t, chainApp, ctx, utxId, ob.Id)
	require.Equal(t, uexecutortypes.Status_REVERTED, settled.OutboundStatus)

	require.NotNil(t, settled.PcRevertExecution, "a failed outbound must mint the bridged funds back")
	require.Equal(t, "SUCCESS", settled.PcRevertExecution.Status)

	amount, ok := new(big.Int).SetString(ob.Amount, 10)
	require.True(t, ok)
	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Cmp(new(big.Int).Add(before, amount)),
		"the revert recipient must be credited the full outbound amount")

	require.NotNil(t, settled.PcRefundExecution, "excess gas must be refunded on failure too")
	require.NotEmpty(t, settled.PcRefundExecution.Status)
}

// TestExecuteStuckOutbound_PendingUnreachable_ThresholdMet_Settles is the
// F-2026-18147 shape on the outbound side: every eligible voter has voted YES
// and the YES count already clears the stored threshold, but the ballot was
// returned PENDING and AddVote rejects repeat votes, so nothing can move it.
func TestExecuteStuckOutbound_PendingUnreachable_ThresholdMet_Settles(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	obs := stuckObservation(true, "", ob.GasFee)
	ballotKey := seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, threeVoters(), allVotedYes(),
		2) // YES (3) already clears the recomputed threshold

	resp, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, obs)
	require.NoError(t, err, "an unreachable PENDING ballot at threshold must be settleable")
	require.Equal(t, ob.Id, resp.OutboundId)

	settled := loadOutbound(t, chainApp, ctx, utxId, ob.Id)
	require.Equal(t, uexecutortypes.Status_OBSERVED, settled.OutboundStatus)
	require.NotNil(t, settled.ObservedTx)

	// Unlike EXPIRED, this ballot was not terminal, so the hatch drives it there.
	ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED, ballot.Status)
}

// TestExecuteStuckOutbound_PendingUnreachable_BelowThreshold_Refused covers the
// other stuck shape: nothing can move the ballot, but the validators never
// carried it. Settling would act on an observation the set did not attest.
func TestExecuteStuckOutbound_PendingUnreachable_BelowThreshold_Refused(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	obs := stuckObservation(true, "", ob.GasFee)
	seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
		},
		3) // YES (2) < 3 → unreachable, but never carried

	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, obs)
	require.Error(t, err, "a ballot that never met its threshold must not be settled")
	require.Contains(t, err.Error(), "against a voting threshold of 3")

	assertOutboundUntouched(t, chainApp, ctx, utxId, ob.Id)
}

// TestExecuteStuckOutbound_PendingWithUnvotedVoter_Refused is the guard against
// widening the hatch: the YES votes already clear the threshold, but one
// eligible voter still holds a NOT_YET_VOTED slot, so a single normal vote
// finalizes it through the real pipeline. Admin settle must not race that.
func TestExecuteStuckOutbound_PendingWithUnvotedVoter_Refused(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	obs := stuckObservation(true, "", ob.GasFee)
	ballotKey := seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
		},
		2)

	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, obs)
	require.Error(t, err, "a PENDING ballot with an unvoted eligible voter can still finalize normally")
	require.Contains(t, err.Error(), "admin execute requires PENDING")

	// The ballot must be left votable so the remaining voter can finalize it.
	ballot, err := chainApp.UvalidatorKeeper.Ballots.Get(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, ballot.Status)

	assertOutboundUntouched(t, chainApp, ctx, utxId, ob.Id)
}

// TestExecuteStuckOutbound_AlreadySettled_Refused is the idempotency barrier. An
// EXPIRED ballot is left untouched by design, so the ballot gate lets a second
// call through and only the outbound's own status stops it — without which the
// admin could re-mint the same funds repeatedly.
func TestExecuteStuckOutbound_AlreadySettled_Refused(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	obs := stuckObservation(false, "execution reverted", ob.GasFee)
	seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, threeVoters(), allVotedYes(), 3)

	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, obs)
	require.NoError(t, err)

	recipient := revertRecipientOf(ob)
	afterFirst := prc20BalanceOf(t, chainApp, ctx, recipient)

	_, err = executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, obs)
	require.Error(t, err, "a second settle must be refused")
	require.Contains(t, err.Error(), "already finalized")

	require.Equal(t, 0, prc20BalanceOf(t, chainApp, ctx, recipient).Cmp(afterFirst),
		"the refused second settle must not have minted again")
}

// TestExecuteStuckOutbound_VotedOutbound_Refused covers the normal-flow overlap:
// once the validators settled the outbound themselves, its ballot is PASSED and
// the hatch has nothing to do.
func TestExecuteStuckOutbound_VotedOutbound_Refused(t *testing.T) {
	chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, uvalidatortypes.Params{Admin: stuckOutboundAdmin}))

	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteOutbound(
			t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), utxId, ob, true, "", ob.GasFee))
	}
	require.Equal(t, uexecutortypes.Status_OBSERVED, loadOutbound(t, chainApp, ctx, utxId, ob.Id).OutboundStatus)

	// The vote path's own observation, so the ballot key resolves to the PASSED ballot.
	obs := uexecutortypes.OutboundObservation{
		Success:     true,
		TxHash:      "0xobserved-" + ob.Id,
		BlockHeight: 1,
		GasFeeUsed:  ob.GasFee,
	}
	_, err := executeStuckOutbound(t, chainApp, ctx, stuckOutboundAdmin, utxId, ob.Id, obs)
	require.Error(t, err, "a ballot the validators finalized is not stuck")
	require.Contains(t, err.Error(), "admin execute requires PENDING")
}

// TestExecuteStuckOutbound_TamperedObservation_Refused pins the security
// property the ballot-key derivation buys: the admin can only settle against the
// exact observation the validators voted on. One changed field derives a
// different key, which has no ballot at all.
func TestExecuteStuckOutbound_TamperedObservation_Refused(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	obs := stuckObservation(true, "", ob.GasFee)
	seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, threeVoters(), allVotedYes(), 3)

	tampered := obs
	tampered.BlockHeight = obs.BlockHeight + 1

	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, tampered)
	require.Error(t, err, "an observation the validators never voted on has no ballot")
	require.Contains(t, err.Error(), "ballot for outbound not found")

	assertOutboundUntouched(t, chainApp, ctx, utxId, ob.Id)
}

func TestExecuteStuckOutbound_BallotNotFound(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)
	// no ballot seeded
	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, stuckObservation(true, "", ob.GasFee))
	require.Error(t, err)
	require.Contains(t, err.Error(), "ballot for outbound not found")
}

func TestExecuteStuckOutbound_AdminAuth_RejectsNonAdmin(t *testing.T) {
	chainApp, ctx, utxId, ob, _ := setupStuckOutbound(t)

	obs := stuckObservation(true, "", ob.GasFee)
	seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, threeVoters(), allVotedYes(), 3)

	const notAdmin = "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"
	_, err := executeStuckOutbound(t, chainApp, ctx, notAdmin, utxId, ob.Id, obs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid admin")

	assertOutboundUntouched(t, chainApp, ctx, utxId, ob.Id)
}

func TestExecuteStuckOutbound_NilObservedTx_Rejected(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.ExecuteStuckOutbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgExecuteStuckOutbound{
		Signer:     admin,
		TxId:       ob.Id,
		UtxId:      utxId,
		ObservedTx: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "observed_tx is required")
}

func TestExecuteStuckOutbound_UnknownOutbound_Rejected(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	const unknown = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, unknown, stuckObservation(true, "", ob.GasFee))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")

	_, err = executeStuckOutbound(t, chainApp, ctx, admin, unknown, ob.Id, stuckObservation(true, "", ob.GasFee))
	require.Error(t, err)
	require.Contains(t, err.Error(), "UniversalTx not found")
}

// TestExecuteStuckOutbound_PrefixedIds_Settles mirrors the vote path: UVs (and
// the operators reading their logs) carry 0x-prefixed IDs, and the handler has
// to strip them exactly once before the keeper lookup.
func TestExecuteStuckOutbound_PrefixedIds_Settles(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	obs := stuckObservation(true, "", ob.GasFee)
	seedOutboundBallot(t, chainApp, ctx, utxId, ob, obs,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, threeVoters(), allVotedYes(), 3)

	resp, err := executeStuckOutbound(t, chainApp, ctx, admin, "0x"+utxId, "0x"+ob.Id, obs)
	require.NoError(t, err)
	require.Equal(t, ob.Id, resp.OutboundId)

	require.Equal(t, uexecutortypes.Status_OBSERVED, loadOutbound(t, chainApp, ctx, utxId, ob.Id).OutboundStatus)
}

// The keeper re-runs that validation, so the malformed value is refused even on
// a direct keeper call that never passed through ValidateBasic.
func TestExecuteStuckOutbound_MalformedGasFeeUsed_RefusedByKeeper(t *testing.T) {
	chainApp, ctx, utxId, ob, admin := setupStuckOutbound(t)

	_, err := executeStuckOutbound(t, chainApp, ctx, admin, utxId, ob.Id, stuckObservation(true, "", "not-a-number"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "observed_tx.gas_fee_used must be a valid uint256")

	assertOutboundUntouched(t, chainApp, ctx, utxId, ob.Id)
}

// assertOutboundUntouched checks a refusal was total: the outbound is still
// PENDING and still queued for signing.
func assertOutboundUntouched(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, utxId, outboundId string) {
	t.Helper()
	ob := loadOutbound(t, chainApp, ctx, utxId, outboundId)
	require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus, "a refused settle must not move the outbound")
	require.Nil(t, ob.ObservedTx)

	has, err := chainApp.UexecutorKeeper.PendingOutbounds.Has(ctx, outboundId)
	require.NoError(t, err)
	require.True(t, has, "a refused settle must leave the outbound queued")
}
