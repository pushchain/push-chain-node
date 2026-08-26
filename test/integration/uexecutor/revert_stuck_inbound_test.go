package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	chainutils "github.com/pushchain/push-chain-node/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// setupRevertStuckInbound builds a chain app with uregistry seeded for the
// source chain + USDC token, sets the uvalidator admin, and returns a sample
// Inbound payload ready for the revert scenarios below.
func setupRevertStuckInbound(t *testing.T) (chainApp *app.ChainApp, ctx sdk.Context, inbound *uexecutortypes.Inbound, admin string) {
	t.Helper()
	chainApp, ctx, _, _ = utils.SetAppWithMultipleValidators(t, 1)

	chainConfig := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound: 5, StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{{
			Name: "addFunds", Identifier: "",
			EventIdentifier:  "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
			ConfirmationType: 5,
		}},
		Enabled: &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
	}
	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

	tokenConfig := uregistrytypes.TokenConfig{
		Chain:   "eip155:11155111",
		Address: usdcAddress.String(),
		Name:    "USD Coin", Symbol: "USDC", Decimals: 6, Enabled: true,
		LiquidityCap: "1000000000000000000000000", TokenType: 1,
		NativeRepresentation: &uregistrytypes.NativeRepresentation{
			ContractAddress: prc20Address.String(),
		},
	}
	require.NoError(t, chainApp.UregistryKeeper.AddChainConfig(ctx, &chainConfig))
	require.NoError(t, chainApp.UregistryKeeper.AddTokenConfig(ctx, &tokenConfig))

	admin = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, uvalidatortypes.Params{Admin: admin}))

	inbound = &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xstuck",
		Sender:      testAddress,
		Recipient:   testAddress,
		Amount:      "1000000",
		AssetAddr:   usdcAddress.String(),
		LogIndex:    "1",
		TxType:      uexecutortypes.TxType_FUNDS,
		RevertInstructions: &uexecutortypes.RevertInstructions{
			FundRecipient: testAddress,
		},
	}
	return chainApp, ctx, inbound, admin
}

// seedExpiredBallot stores an EXPIRED ballot for the given inbound.
func seedBallot(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, inbound *uexecutortypes.Inbound, status uvalidatortypes.BallotStatus) {
	t.Helper()
	ballotKey, err := uexecutortypes.GetInboundBallotKey(*inbound)
	require.NoError(t, err)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballotKey, uvalidatortypes.Ballot{
		Id:                 ballotKey,
		BallotType:         uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		EligibleVoters:     []string{},
		Votes:              []uvalidatortypes.VoteResult{},
		VotingThreshold:    0,
		Status:             status,
		BlockHeightCreated: 1,
		BlockHeightExpiry:  100_000_000,
	}))
}

// seedPendingBallotWithVotes stores a PENDING ballot carrying a real
// eligible-voter list and per-voter vote slots, which seedBallot deliberately
// leaves empty. The F-2026-18147 scenarios all turn on whether any eligible
// voter still holds a NOT_YET_VOTED slot, so they need the populated shape.
//
// The voter strings are never resolved against the staking set on this path —
// RevertStuckInbound only reads Status/EligibleVoters/Votes off the ballot.
func seedPendingBallotWithVotes(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	inbound *uexecutortypes.Inbound,
	status uvalidatortypes.BallotStatus,
	voters []string,
	votes []uvalidatortypes.VoteResult,
	threshold int64,
) {
	t.Helper()
	require.Len(t, votes, len(voters), "each eligible voter needs exactly one vote slot")
	ballotKey, err := uexecutortypes.GetInboundBallotKey(*inbound)
	require.NoError(t, err)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballotKey, uvalidatortypes.Ballot{
		Id:                 ballotKey,
		BallotType:         uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		EligibleVoters:     voters,
		Votes:              votes,
		VotingThreshold:    threshold,
		Status:             status,
		BlockHeightCreated: 1,
		BlockHeightExpiry:  100_000_000,
	}))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballotKey))
}

// threeVoters is the eligible-voter list shared by the F-2026-18147 scenarios.
func threeVoters() []string {
	return []string{"cosmosvaloper1aaa", "cosmosvaloper1bbb", "cosmosvaloper1ccc"}
}

// TestRevertStuckInbound_PendingUnreachable_ThresholdMet_CreatesRevertOutbound
// is the headline F-2026-18147 case.
//
// RecomputeBallotQuorum preserves the votes of still-eligible voters, lowers the
// threshold, and returns PENDING without ever calling CheckIfFinalizingVote. The
// shape reproduced here is what that leaves behind in the worst case: every
// eligible voter has voted YES and the preserved YES count already clears the
// recomputed threshold, so the ballot *should* have passed — but Ballot.AddVote
// rejects repeat votes, so no further vote can ever be cast and nothing will
// move it off PENDING. Natural expiry is 100M blocks away.
//
// Before this fix the admin hatch required EXPIRED, and recompute only expires a
// ballot at zero eligible voters, so the deposit was stranded permanently.
func TestRevertStuckInbound_PendingUnreachable_ThresholdMet_CreatesRevertOutbound(t *testing.T) {
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

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err, "an unreachable PENDING ballot must be revertible")
	require.NotEmpty(t, resp.UtxId)
	require.NotEmpty(t, resp.OutboundId)

	// --- UTX assertions ---
	utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.NoError(t, err)
	require.Equal(t, uexecutortypes.GetInboundUniversalTxKey(*inbound), utx.Id)
	require.NotNil(t, utx.InboundTx)
	require.Equal(t, inbound.TxHash, utx.InboundTx.TxHash)

	require.Len(t, utx.PcTx, 1)
	require.Equal(t, "FAILED", utx.PcTx[0].Status)
	require.Contains(t, utx.PcTx[0].ErrorMsg, "unreachable",
		"the audit trail must record WHY the hatch opened, not the expired wording")

	// --- Revert outbound assertions ---
	require.Len(t, utx.OutboundTx, 1)
	ob := utx.OutboundTx[0]
	require.Equal(t, resp.OutboundId, ob.Id)
	require.Equal(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType)
	// The harness's UniversalCore stub cannot serve getOutboundTxGasAndFees, so the
	// revert's gas metadata is unresolvable and F-2026-18823 records it ABORTED
	// rather than queueing it for a signature it could never receive. What this test
	// pins is that the unreachable-PENDING hatch BUILDS the revert at all; the
	// resolvable (PENDING) path is covered by
	// x/uexecutor/keeper/build_revert_outbound_test.go.
	require.Equal(t, uexecutortypes.Status_ABORTED, ob.OutboundStatus,
		"a revert with unresolvable gas metadata must be ABORTED, not PENDING")
	require.NotEmpty(t, ob.AbortReason, "ABORTED revert must record why it could not be built")
	require.Equal(t, inbound.SourceChain, ob.DestinationChain)
	require.Equal(t, inbound.RevertInstructions.FundRecipient, ob.Recipient)
	require.Equal(t, inbound.Amount, ob.Amount)
	require.Equal(t, inbound.AssetAddr, ob.ExternalAssetAddr)

	// --- PendingOutbounds index ---
	// An ABORTED revert must stay out of the signing queue: no ballot can form for
	// it and there is no admin abort for outbounds, so an indexed row would be
	// permanently stuck.
	has, err := chainApp.UexecutorKeeper.PendingOutbounds.Has(ctx, ob.Id)
	require.NoError(t, err)
	require.False(t, has, "an ABORTED revert must not be indexed in PendingOutbounds")
}

// TestRevertStuckInbound_PendingUnreachable_BelowThreshold_Accepted covers the
// second stuck shape: every eligible voter has voted, but the YES count never
// reached the threshold and the NO count never reached it either, so
// IsFinalizingVote fires for neither branch. Reachable without any recompute at
// all — 3 voters, threshold 3, one dissenting FAILURE vote.
//
// Unreachability, not vote arithmetic, is the predicate; both shapes qualify.
func TestRevertStuckInbound_PendingUnreachable_BelowThreshold_Accepted(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
		},
		3, // YES (2) < 3, NO (1) < 3 → neither branch of IsFinalizingVote fires
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err, "a fully-voted PENDING ballot below threshold is equally unreachable")

	utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.NoError(t, err)
	require.Len(t, utx.OutboundTx, 1)
	require.Equal(t, uexecutortypes.TxType_INBOUND_REVERT, utx.OutboundTx[0].TxType)

	// Unresolvable gas metadata in this harness means the revert is ABORTED and so
	// deliberately not queued (F-2026-18823); the hatch opening is what matters here.
	require.Equal(t, uexecutortypes.Status_ABORTED, utx.OutboundTx[0].OutboundStatus)
	has, err := chainApp.UexecutorKeeper.PendingOutbounds.Has(ctx, utx.OutboundTx[0].Id)
	require.NoError(t, err)
	require.False(t, has, "an ABORTED revert must not be indexed in PendingOutbounds")
}

// TestRevertStuckInbound_PendingWithUnvotedVoter_Refused is the guard against
// widening the hatch too far.
//
// This ballot is deliberately the most tempting possible refusal: the YES votes
// already clear the threshold, so it *looks* exactly like the headline case. It
// is not — one eligible voter still holds a NOT_YET_VOTED slot, so a single
// normal VoteOnBallot finalizes it through the proper VoteInbound pipeline,
// which mints and executes rather than refunding. Admin revert must not race
// that. This is also the shape Hacken's no-code workaround produces: add an
// eligible UV, recompute, and the new voter arrives NOT_YET_VOTED.
func TestRevertStuckInbound_PendingWithUnvotedVoter_Refused(t *testing.T) {
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
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "a PENDING ballot with an unvoted eligible voter can still finalize; admin revert must refuse it")
	require.Contains(t, err.Error(), "admin revert requires EXPIRED")

	// The refusal must be total: no UTX, so no revert outbound can be signed.
	utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
	has, hErr := chainApp.UexecutorKeeper.HasUniversalTx(ctx, utxKey)
	require.NoError(t, hErr)
	require.False(t, has, "a refused revert must not leave a UniversalTx behind")
}

// TestRevertStuckInbound_RejectedBallot_FullyVoted_StillRefused re-pins the
// F-2026-18801 refusal against the new predicate.
//
// A REJECTED ballot is fully voted by construction, so the "every eligible voter
// has voted" test on its own would let it through. It must not: REJECTED means a
// supermajority affirmatively voted the observation invalid, and refunding would
// pay out of the TSS vault against a deposit the validator set concluded never
// happened. PENDING-unreachable is the opposite case — nobody can act at all.
// The status guard in IsUnreachablePending is what keeps them apart.
func TestRevertStuckInbound_RejectedBallot_FullyVoted_StillRefused(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
			uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
			uvalidatortypes.VoteResult_VOTE_RESULT_FAILURE,
		},
		2,
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "REJECTED stays refused however its vote slots are filled (F-2026-18801)")
	require.Contains(t, err.Error(), "admin revert requires EXPIRED")

	utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
	has, hErr := chainApp.UexecutorKeeper.HasUniversalTx(ctx, utxKey)
	require.NoError(t, hErr)
	require.False(t, has, "a refused revert must not leave a UniversalTx behind")
}

// TestRevertStuckInbound_ExpiredBallot_FullyVoted_StillAccepted keeps the
// original precondition intact under the new switch: EXPIRED is accepted on its
// status alone, and still records the expired wording rather than the
// unreachable-pending wording.
func TestRevertStuckInbound_ExpiredBallot_FullyVoted_StillAccepted(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedPendingBallotWithVotes(t, chainApp, ctx, inbound,
		uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED,
		threeVoters(),
		[]uvalidatortypes.VoteResult{
			uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS,
			uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
			uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED,
		},
		3,
	)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)

	utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.NoError(t, err)
	require.Len(t, utx.PcTx, 1)
	require.Contains(t, utx.PcTx[0].ErrorMsg, "expired")
	require.Len(t, utx.OutboundTx, 1)
	require.Equal(t, uexecutortypes.TxType_INBOUND_REVERT, utx.OutboundTx[0].TxType)
}

func TestRevertStuckInbound_HappyPath_ExpiredBallot_CreatesRevertOutbound(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedBallot(t, chainApp, ctx, inbound, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.UtxId)
	require.NotEmpty(t, resp.OutboundId)

	// --- UTX assertions ---
	utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.NoError(t, err)
	require.Equal(t, resp.UtxId, utx.Id, "UTX id should match response")
	require.Equal(t, uexecutortypes.GetInboundUniversalTxKey(*inbound), utx.Id,
		"UTX id must be deterministically derived from the inbound")

	require.NotNil(t, utx.InboundTx)
	require.Equal(t, inbound.TxHash, utx.InboundTx.TxHash)
	require.Equal(t, inbound.SourceChain, utx.InboundTx.SourceChain)
	require.Equal(t, inbound.AssetAddr, utx.InboundTx.AssetAddr)

	require.Len(t, utx.PcTx, 1)
	require.Equal(t, "FAILED", utx.PcTx[0].Status, "PCTx must indicate the original execution failed")
	require.Contains(t, utx.PcTx[0].ErrorMsg, "admin revert")

	// --- Revert outbound assertions ---
	require.Len(t, utx.OutboundTx, 1)
	ob := utx.OutboundTx[0]
	require.Equal(t, resp.OutboundId, ob.Id, "outbound id should match response")
	require.Equal(t, uexecutortypes.GetOutboundRevertId(inbound.SourceChain, inbound.TxHash, inbound.LogIndex), ob.Id,
		"outbound id must follow the canonical revert-id format")
	require.Equal(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType, "outbound type must be INBOUND_REVERT")
	// The harness's UniversalCore stub cannot serve getOutboundTxGasAndFees, so the
	// revert's gas metadata is unresolvable and it is recorded ABORTED rather than
	// queued for a signature it could never receive. The admin message still reports
	// the outbound it created, and the UTX becomes eligible for RESCUE_FUNDS. The
	// resolvable (PENDING) path is covered by
	// x/uexecutor/keeper/build_revert_outbound_test.go.
	require.Equal(t, uexecutortypes.Status_ABORTED, ob.OutboundStatus,
		"a revert with unresolvable gas metadata must be ABORTED, not PENDING")
	require.NotEmpty(t, ob.AbortReason, "ABORTED revert must record why it could not be built")
	require.Equal(t, inbound.SourceChain, ob.DestinationChain, "revert goes back to the source chain")
	require.Equal(t, inbound.RevertInstructions.FundRecipient, ob.Recipient,
		"recipient must use RevertInstructions.FundRecipient when set")
	require.Equal(t, inbound.Amount, ob.Amount, "full amount refunded")
	require.Equal(t, inbound.AssetAddr, ob.ExternalAssetAddr, "external asset addr must match the original deposit asset")
	require.Equal(t, chainutils.LenientCanonicalizeEVMAddress(inbound.Sender), ob.Sender, "sender field carries original depositor")

	// --- PendingOutbounds index assertions ---
	// An ABORTED revert must stay out of the signing queue: no ballot can ever form
	// for it and there is no admin abort for outbounds, so an indexed row would be
	// permanently stuck.
	has, err := chainApp.UexecutorKeeper.PendingOutbounds.Has(ctx, ob.Id)
	require.NoError(t, err)
	require.False(t, has, "an ABORTED revert must not be indexed in PendingOutbounds")
}

// TestRevertStuckInbound_RecipientFallback_UsesSender covers the case where
// the inbound has no RevertInstructions.FundRecipient — the revert should
// refund to inbound.Sender instead.
func TestRevertStuckInbound_RecipientFallback_UsesSender(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	// Strip the FundRecipient to force fallback to Sender.
	inbound.RevertInstructions = nil
	seedBallot(t, chainApp, ctx, inbound, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)

	utx, _, _ := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.Len(t, utx.OutboundTx, 1)
	require.Equal(t, chainutils.LenientCanonicalizeEVMAddress(inbound.Sender), utx.OutboundTx[0].Recipient,
		"with no RevertInstructions, refund goes to original sender")
}

// TestRevertStuckInbound_DuplicateRevert_Rejected verifies idempotency: a
// second revert attempt for the same inbound rejects because the UTX already
// exists. Prevents accidentally creating multiple refunds.
func TestRevertStuckInbound_DuplicateRevert_Rejected(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedBallot(t, chainApp, ctx, inbound, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)

	// First revert succeeds.
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)

	// Second revert for the same inbound must fail.
	_, err = ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists",
		"second revert must reject because UTX is already present")
}

func TestRevertStuckInbound_AdminAuth_RejectsNonAdmin(t *testing.T) {
	chainApp, ctx, inbound, _ := setupRevertStuckInbound(t)
	seedBallot(t, chainApp, ctx, inbound, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED)

	const notAdmin = "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  notAdmin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid admin")
}

func TestRevertStuckInbound_BallotNotFound(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	// no ballot seeded
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ballot for inbound not found")
}

func TestRevertStuckInbound_PendingBallot_Rejected(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedBallot(t, chainApp, ctx, inbound, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires EXPIRED")
}

func TestRevertStuckInbound_PassedBallot_Rejected(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedBallot(t, chainApp, ctx, inbound, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires EXPIRED")
}

func TestRevertStuckInbound_NilInbound_Rejected(t *testing.T) {
	chainApp, ctx, _, admin := setupRevertStuckInbound(t)
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbound is required")
}

// E2E: stuck PENDING ballot → admin recompute (0 eligible) → auto-expired →
// admin revert → revert outbound in pending queue, ready for UV TSS signing.
func TestRevertStuckInbound_RecomputeThenRevert_E2E(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)

	// Seed a stuck PENDING ballot whose eligible voters are valopers that
	// don't exist in the UV set → recompute will produce 0 eligible → auto-expire.
	ballotKey, _ := uexecutortypes.GetInboundBallotKey(*inbound)
	require.NoError(t, chainApp.UvalidatorKeeper.Ballots.Set(ctx, ballotKey, uvalidatortypes.Ballot{
		Id:                 ballotKey,
		BallotType:         uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		EligibleVoters:     []string{"cosmosvaloper1stranded", "cosmosvaloper2stranded"},
		Votes:              []uvalidatortypes.VoteResult{uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED, uvalidatortypes.VoteResult_VOTE_RESULT_NOT_YET_VOTED},
		VotingThreshold:    2,
		Status:             uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated: 1,
		BlockHeightExpiry:  100_000_000,
	}))
	require.NoError(t, chainApp.UvalidatorKeeper.ActiveBallotIDs.Set(ctx, ballotKey))

	// Step 1: recompute. The lone bonded UV in this test isn't in the ballot's
	// stranded-voter list, so this scenario only has 1 actual eligible voter.
	// To force a 0-eligible recompute we unbond that one too.
	stakingVals, _ := chainApp.StakingKeeper.GetAllValidators(ctx)
	require.NotEmpty(t, stakingVals)
	stakingVals[0].Status = 1 // sdk staking Unbonded = iota 1; explicit value to avoid extra import
	require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, stakingVals[0]))

	_, newEligible, _, _, newStatus, err := chainApp.UvalidatorKeeper.RecomputeBallotQuorum(ctx, ballotKey)
	require.NoError(t, err)
	require.Equal(t, int64(0), newEligible)
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED, newStatus)

	// Step 2: admin reverts.
	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	resp, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.UtxId)

	utx, _, _ := chainApp.UexecutorKeeper.GetUniversalTx(ctx, resp.UtxId)
	require.Len(t, utx.OutboundTx, 1)
	require.Equal(t, uexecutortypes.TxType_INBOUND_REVERT, utx.OutboundTx[0].TxType)
}

// TestRevertStuckInbound_RejectedBallot_RefusedDeliberately pins the refusal
// documented for F-2026-18801.
//
// The terminal-routing hook files BOTH terminal-failure statuses into
// ExpiredInbounds, but the admin hatch accepts only EXPIRED. That asymmetry is
// intentional, and this test exists so a future change cannot quietly relax it:
//
//   - EXPIRED is uncertainty. Quorum never formed, the deposit may be real, the
//     funds may be stuck in the source gateway. Refunding is correct.
//   - REJECTED is a supermajority asserting the observation is invalid. A revert
//     outbound there would pay out of the TSS vault against a deposit the
//     validator set concluded never happened.
//
// Note this state is unreachable for inbounds today (VoteOnInboundBallot
// hardcodes VOTE_RESULT_SUCCESS, so threshold-FAILURE never fires); the ballot is
// seeded directly here precisely because no vote path can produce it. If inbound
// negative voting is ever added, this test is the place the design decision has
// to be re-made rather than inherited.
func TestRevertStuckInbound_RejectedBallot_RefusedDeliberately(t *testing.T) {
	chainApp, ctx, inbound, admin := setupRevertStuckInbound(t)
	seedBallot(t, chainApp, ctx, inbound, uvalidatortypes.BallotStatus_BALLOT_STATUS_REJECTED)

	ms := uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper)
	_, err := ms.RevertStuckInbound(sdk.WrapSDKContext(ctx), &uexecutortypes.MsgRevertStuckInbound{
		Signer:  admin,
		Inbound: inbound,
	})
	require.Error(t, err, "admin revert must refuse a REJECTED ballot")
	require.Contains(t, err.Error(), "admin revert requires EXPIRED",
		"the refusal must name the required status so an operator knows why")

	// The refusal must be total: no UTX, and therefore no revert outbound that
	// could later be signed and broadcast.
	utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
	has, hErr := chainApp.UexecutorKeeper.HasUniversalTx(ctx, utxKey)
	require.NoError(t, hErr)
	require.False(t, has, "a refused revert must not leave a UniversalTx behind")
}
