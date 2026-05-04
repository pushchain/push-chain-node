package integrationtest

// Integration tests for the variant-aware PendingOutbounds audit trail
// introduced for F-2026-16642 (outbound side).
//
// These tests exercise:
//   - RecordOutboundVote idempotency and per-variant tracking
//   - Multi-variant accumulation (different validators voting different
//     OutboundObservations for the same outbound_id)
//   - The crucial design property: ballot expiry does NOT remove
//     PendingOutbounds entries (operator-investigation-only design)
//
// See plan-pending-outbound-cleanup.md for the design doc.

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const (
	outboundAuditVoter1 = "cosmosvaloper1outboundaudit10000000000000000000000"
	outboundAuditVoter2 = "cosmosvaloper1outboundaudit20000000000000000000000"
)

// seedPendingOutbound writes a PendingOutboundEntry chain-side (mimicking
// what create_outbound.go does at outbound creation) so the test can then
// drive RecordOutboundVote and BallotHook scenarios against it.
func seedPendingOutbound(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, utxId, outboundId string) {
	t.Helper()
	require.NoError(t, chainApp.UexecutorKeeper.PendingOutbounds.Set(ctx, outboundId, uexecutortypes.PendingOutboundEntry{
		OutboundId:    outboundId,
		UniversalTxId: utxId,
		CreatedAt:     ctx.BlockHeight(),
	}))
}

func makeObservation(success bool, txHash, errorMsg string) uexecutortypes.OutboundObservation {
	return uexecutortypes.OutboundObservation{
		Success:     success,
		BlockHeight: 100,
		TxHash:      txHash,
		ErrorMsg:    errorMsg,
		GasFeeUsed:  "21000",
	}
}

// -------------------------------------------------------------------------
// RecordOutboundVote — variant accumulation, idempotency
// -------------------------------------------------------------------------

func TestRecordOutboundVote_FirstVoteAppendsVariant(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	utxId := "utx-1"
	outboundId := "outbound-1"
	seedPendingOutbound(t, chainApp, ctx, utxId, outboundId)

	obs := makeObservation(true, "0xdesttx1", "")
	ballotKey, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obs)
	require.NoError(t, err)

	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obs, outboundAuditVoter1, ballotKey))

	entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, outboundId)
	require.NoError(t, err)
	require.Equal(t, outboundId, entry.OutboundId)
	require.Equal(t, utxId, entry.UniversalTxId)
	require.Len(t, entry.Variants, 1)
	require.Equal(t, ballotKey, entry.Variants[0].BallotId)
	require.Equal(t, []string{outboundAuditVoter1}, entry.Variants[0].Voters)
	require.Equal(t, "0xdesttx1", entry.Variants[0].ObservedTx.TxHash)
}

func TestRecordOutboundVote_SameVoterTwiceIsIdempotent(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	utxId := "utx-dup"
	outboundId := "outbound-dup"
	seedPendingOutbound(t, chainApp, ctx, utxId, outboundId)

	obs := makeObservation(true, "0xdesttx", "")
	ballotKey, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obs)
	require.NoError(t, err)

	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obs, outboundAuditVoter1, ballotKey))
	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obs, outboundAuditVoter1, ballotKey))

	entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, outboundId)
	require.NoError(t, err)
	require.Len(t, entry.Variants, 1)
	require.Len(t, entry.Variants[0].Voters, 1, "duplicate voter must not be re-added")
}

func TestRecordOutboundVote_DifferentVotersSameVariant(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	utxId := "utx-shared"
	outboundId := "outbound-shared"
	seedPendingOutbound(t, chainApp, ctx, utxId, outboundId)

	obs := makeObservation(true, "0xdesttx", "")
	ballotKey, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obs)
	require.NoError(t, err)

	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obs, outboundAuditVoter1, ballotKey))
	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obs, outboundAuditVoter2, ballotKey))

	entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, outboundId)
	require.NoError(t, err)
	require.Len(t, entry.Variants, 1, "same observation bytes → single variant")
	require.ElementsMatch(t, []string{outboundAuditVoter1, outboundAuditVoter2}, entry.Variants[0].Voters)
}

func TestRecordOutboundVote_DifferentObservationsCreateDistinctVariants(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	utxId := "utx-multi"
	outboundId := "outbound-multi"
	seedPendingOutbound(t, chainApp, ctx, utxId, outboundId)

	// Same outbound, different destination-chain observations → different ballots.
	obsA := makeObservation(true, "0xdesttxA", "")
	obsB := makeObservation(false, "0xdesttxB", "reverted")

	ballotA, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obsA)
	require.NoError(t, err)
	ballotB, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obsB)
	require.NoError(t, err)
	require.NotEqual(t, ballotA, ballotB, "different observations must produce different ballot keys")

	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obsA, outboundAuditVoter1, ballotA))
	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obsB, outboundAuditVoter2, ballotB))

	entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, outboundId)
	require.NoError(t, err)
	require.Len(t, entry.Variants, 2)

	byBallot := make(map[string]uexecutortypes.OutboundObservationVariant, 2)
	for _, v := range entry.Variants {
		byBallot[v.BallotId] = v
	}
	require.Equal(t, []string{outboundAuditVoter1}, byBallot[ballotA].Voters)
	require.True(t, byBallot[ballotA].ObservedTx.Success)
	require.Equal(t, []string{outboundAuditVoter2}, byBallot[ballotB].Voters)
	require.False(t, byBallot[ballotB].ObservedTx.Success)
}

func TestRecordOutboundVote_MissingPendingEntryReturnsError(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	// No seedPendingOutbound call — entry intentionally missing.
	obs := makeObservation(true, "0xdesttx", "")
	ballotKey, err := uexecutortypes.GetOutboundBallotKey("utx-x", "outbound-missing", obs)
	require.NoError(t, err)

	err = chainApp.UexecutorKeeper.RecordOutboundVote(ctx, "outbound-missing", obs, outboundAuditVoter1, ballotKey)
	require.Error(t, err, "PendingOutbounds entry must exist before RecordOutboundVote is called")
}

// -------------------------------------------------------------------------
// CRITICAL: ballot expiry does NOT remove PendingOutbounds entries.
// This is the documented design — operators investigate stuck outbounds
// manually because the destination-chain state is unknown.
// -------------------------------------------------------------------------

func TestBallotExpiry_DoesNotRemovePendingOutbound(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	utxId := "utx-staystay"
	outboundId := "outbound-staystay"
	seedPendingOutbound(t, chainApp, ctx, utxId, outboundId)

	obs := makeObservation(true, "0xdesttx", "")
	ballotKey, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obs)
	require.NoError(t, err)
	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obs, outboundAuditVoter1, ballotKey))

	// Create a matching ballot in uvalidator and force-expire it.
	ballot := uvalidatortypes.Ballot{
		Id:                  ballotKey,
		BallotType:          uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		EligibleVoters:      []string{outboundAuditVoter1},
		Votes:               []uvalidatortypes.VoteResult{uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS},
		VotingThreshold:     1,
		Status:              uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated:  ctx.BlockHeight(),
		BlockHeightExpiry:   ctx.BlockHeight() + 100,
	}
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballotKey, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballotKey))

	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballotKey))

	// THE KEY ASSERTION: PendingOutbounds entry must STILL be present.
	entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, outboundId)
	require.NoError(t, err, "PendingOutbounds entry must NOT be removed on outbound ballot expiry")
	require.Equal(t, outboundId, entry.OutboundId)
	require.Len(t, entry.Variants, 1, "variant entry preserved")
}

func TestMultiBallotExpiry_DoesNotRemovePendingOutbound(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	utxId := "utx-multistayed"
	outboundId := "outbound-multistayed"
	seedPendingOutbound(t, chainApp, ctx, utxId, outboundId)

	obsA := makeObservation(true, "0xdestA", "")
	obsB := makeObservation(false, "0xdestB", "reverted")
	ballotA, _ := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obsA)
	ballotB, _ := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obsB)

	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obsA, outboundAuditVoter1, ballotA))
	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obsB, outboundAuditVoter2, ballotB))

	for _, key := range []string{ballotA, ballotB} {
		ballot := uvalidatortypes.Ballot{
			Id:                  key,
			BallotType:          uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
			EligibleVoters:      []string{outboundAuditVoter1},
			Votes:               []uvalidatortypes.VoteResult{uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS},
			VotingThreshold:     1,
			Status:              uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
			BlockHeightCreated:  ctx.BlockHeight(),
			BlockHeightExpiry:   ctx.BlockHeight() + 100,
		}
		require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, key, ballot))
		require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, key))
		require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, key))
	}

	// PendingOutbounds entry STILL present — even with all variant ballots
	// expired, the outbound persists for operator investigation.
	entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, outboundId)
	require.NoError(t, err)
	require.Len(t, entry.Variants, 2, "audit trail intact for operator forensics")
}

// -------------------------------------------------------------------------
// Sanity: the outbound branch of BallotHooks does NOT route to ExpiredInbounds
// (which is for inbounds only).
// -------------------------------------------------------------------------

func TestBallotHook_OutboundExpiryDoesNotPopulateExpiredInbounds(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	utxId := "utx-noexp"
	outboundId := "outbound-noexp"
	seedPendingOutbound(t, chainApp, ctx, utxId, outboundId)

	obs := makeObservation(true, "0xdesttx", "")
	ballotKey, _ := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obs)
	require.NoError(t, chainApp.UexecutorKeeper.RecordOutboundVote(ctx, outboundId, obs, outboundAuditVoter1, ballotKey))

	ballot := uvalidatortypes.Ballot{
		Id:                  ballotKey,
		BallotType:          uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		EligibleVoters:      []string{outboundAuditVoter1},
		Votes:               []uvalidatortypes.VoteResult{uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS},
		VotingThreshold:     1,
		Status:              uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated:  ctx.BlockHeight(),
		BlockHeightExpiry:   ctx.BlockHeight() + 100,
	}
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballotKey, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballotKey))

	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballotKey))

	// ExpiredInbounds collection is for INBOUND_TX terminal-failure variants only.
	// An OUTBOUND_TX ballot expiring must not write anything there.
	count := 0
	require.NoError(t, chainApp.UexecutorKeeper.ExpiredInbounds.Walk(ctx, nil, func(_ string, _ uexecutortypes.ExpiredInboundEntry) (bool, error) {
		count++
		return false, nil
	}))
	require.Equal(t, 0, count, "OUTBOUND_TX terminal hook must not populate ExpiredInbounds")
}
