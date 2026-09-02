package integrationtest

import (
	"strings"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// hexBlobOfLen returns a lowercase 0x-prefixed blob exactly n characters long.
// Canonicalize leaves an even-bodied lowercase hex blob untouched, so the
// length the keeper sees is the length built here.
func hexBlobOfLen(n int) string {
	body := strings.Repeat("ab", (n-2)/2)
	if len(body)+2 < n {
		body += "c"
	}
	return "0x" + body
}

// universalPayloadOfSize builds a valid payload whose serialized size is
// exactly want bytes.
func universalPayloadOfSize(t *testing.T, want int) *uexecutortypes.UniversalPayload {
	t.Helper()

	// tag byte + 3-byte varint length for every size used here.
	const dataOverhead = 4

	for _, nonce := range []string{"1", "11"} {
		p := &uexecutortypes.UniversalPayload{
			To:    utils.GetDefaultAddresses().HandlerAddr.Hex(),
			Nonce: nonce,
		}
		dataLen := want - p.Size() - dataOverhead
		if dataLen < 2 || dataLen%2 != 0 {
			continue
		}
		p.Data = "0x" + strings.Repeat("ab", (dataLen-2)/2)
		if p.Size() == want {
			return p
		}
	}

	t.Fatalf("could not build a universal payload of exactly %d bytes", want)
	return nil
}

// TestVoteInboundPayloadSizeCap covers the vote half of the 128 KiB universal
// payload cap.
//
// The vote arrives wrapped in an authz.MsgExec — that is what the universal
// validator broadcasts (universalClient/pushsigner/pushsigner.go wrapWithAuthZ)
// and what utils.ExecVoteInbound reproduces. authz.MsgExec carries no
// ValidateBasic of its own, so baseapp does not reach the inner msg at CheckTx;
// the inner ValidateBasic runs later, inside authz's Exec msg server, and the
// keeper check runs after that. Both are exercised here.
func TestVoteInboundPayloadSizeCap(t *testing.T) {
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr

	newInbound := func(txHash, rawPayload string) *uexecutortypes.Inbound {
		return &uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      txHash,
			Sender:      testAddress,
			Amount:      "1000000",
			AssetAddr:   usdcAddress.String(),
			LogIndex:    "1",
			TxType:      uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			RawPayload:  rawPayload,
		}
	}

	utxKeyOf := func(in *uexecutortypes.Inbound) string {
		canon := *in
		canon.Canonicalize()
		return uexecutortypes.GetInboundUniversalTxKey(canon)
	}

	t.Run("raw payload at the cap is voted on", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupInboundValidationTest(t, 4)

		raw := hexBlobOfLen(uexecutortypes.MaxUniversalPayloadBytes)
		require.Len(t, raw, uexecutortypes.MaxUniversalPayloadBytes)

		inbound := newInbound("0xatcap01", raw)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		voteErr := utils.ExecVoteInbound(t, ctx, chainApp, vals[0], sdk.AccAddress(valAddr).String(), inbound)

		// State first: the vote was recorded, so the cap did not reject it.
		entry, err := chainApp.UexecutorKeeper.PendingInbounds.Get(ctx, utxKeyOf(inbound))
		require.NoError(t, err, "a vote at the cap must be recorded")
		require.Len(t, entry.Variants, 1)
		require.Len(t, entry.Variants[0].Inbound.RawPayload, uexecutortypes.MaxUniversalPayloadBytes)

		require.NoError(t, voteErr)
	})

	t.Run("raw payload one byte over the cap is rejected on the authz path", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupInboundValidationTest(t, 4)

		raw := hexBlobOfLen(uexecutortypes.MaxUniversalPayloadBytes + 1)
		require.Len(t, raw, uexecutortypes.MaxUniversalPayloadBytes+1)

		inbound := newInbound("0xovercap01", raw)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		voteErr := utils.ExecVoteInbound(t, ctx, chainApp, vals[0], sdk.AccAddress(valAddr).String(), inbound)

		// State first: nothing about this inbound reached consensus state.
		_, err = chainApp.UexecutorKeeper.PendingInbounds.Get(ctx, utxKeyOf(inbound))
		require.ErrorIs(t, err, collections.ErrNotFound, "an oversized vote must not write PendingInbounds")

		_, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKeyOf(inbound))
		require.NoError(t, err)
		require.False(t, found, "an oversized vote must not create a UniversalTx")

		require.Error(t, voteErr)
		require.Contains(t, voteErr.Error(), "raw_payload too large")
		require.Contains(t, voteErr.Error(), "131073 bytes exceeds the 131072 byte limit")
	})

	t.Run("keeper rejects an oversized vote without any msg validation", func(t *testing.T) {
		chainApp, ctx, _, coreVals, _ := setupInboundValidationTest(t, 4)

		inbound := newInbound("0xovercap02", hexBlobOfLen(uexecutortypes.MaxUniversalPayloadBytes+1))

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)

		// Straight into the keeper, so nothing but the keeper's own check can
		// reject it.
		voteErr := chainApp.UexecutorKeeper.VoteInbound(ctx, valAddr, *inbound)

		_, err = chainApp.UexecutorKeeper.PendingInbounds.Get(ctx, utxKeyOf(inbound))
		require.ErrorIs(t, err, collections.ErrNotFound, "the keeper must not write PendingInbounds for an oversized vote")

		require.Error(t, voteErr)
		require.Contains(t, voteErr.Error(), "raw_payload too large")
	})
}

// TestExecutePayloadSizeCap covers the direct half of the cap: MsgExecutePayload
// is fee exempt (app/txpolicy/gasless.go), so the payload it carries is not
// priced anywhere and only the size cap bounds it.
func TestExecutePayloadSizeCap(t *testing.T) {
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	// A real 20-byte account, since MsgExecutePayload rejects any other length.
	signerAcc := sdk.AccAddress(common.HexToAddress(testAddress).Bytes())
	signer := signerAcc.String()

	newMsg := func(payload *uexecutortypes.UniversalPayload) *uexecutortypes.MsgExecutePayload {
		return &uexecutortypes.MsgExecutePayload{
			Signer: signer,
			UniversalAccountId: &uexecutortypes.UniversalAccountId{
				ChainNamespace: "eip155",
				ChainId:        "11155111",
				Owner:          testAddress,
			},
			UniversalPayload: payload,
			VerificationData: "0x",
		}
	}

	execViaAuthz := func(chainApp *app.ChainApp, ctx sdk.Context, payload *uexecutortypes.UniversalPayload) error {
		execMsg := authz.NewMsgExec(signerAcc, []sdk.Msg{newMsg(payload)})
		_, err := chainApp.AuthzKeeper.Exec(ctx, &execMsg)
		return err
	}

	t.Run("payload over the cap is rejected on the authz path", func(t *testing.T) {
		chainApp, ctx, _, _, _ := setupInboundValidationTest(t, 1)

		oversized := universalPayloadOfSize(t, uexecutortypes.MaxUniversalPayloadBytes+1)
		require.Equal(t, uexecutortypes.MaxUniversalPayloadBytes+1, oversized.Size())

		err := execViaAuthz(chainApp, ctx, oversized)
		require.Error(t, err)
		require.Contains(t, err.Error(), "universal payload too large")
		require.Contains(t, err.Error(), "131073 bytes exceeds the 131072 byte limit")
	})

	t.Run("keeper rejects an oversized payload without any msg validation", func(t *testing.T) {
		chainApp, ctx, _, _, _ := setupInboundValidationTest(t, 1)

		oversized := universalPayloadOfSize(t, uexecutortypes.MaxUniversalPayloadBytes+1)

		// Straight into the keeper, so nothing but the keeper's own check can
		// reject it — and before any EVM work.
		err := chainApp.UexecutorKeeper.ExecutePayload(
			ctx,
			common.HexToAddress(testAddress),
			newMsg(oversized).UniversalAccountId,
			oversized,
			"0x",
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "universal payload too large")
	})

	t.Run("payload at the cap passes the size gate", func(t *testing.T) {
		chainApp, ctx, _, _, _ := setupInboundValidationTest(t, 1)

		atCap := universalPayloadOfSize(t, uexecutortypes.MaxUniversalPayloadBytes)
		require.Equal(t, uexecutortypes.MaxUniversalPayloadBytes, atCap.Size())
		require.NoError(t, newMsg(atCap).ValidateBasic())

		// Execution may still fail further down (this signer has no deployed
		// UEA); what matters is that it is never the size gate.
		if err := execViaAuthz(chainApp, ctx, atCap); err != nil {
			require.NotContains(t, err.Error(), "too large")
		}
	})
}
