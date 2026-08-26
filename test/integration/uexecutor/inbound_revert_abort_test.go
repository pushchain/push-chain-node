package integrationtest

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// Regression coverage for F-2026-18823.
//
// buildRevertOutbound used to fail open: when it could not resolve the revert's
// gas metadata it logged "proceeding without gas fields" and returned the
// outbound anyway, still marked PENDING. attachOutboundsToUtx then indexed it
// into PendingOutbounds unconditionally, where the universal validators refused
// to sign it ("gas price is zero or missing"). The row could never leave the
// queue: no ballot forms for an unsignable outbound and there is no admin abort
// for outbounds. Worse, non-CEA rescue was gated on an INBOUND_REVERT having
// reached REVERTED, so the user had no recovery route either.
//
// The revert is now recorded ABORTED with a reason, kept off the signing queue,
// and accepted by the rescue gate.
//
// NOTE ON THIS ENVIRONMENT: the UniversalCore contract deployed by the test
// harness cannot serve getOutboundTxGasAndFees (its PRC20 stub has no
// SOURCE_CHAIN_NAMESPACE), so every INBOUND_REVERT built here takes the abort
// path. That makes the failure realistic end-to-end but means the resolvable
// path cannot be exercised at this level; it is covered by
// x/uexecutor/keeper/build_revert_outbound_test.go, which drives the same
// function with the gas lookup mocked both ways.

// requireNotQueuedForSigning asserts that an outbound was never indexed into
// PendingOutbounds, i.e. it will not be picked up for TSS signing.
func requireNotQueuedForSigning(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, outboundId string) {
	t.Helper()
	has, err := chainApp.UexecutorKeeper.PendingOutbounds.Has(ctx, outboundId)
	require.NoError(t, err)
	require.False(t, has,
		"outbound %s must not be indexed in PendingOutbounds: it can never be signed and nothing would ever remove it", outboundId)
}

// findInboundRevert returns the INBOUND_REVERT outbound on a UTX, if any.
func findInboundRevert(utx uexecutortypes.UniversalTx) *uexecutortypes.OutboundTx {
	for _, ob := range utx.OutboundTx {
		if ob != nil && ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
			return ob
		}
	}
	return nil
}

// driveNonCEAInboundToAbortedRevert votes a non-CEA FUNDS inbound with an empty
// recipient to quorum. Execution validation rejects it, so an INBOUND_REVERT is
// built — and since the harness cannot serve gas metadata, that revert aborts.
//
// The token/chain config is deliberately left registered so the failure is the
// gas lookup alone; the PRC20-not-found variant is covered in
// vote_inbound_validation_test.go.
func driveNonCEAInboundToAbortedRevert(t *testing.T, txHash string) (*app.ChainApp, sdk.Context, string) {
	t.Helper()

	chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)
	inbound.TxHash = txHash
	inbound.IsCEA = false
	inbound.Recipient = "" // FUNDS requires a recipient — fails ValidateForExecution post-quorum

	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteInbound(t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), inbound))
	}

	return chainApp, ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound)
}

// TestInboundRevert_UnresolvableGasMetadata_AbortsInsteadOfQueueing is the
// headline regression test: the revert must be recorded ABORTED with a reason
// and must never reach PendingOutbounds.
func TestInboundRevert_UnresolvableGasMetadata_AbortsInsteadOfQueueing(t *testing.T) {
	chainApp, ctx, utxId := driveNonCEAInboundToAbortedRevert(t, "0xabortrevert01")

	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found, "UTX must exist after quorum")

	revert := findInboundRevert(utx)
	require.NotNil(t, revert, "a failed non-CEA inbound must still record an INBOUND_REVERT attempt")

	require.Equal(t, uexecutortypes.Status_ABORTED, revert.OutboundStatus,
		"a revert whose gas metadata could not be resolved must be ABORTED, never PENDING")
	require.NotEmpty(t, revert.AbortReason, "the abort reason must say why the revert could not be built")
	require.Contains(t, revert.AbortReason, "gas fee info",
		"the reason must name the lookup that failed")

	// Fail-closed: the gas fields stay empty rather than being half-written.
	require.Empty(t, revert.GasToken)
	require.Empty(t, revert.GasFee)
	require.Empty(t, revert.GasPrice)
	require.Empty(t, revert.GasLimit)

	requireNotQueuedForSigning(t, chainApp, ctx, revert.Id)

	// The whole queue stays clean, not just this id.
	err = chainApp.UexecutorKeeper.PendingOutbounds.Walk(ctx, nil, func(id string, _ uexecutortypes.PendingOutboundEntry) (bool, error) {
		t.Fatalf("PendingOutbounds must be empty, found %s", id)
		return true, nil
	})
	require.NoError(t, err)
}

// TestInboundRevert_AbortedRevert_UnlocksRescue proves the other half of the
// fix: skipping the queue is not enough on its own, because non-CEA rescue used
// to require a REVERTED inbound-revert. An ABORTED one must now be accepted, or
// the user is left with a clean queue and no way out.
func TestInboundRevert_AbortedRevert_UnlocksRescue(t *testing.T) {
	chainApp, ctx, utxId := driveNonCEAInboundToAbortedRevert(t, "0xabortrevert02")

	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found)
	revert := findInboundRevert(utx)
	require.NotNil(t, revert)
	require.Equal(t, uexecutortypes.Status_ABORTED, revert.OutboundStatus,
		"precondition: the revert must have aborted for this test to mean anything")

	prc20Addr := utils.GetDefaultAddresses().PRC20USDCAddr
	senderAddr := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)
	log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
		"eip155", big.NewInt(333), big.NewInt(1_000_000_000), big.NewInt(200_000))

	err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(
		ctx,
		makeRescueReceipt(t, "0xrescueafterabort", log),
		uexecutortypes.PCTx{TxHash: "0xrescueafterabort", Status: "SUCCESS"},
	)
	require.NoError(t, err, "rescue must be accepted when the auto-revert aborted; the funds never came back")

	utx, _, err = chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)

	rescue := findRescueOutbound(utx)
	require.NotNil(t, rescue, "a RESCUE_FUNDS outbound must be attached")
	require.Equal(t, uexecutortypes.Status_PENDING, rescue.OutboundStatus,
		"the rescue itself is signable and must be queued")
	require.Equal(t, "333", rescue.GasFee)

	// The rescue is queued; the aborted revert still is not.
	has, err := chainApp.UexecutorKeeper.PendingOutbounds.Has(ctx, rescue.Id)
	require.NoError(t, err)
	require.True(t, has, "the rescue outbound must be indexed for UV pickup")
	requireNotQueuedForSigning(t, chainApp, ctx, revert.Id)
}
