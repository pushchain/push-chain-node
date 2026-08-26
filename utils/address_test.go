package utils_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/utils"
)

// uexecutorModuleEVMAddr is sha256("uexecutor")[:20] rendered as an EVM address.
const uexecutorModuleEVMAddr = "0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7"

// bech32OfLength returns a bech32 account address that decodes to exactly n bytes.
func bech32OfLength(n int) string {
	bz := make([]byte, n)
	for i := range bz {
		bz[i] = byte(i + 1)
	}
	return sdk.AccAddress(bz).String()
}

// TestGetAddressPair_RejectsNon20ByteAddresses is the regression test for
// F-2026-18200 remediation 2: anything that does not decode to exactly 20 bytes
// must be rejected rather than silently truncated.
func TestGetAddressPair_RejectsNon20ByteAddresses(t *testing.T) {
	for _, length := range []int{19, 21, 22, 32} {
		addr := bech32OfLength(length)
		_, _, err := utils.GetAddressPair(addr)
		require.Error(t, err, "%d-byte address must be rejected", length)
		require.Contains(t, err.Error(), "invalid address length")
	}
}

func TestGetAddressPair_Accepts20ByteAddresses(t *testing.T) {
	bz := make([]byte, common.AddressLength)
	for i := range bz {
		bz[i] = byte(i + 1)
	}

	cosmosAddr, evmAddr, err := utils.GetAddressPair(sdk.AccAddress(bz).String())
	require.NoError(t, err)
	require.Equal(t, sdk.AccAddress(bz), cosmosAddr)
	require.Equal(t, common.BytesToAddress(bz), evmAddr)

	// The 0x form must round-trip as well.
	cosmosAddr, evmAddr, err = utils.GetAddressPair(common.BytesToAddress(bz).Hex())
	require.NoError(t, err)
	require.Equal(t, sdk.AccAddress(bz), cosmosAddr)
	require.Equal(t, common.BytesToAddress(bz), evmAddr)
}

// TestGetAddressPair_ModuleAliasRejected documents the exact attack: an over-long
// address whose rightmost 20 bytes are the uexecutor module account truncates
// onto the module's EVM address, which the UEA trusts unconditionally.
func TestGetAddressPair_ModuleAliasRejected(t *testing.T) {
	moduleAddr := authtypes.NewModuleAddress("uexecutor")
	require.Len(t, moduleAddr, common.AddressLength)
	require.Equal(t, uexecutorModuleEVMAddr, common.BytesToAddress(moduleAddr).Hex())

	for _, prefixLen := range []int{1, 2, 12} {
		aliased := sdk.AccAddress(append(make([]byte, prefixLen), moduleAddr...))
		// Without the length check this collapses onto the module address.
		require.Equal(t, uexecutorModuleEVMAddr, common.BytesToAddress(aliased).Hex())

		_, evmAddr, err := utils.GetAddressPair(aliased.String())
		require.Error(t, err, "aliased %d-byte address must be rejected", len(aliased))
		require.Equal(t, common.Address{}, evmAddr)
	}
}

// TestMustConvertCosmosToHex checks the second truncation site: it must neither
// panic on short input nor keep the leftmost 20 bytes of a long one.
func TestMustConvertCosmosToHex(t *testing.T) {
	bz := make([]byte, common.AddressLength)
	for i := range bz {
		bz[i] = byte(i + 1)
	}
	require.Equal(t, common.BytesToAddress(bz).Hex(), utils.MustConvertCosmosToHex(sdk.AccAddress(bz).String()))

	for _, length := range []int{19, 21, 22, 32} {
		require.NotPanics(t, func() {
			require.Empty(t, utils.MustConvertCosmosToHex(bech32OfLength(length)),
				"%d-byte address must not be converted", length)
		})
	}

	require.Empty(t, utils.MustConvertCosmosToHex("not-a-bech32-address"))
}
