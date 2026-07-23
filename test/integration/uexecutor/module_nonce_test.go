package integrationtest

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestIsUeModuleAddress verifies the module-address check compares raw 20-byte
// values (VM-native identity), so hex casing / EIP-55 checksum never matters and
// a re-parsed lower/upper-case form of the module address still resolves as the
// module — while any other address does not.
func TestIsUeModuleAddress(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UexecutorKeeper

	moduleAddr, moduleHex := k.GetUeModuleAddress(ctx)

	require.True(t, k.IsUeModuleAddress(ctx, moduleAddr), "the module address must resolve as the module")
	// Re-parsed from all-lowercase hex → identical 20 bytes → still the module.
	require.True(t, k.IsUeModuleAddress(ctx, common.HexToAddress(strings.ToLower(moduleHex))))
	// Re-parsed from all-uppercase (non-checksummed) hex → identical 20 bytes → still the module.
	require.True(t, k.IsUeModuleAddress(ctx, common.HexToAddress("0x"+strings.ToUpper(strings.TrimPrefix(moduleHex, "0x")))))

	require.False(t, k.IsUeModuleAddress(ctx, common.HexToAddress("0x00000000000000000000000000000000dEadBeef")), "a different address is not the module")
	require.False(t, k.IsUeModuleAddress(ctx, common.HexToAddress("0x1234567890AbCdeF1234567890abCDeF12345678")), "an unrelated EVM address is not the module")
	require.False(t, k.IsUeModuleAddress(ctx, common.Address{}), "the zero address is not the module")

	// A Solana (SVM) address folded into 20 bytes must not collide with the module.
	solBytes, err := base58.Decode("So11111111111111111111111111111111111111112")
	require.NoError(t, err)
	require.False(t, k.IsUeModuleAddress(ctx, common.BytesToAddress(solBytes)), "an SVM-derived address is not the module")
}

// TestModuleNonce_NonModuleSenderDoesNotAdvanceModuleNonce proves the module-nonce
// counter is only touched for a module sender. A non-module `from` skips the
// module-nonce path entirely (before any EVM work), so ModuleAccountNonce is
// unchanged regardless of whether the downstream EVM call itself succeeds.
func TestModuleNonce_NonModuleSenderDoesNotAdvanceModuleNonce(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UexecutorKeeper

	before, err := k.GetModuleAccountNonce(ctx)
	require.NoError(t, err)

	user := common.HexToAddress("0x00000000000000000000000000000000000000A1")
	uea := common.HexToAddress("0x00000000000000000000000000000000000000A2")
	payload := &uexecutortypes.UniversalPayload{
		To: user.Hex(), Value: "0", Data: "0x", GasLimit: "100000",
		MaxFeePerGas: "0", MaxPriorityFeePerGas: "0", Nonce: "0", Deadline: "0",
	}
	// The EVM call may fail (no real UEA / no signature) — irrelevant to nonce
	// accounting, which is decided before the call.
	_, _ = k.CallUEAExecutePayload(ctx, user, uea, payload, nil)

	after, err := k.GetModuleAccountNonce(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "a non-module sender must not advance ModuleAccountNonce")
}

// TestModuleNonce_ModuleSenderAdvancesModuleNonce is the isolated regression: a
// UEA payload execution sent by the module must advance ModuleAccountNonce by one.
// Before the fix this call sourced its nonce from the (static) account sequence and
// left ModuleAccountNonce untouched, so it silently reused whatever nonce a prior
// module call in the same inbound had already used.
func TestModuleNonce_ModuleSenderAdvancesModuleNonce(t *testing.T) {
	app, ctx, _, _, _, ueaAddr := setupInboundInitiatedOutboundTest(t, 4)
	k := app.UexecutorKeeper
	moduleAddr, _ := k.GetUeModuleAddress(ctx)

	before, err := k.GetModuleAccountNonce(ctx)
	require.NoError(t, err)

	// A minimal, freshly-nonced payload (UEA nonce is 0 right after deployment). The
	// module sender bypasses the UEA signature check, so this executes; either way
	// the module-nonce path runs before the EVM call.
	payload := &uexecutortypes.UniversalPayload{
		To: "0x00000000000000000000000000000000000000A9", Value: "0", Data: "0x",
		GasLimit: "200000", MaxFeePerGas: "0", MaxPriorityFeePerGas: "0", Nonce: "0", Deadline: "0",
	}
	_, _ = k.CallUEAExecutePayload(ctx, moduleAddr, ueaAddr, payload, nil)

	after, err := k.GetModuleAccountNonce(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "a module-sender UEA execution must advance ModuleAccountNonce by exactly 1")
}

// TestModuleNonce_InboundPayloadExecutionAdvancesModuleNonce is the end-to-end
// regression. A FUNDS_AND_PAYLOAD inbound makes two module-sender EVM calls — the
// funds deposit AND the UEA payload execution — and each must advance
// ModuleAccountNonce (Δ2). Before the fix the payload execution didn't, so Δ was 1
// and the UEA tx reused the deposit's nonce.
func TestModuleNonce_InboundPayloadExecutionAdvancesModuleNonce(t *testing.T) {
	app, ctx, vals, inbound, coreVals, _ := setupInboundInitiatedOutboundTest(t, 4)
	k := app.UexecutorKeeper

	before, err := k.GetModuleAccountNonce(ctx)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteInbound(t, ctx, app, vals[i], sdk.AccAddress(valAddr).String(), inbound))
	}

	after, err := k.GetModuleAccountNonce(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), after-before,
		"deposit + UEA payload execution must each advance ModuleAccountNonce (Δ2); pre-fix Δ was 1")
}
