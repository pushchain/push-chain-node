package keeper_test

import (
	"strings"
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestUeModuleAddressDerivation is a unit check of the identity that
// IsUeModuleAddress routes on. GetUeModuleAddress derives the module's EVM address
// as the "uexecutor" module account's bytes, which must equal the
// UNIVERSAL_EXECUTOR_MODULE constant the UEA / VaultPC20 / UniversalCore contracts
// hardcode — otherwise the module-sender bypass and custody gating break. It also
// pins why IsUeModuleAddress compares raw common.Address values (not hex strings):
// identity is the 20 bytes, so re-parsing a lower/upper-case hex form is byte-equal
// and no lowercasing is needed.
//
// (IsUeModuleAddress and the module-sender nonce branch are exercised end-to-end
// against a real keeper in test/integration/uexecutor/module_nonce_test.go; the
// keeper unit harness wires an empty AccountKeeper, so GetUeModuleAddress can't run
// here — hence the direct derivation.)
func TestUeModuleAddressDerivation(t *testing.T) {
	// Same derivation GetUeModuleAddress performs: module account bytes → EVM address.
	moduleEVM := common.BytesToAddress(authtypes.NewModuleAddress(types.ModuleName).Bytes())

	require.Equal(t,
		common.HexToAddress("0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7"), moduleEVM,
		"uexecutor module EVM address must equal UNIVERSAL_EXECUTOR_MODULE")

	// Byte identity is case-insensitive — a re-parsed lower/upper-case form matches.
	require.True(t, moduleEVM == common.HexToAddress(strings.ToLower(moduleEVM.Hex())))
	require.True(t, moduleEVM == common.HexToAddress("0x"+strings.ToUpper(strings.TrimPrefix(moduleEVM.Hex(), "0x"))))
	// A different EVM address does not.
	require.False(t, moduleEVM == common.HexToAddress("0x00000000000000000000000000000000dEadBeef"))
	require.False(t, moduleEVM == common.HexToAddress("0x1234567890AbCdeF1234567890abCDeF12345678"))
	// Nor does a Solana (SVM) address folded into 20 bytes.
	solBytes, err := base58.Decode("So11111111111111111111111111111111111111112")
	require.NoError(t, err)
	require.False(t, moduleEVM == common.BytesToAddress(solBytes))
}
