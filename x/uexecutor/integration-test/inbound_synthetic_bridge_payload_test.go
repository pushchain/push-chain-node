package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/testutils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func setupInboundBridgePayloadTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{{
			Name:             "addFunds",
			Identifier:       "",
			EventIdentifier:  "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
			ConfirmationType: 5,
		}},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr

	tokenConfigTest := uregistrytypes.TokenConfig{
		Chain:        "eip155:11155111",
		Address:      prc20Address.String(),
		Name:         "USD Coin",
		Symbol:       "USDC",
		Decimals:     6,
		Enabled:      true,
		LiquidityCap: "1000000000000000000000000",
		TokenType:    1,
		NativeRepresentation: &uregistrytypes.NativeRepresentation{
			Denom:           "",
			ContractAddress: prc20Address.String(),
		},
	}

	systemConfigTest := uregistrytypes.SystemConfig{
		HandlerContractAddress: utils.GetDefaultAddresses().HandlerAddr.String(),
	}

	app.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)
	app.UregistryKeeper.AddTokenConfig(ctx, &tokenConfigTest)
	app.UregistryKeeper.SetSystemConfig(ctx, systemConfigTest)

	// Register each validator with a universal validator
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		universalValAddr := sdk.AccAddress([]byte(
			fmt.Sprintf("universal-validator-%d", i),
		)).String()

		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr)
		require.NoError(t, err)

		universalVals[i] = universalValAddr
	}

	// Grant authz permission: core validator -> universal validator
	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress) // gives ValAddress
		require.NoError(t, err)

		coreValAddr := sdk.AccAddress(accAddr) // converts to normal account address

		uniValAddr := sdk.MustAccAddressFromBech32(universalVals[i])

		// Define grant for MsgVoteInbound
		msgType := sdk.MsgTypeURL(&uexecutortypes.MsgVoteInbound{})
		auth := authz.NewGenericAuthorization(msgType)

		// Expiration
		exp := ctx.BlockTime().Add(time.Hour)

		// SaveGrant takes (ctx, grantee, granter, authz.Authorization, *time.Time)
		err = app.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, auth, &exp)
		require.NoError(t, err)
	}

	validUA := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          testAddress,
	}

	ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)
	ueaAddrHex, err := app.UexecutorKeeper.DeployUEAV2(ctx, ueModuleAccAddress, validUA)
	require.NoError(t, err)

	// signature
	validVerificationData := ""

	validUP := &uexecutortypes.UniversalPayload{
		To:                   ueaAddrHex.String(),
		Value:                "1000000",
		Data:                 "0xa9059cbb000000000000000000000000527f3692f5c53cfa83f7689885995606f93b616400000000000000000000000000000000000000000000000000000000000f4240",
		GasLimit:             "21000000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "1",
		Deadline:             "9999999999",
		VType:                uexecutortypes.VerificationType(1),
	}

	inbound := &uexecutortypes.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0xabcd",
		Sender:           testAddress,
		Recipient:        "",
		Amount:           "1000000",
		AssetAddr:        prc20Address.String(),
		LogIndex:         "1",
		TxType:           uexecutortypes.InboundTxType_FUNDS_AND_PAYLOAD,
		UniversalPayload: validUP,
		VerificationData: validVerificationData,
	}

	return app, ctx, universalVals, inbound, validators
}

func TestInboundSyntheticBridgePayload(t *testing.T) {
	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr

	t.Run("less than quorum votes keeps inbound pending", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgePayloadTest(t, 4)
		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.True(t, isPending, "inbound should still be pending with < quorum votes")
	})

	t.Run("quorum reached executes inbound and payload executes if UEA deployed", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgePayloadTest(t, 4)

		// --- Quorum reached ---
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after quorum")
	})

	t.Run("quorum reached executes inbound and payload fails if UEA is not deployed", func(t *testing.T) {
		app, ctx, vals, _, coreVals := setupInboundBridgePayloadTest(t, 4)

		validUP := &uexecutortypes.UniversalPayload{
			To:                   utils.GetDefaultAddresses().DefaultTestAddr,
			Value:                "1000000",
			Data:                 "0xa9059cbb000000000000000000000000527f3692f5c53cfa83f7689885995606f93b616400000000000000000000000000000000000000000000000000000000000f4240",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}

		invalidInbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xabcd",
			Sender:           utils.GetDefaultAddresses().TargetAddr2,
			Recipient:        "",
			Amount:           "1000000",
			AssetAddr:        prc20Address.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.InboundTxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
		}

		for i := 0; i < 2; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, invalidInbound)
			require.NoError(t, err)
		}

		valAddr, err := sdk.ValAddressFromBech32(coreVals[2].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[2], coreValAcc, invalidInbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "UEA is not deployed")
	})

	t.Run("vote after quorum fails", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgePayloadTest(t, 4)
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		valAddr, err := sdk.ValAddressFromBech32(coreVals[3].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[3], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})

	t.Run("duplicate vote fails", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgePayloadTest(t, 4)
		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted")
	})

	t.Run("different inbounds tracked separately", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgePayloadTest(t, 4)
		inboundB := *inbound
		inboundB.TxHash = "0xabce"

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, &inboundB)
		require.NoError(t, err)
	})
}
