package integrationtest

import (
	"testing"

	"cosmossdk.io/math"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	utils "github.com/pushchain/push-chain-node/test/utils"
	"github.com/pushchain/push-chain-node/types"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestExecutePayload(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{&uregistrytypes.GatewayMethods{
			Name:            "addFunds",
			Identifier:      "",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}

	app.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)

	params := app.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = math.LegacyNewDec(1000000000)
	app.FeeMarketKeeper.SetParams(ctx, params)

	ms := uexecutorkeeper.NewMsgServerImpl(app.UexecutorKeeper)

	t.Run("Success!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validUP := &uexecutortypes.UniversalPayload{
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
		// mint to module (or faucet)
		err := app.BankKeeper.MintCoins(
			ctx,
			uexecutortypes.ModuleName,
			sdk.NewCoins(sdk.NewCoin(types.BaseDenom, sdkmath.NewInt(2_000_000_000_000_000))),
		)
		require.NoError(t, err)

		// send to evmFrom
		err = app.BankKeeper.SendCoinsFromModuleToAccount(
			ctx,
			uexecutortypes.ModuleName,
			sdk.AccAddress(evmFrom.Bytes()),
			sdk.NewCoins(sdk.NewCoin(types.BaseDenom, sdkmath.NewInt(1_000_000_000_000_000))),
		)
		require.NoError(t, err)

		_, err = app.UexecutorKeeper.DeployUEAV2(ctx, evmFrom, validUA)
		require.NoError(t, err)

		factoryAddr := utils.GetDefaultAddresses().FactoryAddr
		ueaAddr, _, err := app.UexecutorKeeper.CallFactoryToGetUEAAddressForOrigin(ctx, evmFrom, factoryAddr, validUA)
		require.NoError(t, err)

		// send to UEA
		err = app.BankKeeper.SendCoinsFromModuleToAccount(
			ctx,
			uexecutortypes.ModuleName,
			sdk.AccAddress(ueaAddr.Bytes()),
			sdk.NewCoins(sdk.NewCoin(types.BaseDenom, sdkmath.NewInt(1_000_000_000_000_000))),
		)
		require.NoError(t, err)

		msg := &uexecutortypes.MsgExecutePayload{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "0x91987784d56359fa91c3e3e0332f4f0cffedf9c081eb12874a63b41d5b5e5c660dc827947c2ae26e658d0551ad4b2d2aa073d62691429a0ae239d2cc58055bf11c",
		}

		_, err = ms.ExecutePayload(ctx, msg)
		require.NoError(t, err)

	})

	t.Run("Invalid Universal Payload!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validUP := &uexecutortypes.UniversalPayload{
			To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
			Value:                "0",
			Data:                 "0xZZZZ",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
		}

		msg := &uexecutortypes.MsgExecutePayload{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
		}

		_, err := ms.ExecutePayload(ctx, msg)
		require.ErrorContains(t, err, "invalid universal payload")

	})

	t.Run("Invalid Verification Data", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validUP := &uexecutortypes.UniversalPayload{
			To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
			Value:                "0",
			Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(0),
		}

		msg := &uexecutortypes.MsgExecutePayload{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "0xZZZZ",
		}

		_, err := ms.ExecutePayload(ctx, msg)
		require.ErrorContains(t, err, "invalid verificationData format")

	})

}

// TestExecutePayload_AutoDeployOnPreFundedAddress exercises the griefing-recovery path:
// when a non-deployed UEA address already holds a non-zero native balance (e.g. because
// an attacker front-ran with a dust deposit to the precomputed address), MsgExecutePayload
// should auto-deploy the UEA before running the payload, instead of rejecting the tx and
// leaving the owner unable to deploy.
func TestExecutePayload_AutoDeployOnPreFundedAddress(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{&uregistrytypes.GatewayMethods{
			Name:            "addFunds",
			Identifier:      "",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}
	app.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)

	params := app.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = math.LegacyNewDec(1000000000)
	app.FeeMarketKeeper.SetParams(ctx, params)

	ms := uexecutorkeeper.NewMsgServerImpl(app.UexecutorKeeper)

	// Same fixture as TestExecutePayload/Success! — owner has a pre-signed verificationData
	// for this exact payload+nonce, so the execute step can succeed end-to-end.
	validUA := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
	}
	validUP := &uexecutortypes.UniversalPayload{
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

	// Precompute the UEA address WITHOUT deploying — this is the attacker-grief setup.
	factoryAddr := utils.GetDefaultAddresses().FactoryAddr
	ueaAddr, isDeployed, err := app.UexecutorKeeper.CallFactoryToGetUEAAddressForOrigin(ctx, evmFrom, factoryAddr, validUA)
	require.NoError(t, err)
	require.False(t, isDeployed, "precondition: UEA must not be deployed before the test call")

	// "Attacker" pre-funds the precomputed UEA address. This is what would confuse a
	// balance-based SDK into routing to MsgExecutePayload instead of the deploy msg.
	err = app.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		uexecutortypes.ModuleName,
		sdk.AccAddress(ueaAddr.Bytes()),
		sdk.NewCoins(sdk.NewCoin(types.BaseDenom, sdkmath.NewInt(1_000_000_000_000_000))),
	)
	require.NoError(t, err)

	// Submit MsgExecutePayload directly — no standalone DeployUEAV2 call beforehand.
	msg := &uexecutortypes.MsgExecutePayload{
		Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
		UniversalAccountId: validUA,
		UniversalPayload:   validUP,
		VerificationData:   "0x91987784d56359fa91c3e3e0332f4f0cffedf9c081eb12874a63b41d5b5e5c660dc827947c2ae26e658d0551ad4b2d2aa073d62691429a0ae239d2cc58055bf11c",
	}

	_, err = ms.ExecutePayload(ctx, msg)
	require.NoError(t, err, "auto-deploy + execute should succeed when precomputed UEA holds balance")

	// Post-condition: the UEA must now be deployed.
	_, isDeployed, err = app.UexecutorKeeper.CallFactoryToGetUEAAddressForOrigin(ctx, evmFrom, factoryAddr, validUA)
	require.NoError(t, err)
	require.True(t, isDeployed, "UEA must be deployed after auto-deploy path runs successfully")
}

// TestExecutePayload_RejectWhenUndeployedAndUnfunded asserts the rejection arm of the
// auto-deploy logic: when the UEA is not deployed AND has zero native balance, there is
// no griefing to recover from, so MsgExecutePayload must still reject with the existing
// "UEA is not deployed" error rather than deploying on-demand for free.
func TestExecutePayload_RejectWhenUndeployedAndUnfunded(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{&uregistrytypes.GatewayMethods{
			Name:            "addFunds",
			Identifier:      "",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}
	app.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)

	params := app.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = math.LegacyNewDec(1000000000)
	app.FeeMarketKeeper.SetParams(ctx, params)

	ms := uexecutorkeeper.NewMsgServerImpl(app.UexecutorKeeper)

	// Distinct owner — keeps the UEA address disjoint from any other test fixture and
	// ensures neither deploy nor balance exists for this address in fresh state.
	validUA := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x1111111111111111111111111111111111111111",
	}
	// Payload and verificationData are well-formed (pass early validation) but the
	// signature does not need to be valid: the handler must reject at the deploy gate,
	// well before signature verification, so we never hit the UEA contract.
	validUP := &uexecutortypes.UniversalPayload{
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

	msg := &uexecutortypes.MsgExecutePayload{
		Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
		UniversalAccountId: validUA,
		UniversalPayload:   validUP,
		VerificationData:   "0x1234",
	}

	_, err := ms.ExecutePayload(ctx, msg)
	// "UEA is not deployed" is the gate that fires *before* any auto-deploy attempt.
	// Any other error string (e.g. signature-verification revert) would indicate that
	// the handler stealth-deployed the UEA and then ran the payload — which must not
	// happen when the address has zero balance.
	require.ErrorContains(t, err, "UEA is not deployed")
}
