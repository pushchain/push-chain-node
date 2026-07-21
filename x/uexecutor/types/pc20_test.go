package types_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestIsPC20Payload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{"selector only", "0x50433230", true},
		{"selector + metadata", "0x50433230000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7", true},
		{"selector uppercase hex", "0x50433230ABCD", true},
		{"no 0x prefix", "50433230deadbeef", true},
		{"with surrounding space", "  0x50433230  ", true},
		{"empty", "", false},
		{"just 0x", "0x", false},
		{"too short", "0x504332", false},
		{"prc20 funds-only (empty-ish)", "0x", false},
		{"prc20 abi-tuple offset", "0x0000002000000000000000000000000000000000000000000000000000000000", false},
		{"different selector", "0x50524332", false}, // "PRC2"-ish, not PC20
		{"garbage", "0xdeadbeef", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, types.IsPC20Payload(tc.payload))
		})
	}
}

// The VaultPC20 ABI must parse and pack revertExport with the exact argument
// types the on-Push vault expects (bytes32, address, uint256, address).
func TestVaultPC20ABI_RevertExportPacks(t *testing.T) {
	a, err := types.ParseVaultPC20ABI()
	require.NoError(t, err)

	_, err = a.Pack(
		"revertExport",
		common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ab"),
		common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
		big.NewInt(1000),
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
	)
	require.NoError(t, err)
}

// The setWrapperDeployed method added to the UniversalCore ABI must pack with
// (address, string, address).
func TestUniversalCoreABI_SetWrapperDeployedPacks(t *testing.T) {
	a, err := types.ParseUniversalCoreABI()
	require.NoError(t, err)

	_, err = a.Pack(
		"setWrapperDeployed",
		common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
		"eip155:11155111",
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
	)
	require.NoError(t, err)
}

// StripSelector drops exactly the 4-byte magic selector and re-prefixes 0x.
func TestStripSelector(t *testing.T) {
	require.Equal(t, "0xdeadbeef", types.StripSelector("0x"+types.PC20Selector+"deadbeef"))
	require.Equal(t, "0x", types.StripSelector("0x"+types.PC20Selector))       // selector only -> empty user payload
	require.Equal(t, "0x5043", types.StripSelector("0x5043"))                  // too short -> unchanged
	require.Equal(t, "0xabcd", types.StripSelector(types.PC20Selector+"abcd")) // no 0x prefix still stripped
}

// RouteInboundPayload detects a PC20 return and strips its selector; anything
// without a recognised selector passes through unchanged (today's un-prefixed
// PRC20 behaviour, since PRC20Selector is not yet set).
func TestRouteInboundPayload(t *testing.T) {
	isPC20, up := types.RouteInboundPayload("0x" + types.PC20Selector + "abcdef")
	require.True(t, isPC20)
	require.Equal(t, "0xabcdef", up)

	// PRC20-prefixed payload: not PC20, but the selector is still stripped so only
	// the user payload is decoded/executed downstream.
	isPC20, up = types.RouteInboundPayload("0x" + types.PRC20Selector + "abcdef")
	require.False(t, isPC20)
	require.Equal(t, "0xabcdef", up)

	// No recognised selector: passthrough unchanged (legacy un-prefixed PRC20).
	isPC20, up = types.RouteInboundPayload("0xdeadbeef")
	require.False(t, isPC20)
	require.Equal(t, "0xdeadbeef", up)

	isPC20, up = types.RouteInboundPayload("0x")
	require.False(t, isPC20)
	require.Equal(t, "0x", up)
}

// The unlock method added to the VaultPC20 ABI must pack with
// (bytes32, address, uint256, address).
func TestVaultPC20ABI_UnlockPacks(t *testing.T) {
	a, err := types.ParseVaultPC20ABI()
	require.NoError(t, err)

	_, err = a.Pack(
		"unlock",
		common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ab"),
		common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
		big.NewInt(1000),
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
	)
	require.NoError(t, err)
}

// getPC20Source must pack its inputs (address wrapper, string destChain) and
// round-trip its outputs (address sourceAsset, bool known).
func TestUniversalCoreABI_GetPC20SourcePacks(t *testing.T) {
	a, err := types.ParseUniversalCoreABI()
	require.NoError(t, err)

	_, err = a.Pack(
		"getPC20Source",
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		"eip155:11155111",
	)
	require.NoError(t, err)

	out := a.Methods["getPC20Source"].Outputs
	require.Len(t, out, 2)
	source := common.HexToAddress("0x3333333333333333333333333333333333333333")
	packed, err := out.Pack(source, true)
	require.NoError(t, err)
	vals, err := out.Unpack(packed)
	require.NoError(t, err)
	require.Equal(t, source, vals[0].(common.Address))
	require.Equal(t, true, vals[1].(bool))
}

// NormalizeForTxType must flag a PC20 return, strip its selector, and decode only
// the user payload — while an un-prefixed PRC20 payload is untouched.
func TestNormalizeForTxType_PC20(t *testing.T) {
	to := common.HexToAddress("0x000000000000000000000000000000000000beef")
	encoded, err := abiEncodeUniversalPayload(
		to, big.NewInt(1000), []byte{0xde, 0xad, 0xbe, 0xef},
		big.NewInt(21000), big.NewInt(1000000000), big.NewInt(200000000),
		big.NewInt(1), big.NewInt(9999999999), 1,
	)
	require.NoError(t, err)

	t.Run("PC20 selector detected, stripped, user payload decodes", func(t *testing.T) {
		in := types.Inbound{
			SourceChain: "eip155:11155111",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
			RawPayload:  "0x" + types.PC20Selector + strings.TrimPrefix(encoded, "0x"),
		}
		require.NoError(t, in.NormalizeForTxType())
		require.True(t, in.IsPc20, "PC20 selector must set is_pc20")
		require.NotNil(t, in.UniversalPayload)
		require.Equal(t, to.Hex(), in.UniversalPayload.To)
		require.Equal(t, "0xdeadbeef", in.UniversalPayload.Data)
	})

	t.Run("un-prefixed PRC20 payload is not PC20 and decodes unchanged", func(t *testing.T) {
		in := types.Inbound{
			SourceChain: "eip155:11155111",
			TxType:      types.TxType_FUNDS_AND_PAYLOAD,
			RawPayload:  encoded,
		}
		require.NoError(t, in.NormalizeForTxType())
		require.False(t, in.IsPc20)
		require.NotNil(t, in.UniversalPayload)
		require.Equal(t, to.Hex(), in.UniversalPayload.To)
	})
}
