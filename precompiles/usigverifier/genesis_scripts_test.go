package usigverifier_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	usigverifierprecompile "github.com/pushchain/push-chain-node/precompiles/usigverifier"
)

// legacyUSigVerifierAddress is where the Ed25519 signature verifier precompile
// used to live. Nothing is registered at it any more, so a genesis that still
// declares it active routes calls to an unimplemented address, which panics
// (recovered by baseapp, so the tx just fails).
const legacyUSigVerifierAddress = "0x00000000000000000000000000000000000000ca"

// legacyUtxHashVerifierAddress is the other stale entry: the utxhashverifier
// precompile has no implementation anywhere in the tree, and the
// remove-utxverifier upgrade strips it from live chains. Leaving it in genesis
// would re-introduce on every fresh chain exactly the address that upgrade
// exists to remove.
const legacyUtxHashVerifierAddress = "0x00000000000000000000000000000000000000cb"

// TestGenesisScriptsActivateCurrentVerifier guards the genesis half of
// F-2026-18829: a fresh chain must activate the address the verifier is actually
// registered at, and must not declare the legacy one.
func TestGenesisScriptsActivateCurrentVerifier(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

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

			require.NotContains(t, strings.ToLower(line), strings.ToLower(legacyUSigVerifierAddress),
				"genesis must not declare the legacy verifier address, nothing is registered at it")
			require.NotContains(t, strings.ToLower(line), strings.ToLower(legacyUtxHashVerifierAddress),
				"genesis must not declare the utxhashverifier address, nothing is registered at it")
			require.Contains(t, strings.ToLower(line),
				strings.ToLower(usigverifierprecompile.USigVerifierPrecompileAddress),
				"genesis must activate the verifier address the node registers")
		})
	}
}
