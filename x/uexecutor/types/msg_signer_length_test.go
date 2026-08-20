package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// uexecutorModuleEVMAddr is sha256("uexecutor")[:20] rendered as an EVM address.
// The UEA contract trusts calls from it unconditionally.
const uexecutorModuleEVMAddr = "0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7"

// aliasedModuleSigner returns a bech32 signer of the given byte length whose
// rightmost 20 bytes are the uexecutor module account, so that the downstream
// conversion to a 20-byte EVM address collapses onto the module itself.
func aliasedModuleSigner(t *testing.T, length int) string {
	t.Helper()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	require.Len(t, moduleAddr, common.AddressLength)
	require.Equal(t, uexecutorModuleEVMAddr, common.BytesToAddress(moduleAddr).Hex())

	prefix := make([]byte, length-common.AddressLength)
	prefix[0] = 0x01
	addr := sdk.AccAddress(append(prefix, moduleAddr...))
	require.Equal(t, uexecutorModuleEVMAddr, common.BytesToAddress(addr).Hex())
	return addr.String()
}

// TestGaslessMsgs_RejectOverlongSigner is the CheckTx-time guard for
// F-2026-18200: both gasless messages must reject a signer that does not decode
// to exactly 20 bytes, before the ante chain ever runs.
func TestGaslessMsgs_RejectOverlongSigner(t *testing.T) {
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	for _, length := range []int{21, 22, 32} {
		signer := aliasedModuleSigner(t, length)

		execMsg := &types.MsgExecutePayload{
			Signer:             signer,
			UniversalAccountId: validUA,
			UniversalPayload: &types.UniversalPayload{
				To:   "0x000000000000000000000000000000000000dead",
				Data: "0xabcdef",
			},
			VerificationData: "abcdef",
		}
		err := execMsg.ValidateBasic()
		require.Error(t, err, "MsgExecutePayload must reject a %d-byte signer", length)
		require.Contains(t, err.Error(), "invalid signer address length")

		migrateMsg := &types.MsgMigrateUEA{
			Signer:             signer,
			UniversalAccountId: validUA,
			MigrationPayload: &types.MigrationPayload{
				Migration: "0x000000000000000000000000000000000000beef",
				Nonce:     "0",
				Deadline:  "1",
			},
			Signature: "abcdef",
		}
		err = migrateMsg.ValidateBasic()
		require.Error(t, err, "MsgMigrateUEA must reject a %d-byte signer", length)
		require.Contains(t, err.Error(), "invalid signer address length")
	}
}

// TestGaslessMsgs_Accept20ByteSigner is the positive control.
func TestGaslessMsgs_Accept20ByteSigner(t *testing.T) {
	signer := sdk.AccAddress(make([]byte, common.AddressLength)).String()
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	execMsg := &types.MsgExecutePayload{
		Signer:             signer,
		UniversalAccountId: validUA,
		UniversalPayload: &types.UniversalPayload{
			To:   "0x000000000000000000000000000000000000dead",
			Data: "0xabcdef",
		},
		VerificationData: "abcdef",
	}
	require.NoError(t, execMsg.ValidateBasic())

	migrateMsg := &types.MsgMigrateUEA{
		Signer:             signer,
		UniversalAccountId: validUA,
		MigrationPayload: &types.MigrationPayload{
			Migration: "0x000000000000000000000000000000000000beef",
			Nonce:     "0",
			Deadline:  "1",
		},
		Signature: "abcdef",
	}
	require.NoError(t, migrateMsg.ValidateBasic())
}
