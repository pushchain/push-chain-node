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
	require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus, "outbound must start PENDING so UVs sign it")
	require.Equal(t, inbound.SourceChain, ob.DestinationChain, "revert goes back to the source chain")
	require.Equal(t, inbound.RevertInstructions.FundRecipient, ob.Recipient,
		"recipient must use RevertInstructions.FundRecipient when set")
	require.Equal(t, inbound.Amount, ob.Amount, "full amount refunded")
	require.Equal(t, inbound.AssetAddr, ob.ExternalAssetAddr, "external asset addr must match the original deposit asset")
	require.Equal(t, chainutils.LenientCanonicalizeEVMAddress(inbound.Sender), ob.Sender, "sender field carries original depositor")

	// --- PendingOutbounds index assertions ---
	pending, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, ob.Id)
	require.NoError(t, err, "revert outbound must be indexed in PendingOutbounds for UV pickup")
	require.Equal(t, ob.Id, pending.OutboundId)
	require.Equal(t, utx.Id, pending.UniversalTxId)
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
