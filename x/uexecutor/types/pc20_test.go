package types_test

import (
	"math/big"
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
