package integrationtest

// Integration tests for the variant-aware PendingInbounds + ExpiredInbounds
// audit trail introduced for F-2026-16642 (inbound side).
//
// These tests exercise:
//   - RecordInboundVote idempotency and per-variant tracking
//   - BallotHooks.AfterBallotTerminal for INBOUND_TX
//   - PendingInbounds → ExpiredInbounds transition when all variants
//     reach a terminal-failure state (EXPIRED/REJECTED)
//   - Multi-variant scenarios (different validators voting different
//     payloads for the same logical event)
//
// See plan-pending-inbound-cleanup.md for the design doc.

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
	auditVoter1 = "cosmosvaloper1auditvoter1000000000000000000000000"
	auditVoter2 = "cosmosvaloper1auditvoter2000000000000000000000000"
	auditVoter3 = "cosmosvaloper1auditvoter3000000000000000000000000"
)

func makeInbound(txHash, sender string) uexecutortypes.Inbound {
	return uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      txHash,
		Sender:      sender,
		LogIndex:    "0",
		TxType:      uexecutortypes.TxType_FUNDS,
	}
}

// -------------------------------------------------------------------------
// RecordInboundVote — variant accumulation, idempotency
// -------------------------------------------------------------------------

func TestRecordInboundVote_FirstVoteCreatesEntryAndVariant(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)
	inbound := makeInbound("0xfresh", "0xsender")
	ballotKey, err := uexecutortypes.GetInboundBallotKey(inbound)
	require.NoError(t, err)
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inbound)

	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inbound, auditVoter1, ballotKey))

	entry, err := app.UexecutorKeeper.PendingInbounds.Get(ctx, utxKey)
	require.NoError(t, err)
	require.Equal(t, utxKey, entry.UtxKey)
	require.Len(t, entry.Variants, 1)
	require.Equal(t, ballotKey, entry.Variants[0].BallotId)
	require.Equal(t, []string{auditVoter1}, entry.Variants[0].Voters)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, entry.Variants[0].TerminalStatus)
}

func TestRecordInboundVote_SameVoterTwiceIsIdempotent(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)
	inbound := makeInbound("0xduplicate", "0xsender")
	ballotKey, err := uexecutortypes.GetInboundBallotKey(inbound)
	require.NoError(t, err)
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inbound)

	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inbound, auditVoter1, ballotKey))
	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inbound, auditVoter1, ballotKey))

	entry, err := app.UexecutorKeeper.PendingInbounds.Get(ctx, utxKey)
	require.NoError(t, err)
	require.Len(t, entry.Variants, 1)
	require.Len(t, entry.Variants[0].Voters, 1, "duplicate voter must not be re-added")
}

func TestRecordInboundVote_DifferentVotersSameVariant(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)
	inbound := makeInbound("0xshared", "0xsender")
	ballotKey, err := uexecutortypes.GetInboundBallotKey(inbound)
	require.NoError(t, err)
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inbound)

	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inbound, auditVoter1, ballotKey))
	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inbound, auditVoter2, ballotKey))

	entry, err := app.UexecutorKeeper.PendingInbounds.Get(ctx, utxKey)
	require.NoError(t, err)
	require.Len(t, entry.Variants, 1, "same payload bytes → single variant")
	require.ElementsMatch(t, []string{auditVoter1, auditVoter2}, entry.Variants[0].Voters)
}

func TestRecordInboundVote_DifferentPayloadsCreateDistinctVariants(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	// Same UTX-key fields but different sender → different ballot IDs (different
	// marshal bytes), one PendingInbounds entry, two variants.
	inboundA := makeInbound("0xsame", "0xsenderA")
	inboundB := makeInbound("0xsame", "0xsenderB")
	require.Equal(t,
		uexecutortypes.GetInboundUniversalTxKey(inboundA),
		uexecutortypes.GetInboundUniversalTxKey(inboundB),
		"both inbounds must produce the same UTX key (same source/tx/log)",
	)

	ballotKeyA, err := uexecutortypes.GetInboundBallotKey(inboundA)
	require.NoError(t, err)
	ballotKeyB, err := uexecutortypes.GetInboundBallotKey(inboundB)
	require.NoError(t, err)
	require.NotEqual(t, ballotKeyA, ballotKeyB, "different payloads must produce different ballot keys")

	utxKey := uexecutortypes.GetInboundUniversalTxKey(inboundA)

	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inboundA, auditVoter1, ballotKeyA))
	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inboundB, auditVoter2, ballotKeyB))

	entry, err := app.UexecutorKeeper.PendingInbounds.Get(ctx, utxKey)
	require.NoError(t, err)
	require.Len(t, entry.Variants, 2)

	byBallot := make(map[string]uexecutortypes.InboundVariant, 2)
	for _, v := range entry.Variants {
		byBallot[v.BallotId] = v
	}
	require.Equal(t, []string{auditVoter1}, byBallot[ballotKeyA].Voters)
	require.Equal(t, []string{auditVoter2}, byBallot[ballotKeyB].Voters)
}

func TestIsPendingInbound_ReportsEntryPresence(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)
	inbound := makeInbound("0xpresent", "0xsender")
	ballotKey, err := uexecutortypes.GetInboundBallotKey(inbound)
	require.NoError(t, err)

	pending, err := app.UexecutorKeeper.IsPendingInbound(ctx, inbound)
	require.NoError(t, err)
	require.False(t, pending, "no entry yet → not pending")

	require.NoError(t, app.UexecutorKeeper.RecordInboundVote(ctx, inbound, auditVoter1, ballotKey))

	pending, err = app.UexecutorKeeper.IsPendingInbound(ctx, inbound)
	require.NoError(t, err)
	require.True(t, pending, "entry exists → pending")
}

// -------------------------------------------------------------------------
// BallotHooks: terminal transitions move PendingInbounds → ExpiredInbounds
// -------------------------------------------------------------------------

// seedPendingBallot records a vote AND creates a matching uvalidator ballot
// at PENDING status for the given inbound, returning the ballot ID. Tests
// then synthesize terminal transitions by calling MarkBallotExpired or
// MarkBallotFinalized directly (since DefaultExpiryAfterBlocks is too long
// to drive in a unit test).
func seedPendingBallot(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	inbound uexecutortypes.Inbound,
	voter string,
) string {
	t.Helper()

	ballotKey, err := uexecutortypes.GetInboundBallotKey(inbound)
	require.NoError(t, err)

	// Record the variant in PendingInbounds.
	require.NoError(t, chainApp.UexecutorKeeper.RecordInboundVote(ctx, inbound, voter, ballotKey))

	// Create a matching ballot in uvalidator at PENDING status. We bypass
	// VoteOnBallot (which has its own quorum/threshold logic) by writing the
	// ballot directly so the test controls the terminal transition.
	ballot := uvalidatortypes.Ballot{
		Id:                  ballotKey,
		BallotType:          uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		EligibleVoters:      []string{voter},
		Votes:               []uvalidatortypes.VoteResult{uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS},
		VotingThreshold:     1,
		Status:              uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated:  ctx.BlockHeight(),
		BlockHeightExpiry:   ctx.BlockHeight() + 100,
	}
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballotKey, ballot))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballotKey))

	return ballotKey
}

func TestBallotHook_SingleVariantExpiredRoutesToExpiredInbounds(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	inbound := makeInbound("0xtoexpire", "0xsender")
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inbound)

	ballotKey := seedPendingBallot(t, chainApp, ctx, inbound, auditVoter1)

	// Synthesize the terminal transition.
	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballotKey))

	// PendingInbounds should be empty for this UTX key.
	has, err := chainApp.UexecutorKeeper.PendingInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, has, "expired-only single-variant entry must be removed from PendingInbounds")

	// ExpiredInbounds should now hold the entry with EXPIRED terminal status.
	expired, err := chainApp.UexecutorKeeper.ExpiredInbounds.Get(ctx, utxKey)
	require.NoError(t, err)
	require.Equal(t, utxKey, expired.UtxKey)
	require.Len(t, expired.Variants, 1)
	require.Equal(t, ballotKey, expired.Variants[0].BallotId)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, expired.Variants[0].TerminalStatus)
	require.Equal(t, []string{auditVoter1}, expired.Variants[0].Voters)
}

func TestBallotHook_SingleVariantRejectedRoutesToExpiredInbounds(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	inbound := makeInbound("0xtoreject", "0xsender")
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inbound)

	ballotKey := seedPendingBallot(t, chainApp, ctx, inbound, auditVoter1)

	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotFinalized(ctx, ballotKey, uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED))

	has, err := chainApp.UexecutorKeeper.PendingInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, has)

	expired, err := chainApp.UexecutorKeeper.ExpiredInbounds.Get(ctx, utxKey)
	require.NoError(t, err)
	require.Len(t, expired.Variants, 1)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED, expired.Variants[0].TerminalStatus)
}

func TestBallotHook_PassedDoesNotRouteToExpiredInbounds(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	inbound := makeInbound("0xtopass", "0xsender")
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inbound)

	ballotKey := seedPendingBallot(t, chainApp, ctx, inbound, auditVoter1)

	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotFinalized(ctx, ballotKey, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))

	// PendingInbounds entry is removed by the hook because all (one) variants are now terminal.
	has, err := chainApp.UexecutorKeeper.PendingInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, has)

	// PASSED variants are NOT routed to ExpiredInbounds — the existing post-finalization
	// path produced (or will produce) a UniversalTx instead.
	hasExpired, err := chainApp.UexecutorKeeper.ExpiredInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, hasExpired, "PASSED ballot must not route to ExpiredInbounds")
}

// -------------------------------------------------------------------------
// Multi-variant scenarios
// -------------------------------------------------------------------------

func TestBallotHook_MultiVariant_OneTerminalOthersStillPending(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	inboundA := makeInbound("0xmulti", "0xsenderA")
	inboundB := makeInbound("0xmulti", "0xsenderB") // same UTX key, different ballot
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inboundA)

	ballotA := seedPendingBallot(t, chainApp, ctx, inboundA, auditVoter1)
	ballotB := seedPendingBallot(t, chainApp, ctx, inboundB, auditVoter2)

	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballotA))

	// Entry must remain in PendingInbounds because variant B is still PENDING.
	entry, err := chainApp.UexecutorKeeper.PendingInbounds.Get(ctx, utxKey)
	require.NoError(t, err, "entry must remain while any variant is still PENDING")
	require.Len(t, entry.Variants, 2)

	statusByBallot := make(map[string]uvalidatortypes.BallotStatus, 2)
	for _, v := range entry.Variants {
		statusByBallot[v.BallotId] = v.TerminalStatus
	}
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, statusByBallot[ballotA])
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, statusByBallot[ballotB])

	// ExpiredInbounds must NOT have an entry yet.
	hasExpired, err := chainApp.UexecutorKeeper.ExpiredInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, hasExpired, "ExpiredInbounds must wait for all variants to terminate")
}

func TestBallotHook_MultiVariant_AllExpiredRoutesEntireEntry(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	inboundA := makeInbound("0xallexp", "0xsenderA")
	inboundB := makeInbound("0xallexp", "0xsenderB")
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inboundA)

	ballotA := seedPendingBallot(t, chainApp, ctx, inboundA, auditVoter1)
	ballotB := seedPendingBallot(t, chainApp, ctx, inboundB, auditVoter2)

	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballotA))
	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballotB))

	hasPending, err := chainApp.UexecutorKeeper.PendingInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, hasPending, "entry must be removed once all variants are terminal")

	expired, err := chainApp.UexecutorKeeper.ExpiredInbounds.Get(ctx, utxKey)
	require.NoError(t, err)
	require.Len(t, expired.Variants, 2, "ExpiredInbounds preserves the full audit trail")
	for _, v := range expired.Variants {
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, v.TerminalStatus)
	}
}

func TestBallotHook_MultiVariant_OnePassesOthersExpire_NotRoutedToExpired(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	inboundA := makeInbound("0xmixed", "0xsenderA")
	inboundB := makeInbound("0xmixed", "0xsenderB")
	utxKey := uexecutortypes.GetInboundUniversalTxKey(inboundA)

	ballotA := seedPendingBallot(t, chainApp, ctx, inboundA, auditVoter1)
	ballotB := seedPendingBallot(t, chainApp, ctx, inboundB, auditVoter2)

	// Variant A passes (would produce a UTX in real flow), variant B expires.
	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotFinalized(ctx, ballotA, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED))
	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballotB))

	// Entry removed from PendingInbounds (all variants terminal).
	hasPending, err := chainApp.UexecutorKeeper.PendingInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, hasPending)

	// But NOT routed to ExpiredInbounds because at least one variant PASSED.
	hasExpired, err := chainApp.UexecutorKeeper.ExpiredInbounds.Has(ctx, utxKey)
	require.NoError(t, err)
	require.False(t, hasExpired, "PASSED variant suppresses ExpiredInbounds routing")
}

// -------------------------------------------------------------------------
// AllExpiredInbounds query
// -------------------------------------------------------------------------

func TestQueryAllExpiredInbounds(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)

	inbound1 := makeInbound("0xq1", "0xsender")
	inbound2 := makeInbound("0xq2", "0xsender")

	ballot1 := seedPendingBallot(t, chainApp, ctx, inbound1, auditVoter1)
	ballot2 := seedPendingBallot(t, chainApp, ctx, inbound2, auditVoter2)

	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballot1))
	require.NoError(t, chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballot2))

	resp, err := chainApp.UexecutorKeeper.AllExpiredInbounds(
		sdk.WrapSDKContext(ctx),
		&uexecutortypes.QueryAllExpiredInboundsRequest{},
	)
	require.NoError(t, err)
	require.Len(t, resp.Entries, 2)
}
