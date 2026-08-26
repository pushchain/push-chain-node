package integrationtest

import (
	"testing"

	"cosmossdk.io/math"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	"github.com/pushchain/push-chain-node/types"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// TestGaslessExecutePayloadWithModuleSender is the invariant guard for
// F-2026-18197.
//
// The fix hardens x/vm's Keeper.EthereumTx to require that msg.From is the
// ECDSA signer of the raw transaction, so that an MsgEthereumTx smuggled in via
// a nested-message dispatcher can no longer execute as somebody else. Push's
// gasless / module-sender flows must be completely unaffected by that, and they
// are - because they never reach that msg server. MsgExecutePayload runs the
// payload through CallEVM / DerivedEVMCall, which go straight to
// ApplyMessageWithConfig; no MsgEthereumTx is ever constructed.
//
// This test pins that down end to end: a gasless MsgExecutePayload, whose EVM
// caller is the uexecutor module account, still executes successfully.
func TestGaslessExecutePayloadWithModuleSender(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	// The uexecutor module account is derived from a name, not from a key pair.
	// It can never produce an ECDSA signature, so if a module operation ever
	// routed through MsgEthereumTx the new VerifySender check would reject it
	// 100% of the time. That is why module-driven EVM calls must keep using the
	// ApplyMessage* path, and why this test exists.
	moduleAcc := app.AccountKeeper.GetModuleAccount(ctx, uexecutortypes.ModuleName)
	require.NotNil(t, moduleAcc)
	require.Nil(t, moduleAcc.GetPubKey(),
		"the uexecutor module account must have no public key - it cannot sign an MsgEthereumTx")

	app.UregistryKeeper.AddChainConfig(ctx, &uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{{
			Name:            "addFunds",
			Identifier:      "",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	})

	params := app.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = math.LegacyNewDec(1000000000)
	app.FeeMarketKeeper.SetParams(ctx, params)

	ms := uexecutorkeeper.NewMsgServerImpl(app.UexecutorKeeper)

	universalAccount := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
	}
	payload := &uexecutortypes.UniversalPayload{
		To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
		Value:                "0",
		Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
		GasLimit:             "21000000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "1",
		Deadline:             "0",
		VType:                uexecutortypes.VerificationType(0),
	}

	evmFrom := common.HexToAddress("0x1000000000000000000000000000000000000001")

	err := app.BankKeeper.MintCoins(
		ctx,
		uexecutortypes.ModuleName,
		sdk.NewCoins(sdk.NewCoin(types.BaseDenom, sdkmath.NewInt(2_000_000_000_000_000))),
	)
	require.NoError(t, err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		uexecutortypes.ModuleName,
		sdk.AccAddress(evmFrom.Bytes()),
		sdk.NewCoins(sdk.NewCoin(types.BaseDenom, sdkmath.NewInt(1_000_000_000_000_000))),
	)
	require.NoError(t, err)

	_, err = app.UexecutorKeeper.DeployUEAV2(ctx, evmFrom, universalAccount)
	require.NoError(t, err)

	ueaAddr, _, err := app.UexecutorKeeper.CallFactoryToGetUEAAddressForOrigin(
		ctx, evmFrom, utils.GetDefaultAddresses().FactoryAddr, universalAccount,
	)
	require.NoError(t, err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		uexecutortypes.ModuleName,
		sdk.AccAddress(ueaAddr.Bytes()),
		sdk.NewCoins(sdk.NewCoin(types.BaseDenom, sdkmath.NewInt(1_000_000_000_000_000))),
	)
	require.NoError(t, err)

	// The gasless message itself: signer is a relayer, the EVM caller is the
	// uexecutor module. This must still succeed after the x/vm change.
	// testSigner (execute_payload_test.go) is a valid 20-byte account. The
	// literal this test originally carried decoded to 42 bytes, which
	// F-2026-18200's signer-length guard rejects in GetAddressPair before
	// ExecutePayload does any work - so the test failed on an address that was
	// never the thing under test.
	_, err = ms.ExecutePayload(ctx, &uexecutortypes.MsgExecutePayload{
		Signer:             testSigner,
		UniversalAccountId: universalAccount,
		UniversalPayload:   payload,
		VerificationData:   "0x91987784d56359fa91c3e3e0332f4f0cffedf9c081eb12874a63b41d5b5e5c660dc827947c2ae26e658d0551ad4b2d2aa073d62691429a0ae239d2cc58055bf11c",
	})
	require.NoError(t, err, "gasless module-sender MsgExecutePayload must still execute end to end")
}
