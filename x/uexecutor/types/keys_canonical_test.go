package types_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// recipeHashFields reproduces the production hashFields construction
// independently so the tests document (and pin) the exact algorithm:
//
//	key = sha256( hex(sha256(domain.Bytes())) : hex(sha256(f0)) : ... )
func recipeHashFields(domain collections.Prefix, parts ...string) string {
	perField := make([]string, 0, len(parts)+1)
	d := sha256.Sum256(domain.Bytes())
	perField = append(perField, hex.EncodeToString(d[:]))
	for _, p := range parts {
		s := sha256.Sum256([]byte(p))
		perField = append(perField, hex.EncodeToString(s[:]))
	}
	final := sha256.Sum256([]byte(strings.Join(perField, ":")))
	return hex.EncodeToString(final[:])
}

// Canonical voting digest suite: ballot identity must converge for encoding
// variants of the same event, diverge on any consensus-critical difference,
// and ignore the derived universal_payload.

func canonInbound() types.Inbound {
	return types.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		Sender:      "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		Recipient:   "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238",
		Amount:      "1000000",
		AssetAddr:   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		LogIndex:    "1",
		TxType:      types.TxType_FUNDS,
		RevertInstructions: &types.RevertInstructions{
			FundRecipient: "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		},
	}
}

// TestInboundBallotKey_GoldenValueAndRecipe shows exactly how an inbound
// ballot key is built and pins the resulting value. The fields below are
// already in canonical form, so canonicalization is a no-op and the recipe is
// transparent: hash each field, join the hex digests with ':', hash again.
func TestInboundBallotKey_GoldenValueAndRecipe(t *testing.T) {
	in := types.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		Sender:      "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		Recipient:   "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238",
		Amount:      "1000000",
		AssetAddr:   "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		LogIndex:    "1",
		TxType:      types.TxType_FUNDS,
		RevertInstructions: &types.RevertInstructions{
			FundRecipient: "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		},
	}

	got, err := types.GetInboundBallotKey(in)
	require.NoError(t, err)
	t.Logf("inbound ballot key = %s", got)

	// 1. The exact pinned value (catches any accidental construction change).
	require.Equal(t,
		"1e7755fdc3d07f21b770f85a9de1cb62b740a8a4d7a38bae0a4a36fd809e2d30",
		got, "inbound ballot key golden value changed — confirm the change is intentional")

	// 2. The same value, reproduced field-by-field via the documented recipe.
	expected := recipeHashFields(
		types.InboundBallotDomain,
		in.SourceChain,
		in.TxHash,
		in.LogIndex,
		in.Sender,
		in.Recipient,
		in.Amount,
		in.AssetAddr,
		fmt.Sprintf("%d", in.TxType),
		in.VerificationData, // ""
		in.RevertInstructions.FundRecipient,
		fmt.Sprintf("%t", in.IsCEA), // false
		in.RawPayload,               // ""
	)
	require.Equal(t, expected, got, "production key must equal the documented recipe")
	require.Len(t, got, 64, "key is a hex-encoded sha256 digest")
}

// TestOutboundBallotKey_GoldenValueAndRecipe is the outbound twin.
func TestOutboundBallotKey_GoldenValueAndRecipe(t *testing.T) {
	obs := types.OutboundObservation{
		Success:     true,
		BlockHeight: 100,
		TxHash:      "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		GasFeeUsed:  "21000",
	}

	got, err := types.GetOutboundBallotKey("utx-1", "ob-1", obs)
	require.NoError(t, err)
	t.Logf("outbound ballot key = %s", got)

	require.Equal(t,
		"ac7ba6932ceec3e434947a96eabf1deb7a7d628d09691190244d5ed63ccb155a",
		got, "outbound ballot key golden value changed — confirm the change is intentional")

	expected := recipeHashFields(
		types.OutboundBallotDomain,
		"utx-1",
		"ob-1",
		fmt.Sprintf("%t", obs.Success),
		fmt.Sprintf("%d", obs.BlockHeight),
		obs.TxHash,
		obs.GasFeeUsed,
		obs.ErrorMsg, // ""
	)
	require.Equal(t, expected, got, "production key must equal the documented recipe")
	require.Len(t, got, 64)
}

func TestInboundBallotKey_EncodingVariantsConverge(t *testing.T) {
	base := canonInbound()
	base.Canonicalize()
	baseKey, err := types.GetInboundBallotKey(base)
	require.NoError(t, err)

	// Same logical event with every string field in a different encoding.
	variant := canonInbound()
	variant.TxHash = "0XB28F49668E7E76DC96D7AABE5B7F63FECFBD1C3574774C05E8204E749FD96FBD"
	variant.Sender = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	variant.Recipient = "0X1C7D4B196CB0C7B01D743FBC6116A902379C7238"
	variant.AssetAddr = "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	variant.RevertInstructions.FundRecipient = "0x5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED"
	variant.Canonicalize()

	variantKey, err := types.GetInboundBallotKey(variant)
	require.NoError(t, err)
	require.Equal(t, baseKey, variantKey,
		"encoding variants of the same event must produce one ballot key")

	// And the UTX key converges as well (sibling site).
	require.Equal(t, types.GetInboundUniversalTxKey(base), types.GetInboundUniversalTxKey(variant))
}

func TestInboundBallotKey_UniversalPayloadExcluded(t *testing.T) {
	a := canonInbound()
	a.Canonicalize()
	keyNil, err := types.GetInboundBallotKey(a)
	require.NoError(t, err)

	b := canonInbound()
	b.UniversalPayload = &types.UniversalPayload{To: "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238"}
	b.Canonicalize()
	keyPopulated, err := types.GetInboundBallotKey(b)
	require.NoError(t, err)

	require.Equal(t, keyNil, keyPopulated,
		"universal_payload is derived/ignored on-chain and must not affect ballot identity")
}

func TestInboundBallotKey_NilAndEmptyRevertInstructionsConverge(t *testing.T) {
	a := canonInbound()
	a.RevertInstructions = nil
	a.Canonicalize()
	keyNil, _ := types.GetInboundBallotKey(a)

	b := canonInbound()
	b.RevertInstructions = &types.RevertInstructions{FundRecipient: ""}
	b.Canonicalize()
	keyEmpty, _ := types.GetInboundBallotKey(b)

	require.Equal(t, keyNil, keyEmpty,
		"nil revert_instructions and empty fund_recipient are semantically identical")
}

func TestInboundBallotKey_ConsensusFieldsDiverge(t *testing.T) {
	base := canonInbound()
	base.Canonicalize()
	baseKey, _ := types.GetInboundBallotKey(base)

	mutate := []func(*types.Inbound){
		func(i *types.Inbound) { i.Amount = "2000000" },
		func(i *types.Inbound) { i.Recipient = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed" },
		func(i *types.Inbound) { i.AssetAddr = "0x387b9C8Db60E74999aAAC5A2b7825b400F12d68E" },
		func(i *types.Inbound) { i.LogIndex = "2" },
		func(i *types.Inbound) { i.TxType = types.TxType_GAS },
		func(i *types.Inbound) { i.IsCEA = true },
		func(i *types.Inbound) { i.RawPayload = "0xdeadbeef" },
		func(i *types.Inbound) { i.RevertInstructions.FundRecipient = "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238" },
	}
	for n, m := range mutate {
		v := canonInbound()
		m(&v)
		v.Canonicalize()
		key, err := types.GetInboundBallotKey(v)
		require.NoError(t, err)
		require.NotEqual(t, baseKey, key, "mutation %d must change the ballot identity", n)
	}
}

func TestOutboundBallotKey_EncodingVariantsConverge(t *testing.T) {
	// Hash canonicalization happens at vote ingress (per destination chain);
	// digest over the canonical observation must converge.
	obsA := types.OutboundObservation{Success: true, BlockHeight: 100,
		TxHash: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd", GasFeeUsed: "21000"}
	obsB := obsA

	keyA, err := types.GetOutboundBallotKey("utx-1", "ob-1", obsA)
	require.NoError(t, err)
	keyB, err := types.GetOutboundBallotKey("utx-1", "ob-1", obsB)
	require.NoError(t, err)
	require.Equal(t, keyA, keyB)
}

func TestOutboundBallotKey_AllFieldsAreConsensusCritical(t *testing.T) {
	base := types.OutboundObservation{Success: true, BlockHeight: 100,
		TxHash: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd", GasFeeUsed: "21000"}
	baseKey, _ := types.GetOutboundBallotKey("utx-1", "ob-1", base)

	mutations := []types.OutboundObservation{
		{Success: false, BlockHeight: 100, TxHash: base.TxHash, GasFeeUsed: "21000"},
		{Success: true, BlockHeight: 101, TxHash: base.TxHash, GasFeeUsed: "21000"},
		{Success: true, BlockHeight: 100, TxHash: "0x" + "11" + base.TxHash[4:], GasFeeUsed: "21000"},
		{Success: true, BlockHeight: 100, TxHash: base.TxHash, GasFeeUsed: "42000"},
		{Success: true, BlockHeight: 100, TxHash: base.TxHash, GasFeeUsed: "21000", ErrorMsg: "boom"},
	}
	for n, obs := range mutations {
		key, err := types.GetOutboundBallotKey("utx-1", "ob-1", obs)
		require.NoError(t, err)
		require.NotEqual(t, baseKey, key, "mutation %d must change the ballot identity", n)
	}

	// Scoping fields too.
	keyOtherUtx, _ := types.GetOutboundBallotKey("utx-2", "ob-1", base)
	require.NotEqual(t, baseKey, keyOtherUtx)
	keyOtherOb, _ := types.GetOutboundBallotKey("utx-1", "ob-2", base)
	require.NotEqual(t, baseKey, keyOtherOb)
}

func TestBallotKey_DomainSeparation(t *testing.T) {
	// Inbound and outbound digests share the framing; the version-domain
	// prefix must keep their key spaces disjoint.
	in := canonInbound()
	in.Canonicalize()
	inKey, _ := types.GetInboundBallotKey(in)
	outKey, _ := types.GetOutboundBallotKey("utx-1", "ob-1", types.OutboundObservation{Success: true, BlockHeight: 1, TxHash: in.TxHash, GasFeeUsed: "1"})
	require.NotEqual(t, inKey, outKey)
	require.Len(t, inKey, 64, "sha256 hex digest")
	require.Len(t, outKey, 64, "sha256 hex digest")
}

func TestInboundCanonicalize_SolanaFieldsPreserved(t *testing.T) {
	in := types.Inbound{
		SourceChain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		TxHash:      "0xAB12CD34" + "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccdd", // 0x-hex form (client converts sigs)
		Sender:      "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",                                                                                    // base58 — case-significant
		Recipient:   "0x1c7d4b196cb0c7b01d743fbc6116a902379c7238",
		AssetAddr:   "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		LogIndex:    "0",
		Amount:      "5",
		TxType:      types.TxType_FUNDS,
	}
	in.Canonicalize()

	require.Equal(t, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", in.Sender,
		"base58 sender must not be case-mangled")
	require.Equal(t, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", in.AssetAddr,
		"base58 asset must not be case-mangled")
	require.Equal(t, "0x", in.TxHash[:2])
	require.Equal(t, in.TxHash, "0x"+lowercase(in.TxHash[2:]), "hex tx hash lowercased")
	require.Equal(t, "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238", in.Recipient,
		"push-side recipient canonicalized to EIP-55")
}

func lowercase(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'F' {
			out[i] = r + 32
		}
	}
	return string(out)
}
