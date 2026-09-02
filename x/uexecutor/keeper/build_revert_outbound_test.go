package keeper_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

const (
	revertSourceChain = "eip155:11155111"
	revertAssetAddr   = "0x0000000000000000000000000000000000000e07"
	revertPRC20Addr   = "0x0000000000000000000000000000000000000e06"
	revertGasTokenHex = "0x0000000000000000000000000000000000001111"
)

// revertTestInbound is a non-CEA FUNDS inbound whose execution failed, i.e. the
// input to buildRevertOutbound.
func revertTestInbound() *types.Inbound {
	return &types.Inbound{
		SourceChain: revertSourceChain,
		TxHash:      "0xdeadbeef",
		LogIndex:    "1",
		Sender:      "0x778d3206374F8ac265728e18E3fE2Ae6b93E4ce4",
		Recipient:   "0x778d3206374F8ac265728e18E3fE2Ae6b93E4ce4",
		Amount:      "1000000",
		AssetAddr:   revertAssetAddr,
		TxType:      types.TxType_FUNDS,
		RevertInstructions: &types.RevertInstructions{
			FundRecipient: "0x527F3692F5C53CfA83F7689885995606F93b6164",
		},
	}
}

func revertTestTokenConfig() uregistrytypes.TokenConfig {
	return uregistrytypes.TokenConfig{
		Chain:   revertSourceChain,
		Address: revertAssetAddr,
		Enabled: true,
		NativeRepresentation: &uregistrytypes.NativeRepresentation{
			ContractAddress: revertPRC20Addr,
		},
	}
}

// expectGasFeeCall stubs UniversalCore.getOutboundTxGasAndFees to return a
// well-formed 6-output response, i.e. the healthy path.
func expectGasFeeCall(t *testing.T, f *testFixture, gasFee, gasPrice, gasLimit *big.Int) {
	t.Helper()

	ucABI, err := types.ParseUniversalCoreABI()
	require.NoError(t, err)

	packed, err := ucABI.Methods["getOutboundTxGasAndFees"].Outputs.Pack(
		common.HexToAddress(revertGasTokenHex), // gasToken
		gasFee,                                 // gasFee
		big.NewInt(0),                          // protocolFee
		gasPrice,                               // gasPrice
		"eip155",                               // chainNamespace
		gasLimit,                               // gasLimitUsed
	)
	require.NoError(t, err)

	// cosmos/evm v0.6.0: GetGasFeeInfoForRevertOutbound builds the StateDB itself
	// and passes it into CallEVM, so the mock must expect that call too.
	f.mockEVMKeeper.EXPECT().NewStateDB(gomock.Any()).Return(nil).AnyTimes()
	f.mockEVMKeeper.EXPECT().
		CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Eq("getOutboundTxGasAndFees"), gomock.Any(), gomock.Any()).
		Return(&evmtypes.MsgEthereumTxResponse{Ret: packed}, nil).
		AnyTimes()
}

// attachRevert stores a UTX and runs the revert outbound through the same attach
// path the production callers use, so PendingOutbounds indexing is exercised.
func attachRevert(t *testing.T, f *testFixture, utxId string, ob *types.OutboundTx) {
	t.Helper()

	require.NoError(t, f.k.UniversalTx.Set(f.ctx, utxId, types.UniversalTx{
		Id:        utxId,
		InboundTx: revertTestInbound(),
	}))
	f.mockUregistryKeeper.EXPECT().
		GetChainConfig(gomock.Any(), revertSourceChain).
		Return(uregistrytypes.ChainConfig{Chain: revertSourceChain}, nil).
		AnyTimes()

	require.NoError(t, f.k.TestAttachOutboundsToUtx(f.ctx, utxId, []*types.OutboundTx{ob}, "execution failed"))
}

func hasEvent(events sdk.Events, evtType string) bool {
	for _, e := range events {
		if e.Type == evtType {
			return true
		}
	}
	return false
}

// TestBuildRevertOutbound_HealthyPath is the regression guard for the untouched
// path: when the gas metadata resolves, the revert is PENDING, carries the exact
// values UniversalCore returned, and is indexed for universal-validator pickup.
func TestBuildRevertOutbound_HealthyPath(t *testing.T) {
	f := setupPendingOutboundFixture(t)

	f.mockUregistryKeeper.EXPECT().
		GetTokenConfig(gomock.Any(), revertSourceChain, revertAssetAddr).
		Return(revertTestTokenConfig(), nil).
		AnyTimes()
	expectGasFeeCall(t, f, big.NewInt(123_456), big.NewInt(1_000_000_000), big.NewInt(200_000))

	inbound := revertTestInbound()
	ob, err := f.k.TestBuildRevertOutbound(f.ctx, inbound)
	require.NoError(t, err, "healthy gas metadata must not produce an error")
	require.NotNil(t, ob)

	require.Equal(t, types.Status_PENDING, ob.OutboundStatus, "healthy revert must stay PENDING so UVs sign it")
	require.Empty(t, ob.AbortReason, "healthy revert must carry no abort reason")
	require.Equal(t, types.TxType_INBOUND_REVERT, ob.TxType)
	require.Equal(t, revertSourceChain, ob.DestinationChain)
	require.Equal(t, inbound.Amount, ob.Amount)
	require.Equal(t, inbound.AssetAddr, ob.ExternalAssetAddr)
	require.Equal(t, inbound.RevertInstructions.FundRecipient, ob.Recipient)

	// Gas fields exactly as UniversalCore returned them.
	require.Equal(t, common.HexToAddress(revertGasTokenHex).Hex(), ob.GasToken)
	require.Equal(t, "123456", ob.GasFee)
	require.Equal(t, "1000000000", ob.GasPrice)
	require.Equal(t, "200000", ob.GasLimit)

	// ...and it still enters the signing queue.
	attachRevert(t, f, "utx-healthy", ob)
	entry, err := f.k.PendingOutbounds.Get(f.ctx, ob.Id)
	require.NoError(t, err, "a PENDING revert must be indexed in PendingOutbounds")
	require.Equal(t, "utx-healthy", entry.UniversalTxId)
}

// TestBuildRevertOutbound_GasFeeLookupFails is the headline case: the
// UniversalCore call reverts, so the outbound must be recorded ABORTED and must
// never reach the signing queue.
func TestBuildRevertOutbound_GasFeeLookupFails(t *testing.T) {
	f := setupPendingOutboundFixture(t)

	f.mockUregistryKeeper.EXPECT().
		GetTokenConfig(gomock.Any(), revertSourceChain, revertAssetAddr).
		Return(revertTestTokenConfig(), nil).
		AnyTimes()
	// cosmos/evm v0.6.0: GetGasFeeInfoForRevertOutbound builds the StateDB itself
	// and passes it into CallEVM, so the mock must expect that call too.
	f.mockEVMKeeper.EXPECT().NewStateDB(gomock.Any()).Return(nil).AnyTimes()
	f.mockEVMKeeper.EXPECT().
		CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Eq("getOutboundTxGasAndFees"), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("execution reverted: ZeroGasPrice")).
		AnyTimes()

	ob, err := f.k.TestBuildRevertOutbound(f.ctx, revertTestInbound())
	require.Error(t, err, "a gas-metadata failure must be reported, not swallowed")
	require.Contains(t, err.Error(), "gas fee info")
	require.NotNil(t, ob, "the aborted attempt is still returned so it can be recorded")

	require.Equal(t, types.Status_ABORTED, ob.OutboundStatus,
		"an unsignable revert must be ABORTED, never PENDING")
	require.NotEmpty(t, ob.AbortReason, "abort reason must explain why the revert could not be built")
	require.Contains(t, ob.AbortReason, "ZeroGasPrice")
	require.Empty(t, ob.GasFee)
	require.Empty(t, ob.GasPrice)
	require.Empty(t, ob.GasLimit)
	require.Empty(t, ob.GasToken)

	// Recorded on the UTX for the audit trail...
	attachRevert(t, f, "utx-aborted", ob)
	utx, found, err := f.k.GetUniversalTx(f.ctx, "utx-aborted")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, utx.OutboundTx, 1)
	require.Equal(t, types.Status_ABORTED, utx.OutboundTx[0].OutboundStatus)

	// ...but NOT queued for signing: an unsignable row here would sit forever.
	has, err := f.k.PendingOutbounds.Has(f.ctx, ob.Id)
	require.NoError(t, err)
	require.False(t, has, "an ABORTED revert must never be indexed in PendingOutbounds")

	require.True(t, hasEvent(f.ctx.EventManager().Events(), "outbound_aborted"),
		"an outbound_aborted event must be emitted so monitoring sees the failure")
}

// TestBuildRevertOutbound_TokenConfigMissing covers the other fail-open branch:
// the PRC20 for the inbound asset cannot be resolved at all.
func TestBuildRevertOutbound_TokenConfigMissing(t *testing.T) {
	f := setupPendingOutboundFixture(t)

	f.mockUregistryKeeper.EXPECT().
		GetTokenConfig(gomock.Any(), revertSourceChain, revertAssetAddr).
		Return(uregistrytypes.TokenConfig{}, errors.New("token config not found")).
		AnyTimes()

	ob, err := f.k.TestBuildRevertOutbound(f.ctx, revertTestInbound())
	require.Error(t, err)
	require.Contains(t, err.Error(), "PRC20")
	require.NotNil(t, ob)
	require.Equal(t, types.Status_ABORTED, ob.OutboundStatus)
	require.Contains(t, ob.AbortReason, "token config not found")

	attachRevert(t, f, "utx-no-token-config", ob)
	has, err := f.k.PendingOutbounds.Has(f.ctx, ob.Id)
	require.NoError(t, err)
	require.False(t, has, "an ABORTED revert must never be indexed in PendingOutbounds")
}

// TestBuildRevertOutbound_TokenConfigWithoutNativeRepresentation covers a token
// config that resolves but carries no PRC20 — the lookup returns no error, so the
// abort reason has to be synthesised.
func TestBuildRevertOutbound_TokenConfigWithoutNativeRepresentation(t *testing.T) {
	f := setupPendingOutboundFixture(t)

	f.mockUregistryKeeper.EXPECT().
		GetTokenConfig(gomock.Any(), revertSourceChain, revertAssetAddr).
		Return(uregistrytypes.TokenConfig{Chain: revertSourceChain, Address: revertAssetAddr}, nil).
		AnyTimes()

	ob, err := f.k.TestBuildRevertOutbound(f.ctx, revertTestInbound())
	require.Error(t, err)
	require.NotNil(t, ob)
	require.Equal(t, types.Status_ABORTED, ob.OutboundStatus)
	require.Contains(t, ob.AbortReason, "no native representation")
}

// TestBuildRevertOutbound_NilInbound proves the (outbound, error) contract: a nil
// outbound only ever comes back with a non-nil error, which is what the admin
// revert path checks before it claims a revert was created.
func TestBuildRevertOutbound_NilInbound(t *testing.T) {
	f := setupPendingOutboundFixture(t)

	ob, err := f.k.TestBuildRevertOutbound(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, ob)
}
