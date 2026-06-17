package integrationtest

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// Ballot-convergence regression: validators observing the SAME bridge event
// but submitting different string encodings (EIP-55 vs lowercase vs
// 0X-uppercase) must aggregate on ONE ballot and finalize. Pre-fix, the
// ballot key hashed the full proto encoding, so each encoding variant
// produced its own ballot and quorum never formed.
func TestVoteInbound_EncodingVariantsConvergeOnOneBallot(t *testing.T) {
	app, ctx, vals, baseInbound, coreVals := setupInboundBridgeTest(t, 4)

	// Same logical event in three different encodings.
	const txLower = "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	variants := make([]uexecutortypes.Inbound, 3)
	for i := range variants {
		v := *baseInbound
		v.RevertInstructions = &uexecutortypes.RevertInstructions{
			FundRecipient: baseInbound.RevertInstructions.FundRecipient,
		}
		variants[i] = v
	}

	// Voter 0: all lowercase.
	variants[0].TxHash = txLower
	variants[0].Sender = strings.ToLower(baseInbound.Sender)
	variants[0].AssetAddr = strings.ToLower(baseInbound.AssetAddr)
	variants[0].Recipient = strings.ToLower(baseInbound.Recipient)
	variants[0].RevertInstructions.FundRecipient = strings.ToLower(baseInbound.RevertInstructions.FundRecipient)

	// Voter 1: as produced by the EVM client (EIP-55 mixed case).
	variants[1].TxHash = "0xB28F49668E7E76DC96D7AABE5B7F63FECFBD1C3574774C05E8204E749FD96FBD"

	// Voter 2: 0X-uppercase everything.
	variants[2].TxHash = "0X" + strings.ToUpper(txLower[2:])
	variants[2].Sender = "0X" + strings.ToUpper(baseInbound.Sender[2:])
	variants[2].AssetAddr = "0X" + strings.ToUpper(baseInbound.AssetAddr[2:])

	// Vote with 3 of 4 validators (votesNeeded = (2*4)/3+1 = 3), each using a
	// different encoding of the same event.
	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		require.NoError(t, utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, &variants[i]),
			"vote %d with encoding variant must be accepted", i)

		// Mid-flight (after the first two votes): the audit trail must show a
		// SINGLE variant — both encodings recorded as the same observation.
		if i == 1 {
			utxKey := uexecutortypes.GetInboundUniversalTxKey(variants[0])
			entry, err := app.UexecutorKeeper.PendingInbounds.Get(ctx, utxKey)
			require.NoError(t, err)
			require.Len(t, entry.Variants, 1,
				"different encodings of the same event must record as ONE variant, not fragment")
			require.Len(t, entry.Variants[0].Voters, 2)
		}
	}

	// Quorum reached on the single converged ballot → inbound executed.
	isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, variants[2])
	require.NoError(t, err)
	require.False(t, isPending, "ballot must finalize — encodings converged on one ballot")

	// Exactly one UTX exists, under the canonical key, regardless of which
	// encoding is used to derive it.
	utxCount := 0
	require.NoError(t, app.UexecutorKeeper.UniversalTx.Walk(ctx, nil, func(_ string, _ uexecutortypes.UniversalTx) (bool, error) {
		utxCount++
		return false, nil
	}))
	require.Equal(t, 1, utxCount, "one event must yield exactly one UniversalTx")

	for i, v := range variants {
		utx, found, err := app.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(v))
		require.NoError(t, err)
		require.True(t, found, "variant %d must derive the canonical UTX key", i)
		// Stored inbound carries canonical forms (EIP-55 addresses, lowercase hash).
		require.Equal(t, txLower, utx.InboundTx.TxHash)
		require.Equal(t, baseInbound.AssetAddr, utx.InboundTx.AssetAddr,
			"stored asset address must be the canonical EIP-55 form")
	}
}

// Outbound twin of the convergence test: three validators observe the same
// destination-chain tx but submit the hash in different encodings. The
// canonical outbound digest must aggregate them on one ballot and finalize.
func TestVoteOutbound_EncodingVariantsConvergeOnOneBallot(t *testing.T) {
	app, ctx, _, utxId, outbound, coreVals := setupOutboundVotingTest(t, 4)

	const destLower = "0x46cec75af4cb022d4f234e4d4b9b35e3aae66048007a06a7c1de6b9b76d27a39"
	encodings := []string{
		destLower, // canonical lowercase
		"0x46CEC75AF4CB022D4F234E4D4B9B35E3AAE66048007A06A7C1DE6B9B76D27A39", // uppercase body
		"46cec75af4cb022d4f234e4d4b9b35e3aae66048007a06a7c1de6b9b76d27a39",   // no prefix
	}

	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)

		obs := uexecutortypes.OutboundObservation{
			Success:     true,
			BlockHeight: 42,
			TxHash:      encodings[i],
			GasFeeUsed:  outbound.GasFee,
		}
		require.NoError(t, app.UexecutorKeeper.VoteOutbound(ctx, valAddr, utxId, outbound.Id, obs),
			"vote %d with encoding %q must be accepted", i, encodings[i])
	}

	// One ballot → quorum → outbound observed, with the canonical hash stored.
	utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.Equal(t, uexecutortypes.Status_OBSERVED, utx.OutboundTx[0].OutboundStatus,
		"equivalent encodings must aggregate on one ballot and finalize")
	require.NotNil(t, utx.OutboundTx[0].ObservedTx)
	require.Equal(t, destLower, utx.OutboundTx[0].ObservedTx.TxHash,
		"stored observation must carry the canonical 0x-lowercase hash")
}

// TestInboundBallotKey_StoreAndFetchAcrossEncodings demonstrates the full
// key lifecycle: derive a ballot key from one encoding of an event, store a
// ballot under it, then derive the key again from a DIFFERENT encoding of the
// same event and fetch the stored ballot back. This is what lets a second
// validator's differently-encoded vote find the first validator's ballot.
func TestInboundBallotKey_StoreAndFetchAcrossEncodings(t *testing.T) {
	chainApp, ctx, _, baseInbound, _ := setupInboundBridgeTest(t, 1)

	// Encoding A: all lowercase. Derive the key and store a ballot under it.
	a := *baseInbound
	a.TxHash = "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	a.Sender = strings.ToLower(baseInbound.Sender)
	a.AssetAddr = strings.ToLower(baseInbound.AssetAddr)

	keyA, err := uexecutortypes.GetInboundBallotKey(a)
	require.NoError(t, err)
	t.Logf("derived ballot key (encoding A) = %s", keyA)

	stored := uvalidatortypes.Ballot{
		Id:                 keyA,
		BallotType:         uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
		EligibleVoters:     []string{"cosmosvaloper1aaa"},
		Votes:              []uvalidatortypes.VoteResult{uvalidatortypes.VoteResult_VOTE_RESULT_SUCCESS},
		VotingThreshold:    1,
		Status:             uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING,
		BlockHeightCreated: ctx.BlockHeight(),
		BlockHeightExpiry:  ctx.BlockHeight() + 100,
	}
	require.NoError(t, chainApp.UvalidatorKeeper.SetBallot(ctx, stored))

	// Encoding B: 0X-uppercase tx hash, EIP-55 addresses — same logical event.
	b := *baseInbound
	b.TxHash = "0X" + strings.ToUpper(a.TxHash[2:])
	b.Sender = baseInbound.Sender
	b.AssetAddr = baseInbound.AssetAddr

	keyB, err := uexecutortypes.GetInboundBallotKey(b)
	require.NoError(t, err)
	t.Logf("derived ballot key (encoding B) = %s", keyB)

	require.Equal(t, keyA, keyB, "different encodings of the same event derive the same key")

	// Fetch the ballot stored under encoding A's key, using encoding B's key.
	fetched, err := chainApp.UvalidatorKeeper.GetBallot(ctx, keyB)
	require.NoError(t, err)
	require.Equal(t, keyA, fetched.Id, "encoding B's key fetches the ballot stored under encoding A")
	require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, fetched.Status)
}
