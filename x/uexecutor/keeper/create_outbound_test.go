package keeper_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func outboundEvent(payload, token string) *types.UniversalTxOutboundEvent {
	return &types.UniversalTxOutboundEvent{
		TxID:            "0xabc123",
		Sender:          "0x1111111111111111111111111111111111111111",
		ChainId:         "eip155:11155111",
		Token:           token,
		Target:          "0x2222222222222222222222222222222222222222",
		Amount:          big.NewInt(1000),
		GasToken:        "0x3333333333333333333333333333333333333333",
		GasFee:          big.NewInt(10),
		GasLimit:        big.NewInt(21000),
		GasPrice:        big.NewInt(5),
		RevertRecipient: "0x4444444444444444444444444444444444444444",
		TxType:          types.TxType_FUNDS_AND_PAYLOAD,
		Payload:         payload,
	}
}

// A PC20 export must build an outbound WITHOUT consulting the PRC20 registry.
// Skipping that lookup is what stops the whole EVM tx (and the vault lock) from
// reverting for a Push-native token that has no PRC20 config. The absence of a
// gomock EXPECT on the uregistry keeper is the assertion: any registry call
// would fail the test.
func TestBuildOutboundFromEvent_PC20SkipsRegistry(t *testing.T) {
	f := SetupTest(t)

	pc20Token := "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	payload := "0x50433230000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7"

	out, err := f.k.TestBuildOutboundFromEvent(f.ctx, outboundEvent(payload, pc20Token), "0xdeadbeef", 3)
	require.NoError(t, err)
	require.NotNil(t, out)

	require.True(t, out.IsPc20)
	require.Equal(t, pc20Token, out.Pc20ContractAddress)
	require.Empty(t, out.ExternalAssetAddr, "PC20 export has no external asset at creation")
	require.Empty(t, out.Prc20AssetAddr, "PC20 export is not a PRC20")

	// Common fields carried straight from the event.
	require.Equal(t, "eip155:11155111", out.DestinationChain)
	require.Equal(t, "1000", out.Amount)
	require.Equal(t, "0x2222222222222222222222222222222222222222", out.Recipient)
	require.Equal(t, payload, out.Payload)
	require.Equal(t, types.TxType_FUNDS_AND_PAYLOAD, out.TxType)
	require.Equal(t, types.Status_PENDING, out.OutboundStatus)
	require.Equal(t, "abc123", out.Id, "0x prefix trimmed from tx id")
	require.Equal(t, "4444444444444444444444444444444444444444", trim0x(out.RevertInstructions.FundRecipient))
	require.Equal(t, "deadbeef", trim0x(out.PcTx.TxHash))
	require.Equal(t, "3", out.PcTx.LogIndex)
}

// A non-PC20 (PRC20) payload takes the existing path: resolve the external asset
// from the registry.
func TestBuildOutboundFromEvent_PRC20UsesRegistry(t *testing.T) {
	f := SetupTest(t)

	prc20 := "0x5555555555555555555555555555555555555555"
	external := "0x9999999999999999999999999999999999999999"

	f.mockUregistryKeeper.EXPECT().
		GetTokenConfigByPRC20(gomock.Any(), "eip155:11155111", prc20).
		Return(uregistrytypes.TokenConfig{Address: external}, nil)

	out, err := f.k.TestBuildOutboundFromEvent(f.ctx, outboundEvent("0x", prc20), "0xdeadbeef", 1)
	require.NoError(t, err)
	require.False(t, out.IsPc20)
	require.Empty(t, out.Pc20ContractAddress)
	require.Equal(t, external, out.ExternalAssetAddr)
	require.Equal(t, prc20, out.Prc20AssetAddr)
}

// A PRC20 payload whose token is unregistered still surfaces the registry error
// (unchanged behavior) — only PC20 is allowed to skip the lookup.
func TestBuildOutboundFromEvent_PRC20UnregisteredErrors(t *testing.T) {
	f := SetupTest(t)

	prc20 := "0x5555555555555555555555555555555555555555"
	f.mockUregistryKeeper.EXPECT().
		GetTokenConfigByPRC20(gomock.Any(), "eip155:11155111", prc20).
		Return(uregistrytypes.TokenConfig{}, errors.New("not found"))

	out, err := f.k.TestBuildOutboundFromEvent(f.ctx, outboundEvent("0x", prc20), "0xdeadbeef", 1)
	require.Error(t, err)
	require.Nil(t, out)
}

// The PC20 revert path depends on VaultPC20 being registered as a system
// contract. Until that wiring lands, CallVaultPC20RevertExport must fail fast
// with a clear error (before any EVM call) rather than silently calling the
// zero address.
func TestCallVaultPC20RevertExport_UnregisteredErrors(t *testing.T) {
	f := SetupTest(t)

	_, ok := uregistrytypes.SYSTEM_CONTRACTS["VAULT_PC20"]
	require.False(t, ok, "precondition: VAULT_PC20 not yet registered")

	_, err := f.k.CallVaultPC20RevertExport(
		f.ctx,
		common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ab"),
		common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		big.NewInt(1000),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "VAULT_PC20")
}

func trim0x(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
