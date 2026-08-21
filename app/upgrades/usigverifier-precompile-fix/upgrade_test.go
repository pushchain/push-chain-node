package usigverifierprecompilefix

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	usigverifierprecompile "github.com/pushchain/push-chain-node/precompiles/usigverifier"
)

const currentAddr = usigverifierprecompile.USigVerifierPrecompileAddress

// baseline mirrors the non-verifier entries of a real chain's ActiveStaticPrecompiles.
var baseline = []string{
	"0x00000000000000000000000000000000000000CB",
	"0x0000000000000000000000000000000000000100",
	"0x0000000000000000000000000000000000000400",
	"0x0000000000000000000000000000000000000800",
	"0x0000000000000000000000000000000000000801",
	"0x0000000000000000000000000000000000000802",
	"0x0000000000000000000000000000000000000803",
	"0x0000000000000000000000000000000000000804",
	"0x0000000000000000000000000000000000000805",
}

func withLegacy() []string {
	out := append([]string{LegacyUSigVerifierAddress}, baseline...)
	slices.Sort(out)
	return out
}

func TestSyncActiveStaticPrecompiles_ReplacesLegacyAddress(t *testing.T) {
	got, removed, added := syncActiveStaticPrecompiles(withLegacy())

	require.True(t, removed, "legacy address should have been removed")
	require.True(t, added, "current address should have been added")

	require.NotContains(t, got, LegacyUSigVerifierAddress)
	require.Contains(t, got, currentAddr)

	// Everything else survives untouched, and the list stays sorted so that
	// x/vm's ValidatePrecompiles accepts it.
	for _, addr := range baseline {
		require.Contains(t, got, addr)
	}
	require.Len(t, got, len(baseline)+1)
	require.True(t, slices.IsSorted(got), "precompile list must stay sorted: %v", got)
}

func TestSyncActiveStaticPrecompiles_Idempotent(t *testing.T) {
	first, _, _ := syncActiveStaticPrecompiles(withLegacy())

	second, removed, added := syncActiveStaticPrecompiles(first)
	require.False(t, removed, "second run should find nothing to remove")
	require.False(t, added, "second run should find nothing to add")
	require.Equal(t, first, second)
}

func TestSyncActiveStaticPrecompiles_AddsCurrentWhenBothMissing(t *testing.T) {
	got, removed, added := syncActiveStaticPrecompiles(slices.Clone(baseline))

	require.False(t, removed)
	require.True(t, added)
	require.Contains(t, got, currentAddr)
	require.Len(t, got, len(baseline)+1)
}

func TestSyncActiveStaticPrecompiles_RemovesLegacyWhenCurrentPresent(t *testing.T) {
	in := append(withLegacy(), currentAddr)
	slices.Sort(in)

	got, removed, added := syncActiveStaticPrecompiles(in)

	require.True(t, removed)
	require.False(t, added)
	require.NotContains(t, got, LegacyUSigVerifierAddress)
	require.Contains(t, got, currentAddr)
	require.Len(t, got, len(baseline)+1)
}

func TestSyncActiveStaticPrecompiles_MatchesLegacyCaseInsensitively(t *testing.T) {
	in := append([]string{strings.ToUpper(LegacyUSigVerifierAddress[2:])}, baseline...)
	in[0] = "0x" + in[0]

	got, removed, _ := syncActiveStaticPrecompiles(in)

	require.True(t, removed)
	for _, addr := range got {
		require.False(t, strings.EqualFold(addr, LegacyUSigVerifierAddress))
	}
}

// TestGenesisScriptsActivateCurrentVerifier guards the genesis half of the same
// fix: a fresh chain must activate the address the verifier is registered at and
// must not declare the legacy one, which nothing implements.
func TestGenesisScriptsActivateCurrentVerifier(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")

	scripts := []string{
		"scripts/test_node.sh",
		"local-native/scripts/setup-genesis-auto.sh",
		"local-multi-validator/scripts/setup-genesis-auto.sh",
		"testnet/core/setup/setup_genesis_validator.sh",
	}

	for _, script := range scripts {
		t.Run(script, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repoRoot, script))
			require.NoError(t, err)

			var line string
			for _, l := range strings.Split(string(raw), "\n") {
				if strings.Contains(l, "active_static_precompiles") {
					line = l
					break
				}
			}
			require.NotEmpty(t, line, "no active_static_precompiles assignment found")

			require.NotContains(t, strings.ToLower(line), strings.ToLower(LegacyUSigVerifierAddress),
				"genesis must not declare the legacy verifier address, nothing is registered at it")
			require.Contains(t, strings.ToLower(line), strings.ToLower(currentAddr),
				"genesis must activate the verifier address the node registers")
		})
	}
}
