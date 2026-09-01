package types_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

// testdata/read_requested_abi.json is the ReadRequested fragment lifted verbatim
// from the compiled UniversalCallback artifact.
//
//	repo: push-chain-core-contracts
//
// Refresh it with `forge build --contracts src/UniversalCallback.sol` and copy the
// ReadRequested entry out of out/UniversalCallback.sol/UniversalCallback.json.
const artifactABIPath = "testdata/read_requested_abi.json"

// Our ABI fragment is transcribed by hand, and every part of it feeds topic0 —
// types, indexed flags, and field order alike. IngestReadRequests filters on topic0,
// so any drift makes the module silently ignore every real ReadRequested log and
// record nothing at all. No round-trip test catches that: it encodes and decodes
// with the same fragment, so a wrong fragment agrees with itself.
//
// This is the only test that compares us against the contract. It earned its place —
// the fragment had callbackGasLimit last, while the contract emits it fifth.
func TestReadRequestedABIMatchesCompiledArtifact(t *testing.T) {
	raw, err := os.ReadFile(artifactABIPath)
	require.NoError(t, err, "vendored artifact fragment must be present")

	fromArtifact, err := abi.JSON(strings.NewReader(string(raw)))
	require.NoError(t, err)
	want, ok := fromArtifact.Events["ReadRequested"]
	require.True(t, ok, "artifact must declare ReadRequested")

	require.Equal(t, want.ID, types.ReadRequestedEventSig,
		"topic0 disagrees with the compiled contract: %s\n"+
			"ours wants: %s\n"+
			"IngestReadRequests filters on this, so every real log would be dropped",
		want.Sig, types.ReadRequestedEventSigName())

	// topic0 equality already implies the signature matches, but compare the fields
	// individually so a failure says which one moved rather than just "hashes differ".
	got := types.ReadRequestedEventInputs()
	require.Len(t, got, len(want.Inputs), "input count")
	for i := range want.Inputs {
		require.Equal(t, want.Inputs[i].Name, got[i].Name, "input %d name", i)
		require.Equal(t, want.Inputs[i].Type.String(), got[i].Type.String(), "input %d type", i)
		require.Equal(t, want.Inputs[i].Indexed, got[i].Indexed, "input %d indexed", i)
	}
}
