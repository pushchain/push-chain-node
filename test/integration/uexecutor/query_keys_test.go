package integrationtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// The InboundKeys / OutboundBallotKey queries let off-chain validators read the
// canonical UTX id + ballot ids from the chain instead of re-implementing the
// canonicalization + digest rules. These tests exercise each query.

func TestQueryInboundKeys_MatchesDerivation(t *testing.T) {
	app, ctx, _, inbound, _ := setupInboundBridgeTest(t, 1)
	q := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

	resp, err := q.InboundKeys(ctx, &uexecutortypes.QueryInboundKeysRequest{Inbound: inbound})
	require.NoError(t, err)

	// The returned keys must equal direct derivation from the canonical inbound.
	canon := *inbound
	canon.Canonicalize()
	require.Equal(t, uexecutortypes.GetInboundUniversalTxKey(canon), resp.UtxId)
	wantBallot, err := uexecutortypes.GetInboundBallotKey(canon)
	require.NoError(t, err)
	require.Equal(t, wantBallot, resp.BallotId)

	// Response echoes the canonical form the chain derived from.
	require.NotNil(t, resp.CanonicalInbound)
	require.Equal(t, canon.AssetAddr, resp.CanonicalInbound.AssetAddr)
	require.Len(t, resp.UtxId, 64)
	require.Len(t, resp.BallotId, 64)
}

func TestQueryInboundKeys_EncodingVariantsAgree(t *testing.T) {
	app, ctx, _, inbound, _ := setupInboundBridgeTest(t, 1)
	q := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

	lower := *inbound
	lower.AssetAddr = strings.ToLower(inbound.AssetAddr)
	lower.Sender = strings.ToLower(inbound.Sender)

	upper := *inbound
	upper.AssetAddr = "0X" + strings.ToUpper(strings.TrimPrefix(inbound.AssetAddr, "0x"))

	rl, err := q.InboundKeys(ctx, &uexecutortypes.QueryInboundKeysRequest{Inbound: &lower})
	require.NoError(t, err)
	ru, err := q.InboundKeys(ctx, &uexecutortypes.QueryInboundKeysRequest{Inbound: &upper})
	require.NoError(t, err)

	require.Equal(t, rl.UtxId, ru.UtxId, "encoding variants must yield one UTX id")
	require.Equal(t, rl.BallotId, ru.BallotId, "encoding variants must yield one ballot id")
}

func TestQueryInboundKeys_NilRejected(t *testing.T) {
	app, ctx, _, _, _ := setupInboundBridgeTest(t, 1)
	q := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

	_, err := q.InboundKeys(ctx, &uexecutortypes.QueryInboundKeysRequest{Inbound: nil})
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbound is required")
}

func TestQueryOutboundBallotKey_MatchesDerivation(t *testing.T) {
	app, ctx, _, utxId, outbound, _ := setupOutboundVotingTest(t, 4)
	q := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

	obs := &uexecutortypes.OutboundObservation{
		Success:     true,
		BlockHeight: 42,
		TxHash:      "0XB28F49668E7E76DC96D7AABE5B7F63FECFBD1C3574774C05E8204E749FD96FBD", // mixed/upper
		GasFeeUsed:  outbound.GasFee,
	}

	resp, err := q.OutboundBallotKey(ctx, &uexecutortypes.QueryOutboundBallotKeyRequest{
		UtxId:      utxId,
		OutboundId: outbound.Id,
		ObservedTx: obs,
	})
	require.NoError(t, err)

	// Equals derivation from the canonicalized observation (lowercased hash).
	canonObs := *obs
	canonObs.TxHash = "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	want, err := uexecutortypes.GetOutboundBallotKey(utxId, outbound.Id, canonObs)
	require.NoError(t, err)
	require.Equal(t, want, resp.BallotId)

	require.NotNil(t, resp.CanonicalObservedTx)
	require.Equal(t, canonObs.TxHash, resp.CanonicalObservedTx.TxHash, "query returns the canonical hash")
	require.Len(t, resp.BallotId, 64)
}

func TestQueryOutboundBallotKey_EncodingVariantsAgree(t *testing.T) {
	app, ctx, _, utxId, outbound, _ := setupOutboundVotingTest(t, 4)
	q := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

	mk := func(hash string) *uexecutortypes.QueryOutboundBallotKeyRequest {
		return &uexecutortypes.QueryOutboundBallotKeyRequest{
			UtxId: utxId, OutboundId: outbound.Id,
			ObservedTx: &uexecutortypes.OutboundObservation{Success: true, BlockHeight: 7, TxHash: hash, GasFeeUsed: "100"},
		}
	}
	a, err := q.OutboundBallotKey(ctx, mk("0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"))
	require.NoError(t, err)
	b, err := q.OutboundBallotKey(ctx, mk("b28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd")) // no 0x
	require.NoError(t, err)
	require.Equal(t, a.BallotId, b.BallotId, "encoding variants must yield one outbound ballot id")
}

func TestQueryOutboundBallotKey_Errors(t *testing.T) {
	app, ctx, _, utxId, outbound, _ := setupOutboundVotingTest(t, 4)
	q := uexecutorkeeper.NewQuerier(app.UexecutorKeeper)

	obs := &uexecutortypes.OutboundObservation{Success: true, BlockHeight: 1, TxHash: "0xaa", GasFeeUsed: "1"}

	// nil observation
	_, err := q.OutboundBallotKey(ctx, &uexecutortypes.QueryOutboundBallotKeyRequest{UtxId: utxId, OutboundId: outbound.Id})
	require.Error(t, err)
	require.Contains(t, err.Error(), "observed_tx is required")

	// unknown utx
	_, err = q.OutboundBallotKey(ctx, &uexecutortypes.QueryOutboundBallotKeyRequest{UtxId: "does-not-exist", OutboundId: outbound.Id, ObservedTx: obs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")

	// known utx, unknown outbound
	_, err = q.OutboundBallotKey(ctx, &uexecutortypes.QueryOutboundBallotKeyRequest{UtxId: utxId, OutboundId: "no-such-outbound", ObservedTx: obs})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
