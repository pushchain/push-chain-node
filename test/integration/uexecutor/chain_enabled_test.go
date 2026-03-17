package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"

	"github.com/pushchain/push-chain-node/app"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
)

// setupChainEnabledTest is a variant of setupInboundBridgeTest that
// accepts explicit inbound/outbound enabled flags on the chain config.
func setupChainEnabledTest(
	t *testing.T,
	numVals int,
	isInboundEnabled bool,
	isOutboundEnabled bool,
) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator) {
	t.Helper()

	testApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

	chainConfig := uregistrytypes.ChainConfig{
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
			IsInboundEnabled:  isInboundEnabled,
			IsOutboundEnabled: isOutboundEnabled,
		},
	}

	tokenConfig := uregistrytypes.TokenConfig{
		Chain:        "eip155:11155111",
		Address:      usdcAddress.String(),
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

	testApp.UregistryKeeper.AddChainConfig(ctx, &chainConfig)
	testApp.UregistryKeeper.AddTokenConfig(ctx, &tokenConfig)

	universalVals := make([]string, len(validators))
	for i, val := range validators {
		universalValAddr := sdk.AccAddress([]byte(fmt.Sprintf("universal-validator-%d", i))).String()
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		err := testApp.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, network)
		require.NoError(t, err)
		universalVals[i] = universalValAddr
	}

	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		coreValAddr := sdk.AccAddress(accAddr)
		uniValAddr := sdk.MustAccAddressFromBech32(universalVals[i])

		auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteInbound{}))
		exp := ctx.BlockTime().Add(time.Hour)
		err = testApp.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, auth, &exp)
		require.NoError(t, err)
	}

	inbound := &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xabcd",
		Sender:      testAddress,
		Recipient:   testAddress,
		Amount:      "1000000",
		AssetAddr:   usdcAddress.String(),
		LogIndex:    "1",
		TxType:      uexecutortypes.TxType_FUNDS,
		RevertInstructions: &uexecutortypes.RevertInstructions{
			FundRecipient: testAddress,
		},
	}

	return testApp, ctx, universalVals, inbound, validators
}

// ----- VoteInbound tests -----

func TestVoteInbound_ChainEnabled(t *testing.T) {

	t.Run("fails when inbound is disabled for source chain", func(t *testing.T) {
		testApp, ctx, vals, inbound, coreVals := setupChainEnabledTest(t, 4, false, true)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, testApp, vals[0], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "inbound is disabled for chain eip155:11155111")
	})

	t.Run("no state changes when inbound is disabled", func(t *testing.T) {
		testApp, ctx, vals, inbound, coreVals := setupChainEnabledTest(t, 4, false, true)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		_ = utils.ExecVoteInbound(t, ctx, testApp, vals[0], coreValAcc, inbound)

		// Inbound must NOT be added to the pending set
		isPending, err := testApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should not be pending when inbound is disabled")

		// No UTX must be created
		_, found, err := testApp.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound))
		require.NoError(t, err)
		require.False(t, found, "no UTX should be created when inbound is disabled")
	})

	t.Run("all validator votes fail when inbound is disabled, no UTX created", func(t *testing.T) {
		testApp, ctx, vals, inbound, coreVals := setupChainEnabledTest(t, 4, false, true)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, testApp, vals[i], coreValAcc, inbound)
			require.Error(t, err)
			require.Contains(t, err.Error(), "inbound is disabled for chain eip155:11155111")
		}

		// Even after quorum-worth of attempts, no UTX should exist
		_, found, err := testApp.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound))
		require.NoError(t, err)
		require.False(t, found, "no UTX should be created even after quorum-worth of disabled attempts")
	})

	t.Run("succeeds and UTX is created when inbound is enabled", func(t *testing.T) {
		testApp, ctx, vals, inbound, coreVals := setupChainEnabledTest(t, 4, true, true)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, testApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utx, found, err := testApp.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound))
		require.NoError(t, err)
		require.True(t, found, "UTX should be created when inbound is enabled and quorum reached")
		require.NotEmpty(t, utx.PcTx, "PC tx should be recorded after inbound execution")
		require.Equal(t, "SUCCESS", utx.PcTx[len(utx.PcTx)-1].Status)
	})
}

// ----- ExecutePayload tests -----

func TestExecutePayload_ChainEnabled(t *testing.T) {

	// The IsInboundEnabled check in ExecutePayload fires after payload ABI parsing
	// and verificationData hex decode, but before any EVM calls — so no UEA or
	// funding setup is needed for the disabled case.

	t.Run("fails when inbound is disabled for the chain", func(t *testing.T) {
		testApp, ctx, _ := utils.SetAppWithValidators(t)

		testApp.UregistryKeeper.AddChainConfig(ctx, &uregistrytypes.ChainConfig{
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
				EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
			}},
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  false, // disabled
				IsOutboundEnabled: true,
			},
		})

		ms := uexecutorkeeper.NewMsgServerImpl(testApp.UexecutorKeeper)

		_, err := ms.ExecutePayload(ctx, &uexecutortypes.MsgExecutePayload{
			Signer: "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: &uexecutortypes.UniversalAccountId{
				ChainNamespace: "eip155",
				ChainId:        "11155111",
				Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
			},
			UniversalPayload: &uexecutortypes.UniversalPayload{
				To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "0",
				VType:                uexecutortypes.VerificationType(0),
			},
			VerificationData: "0x91987784d56359fa91c3e3e0332f4f0cffedf9c081eb12874a63b41d5b5e5c660dc827947c2ae26e658d0551ad4b2d2aa073d62691429a0ae239d2cc58055bf11c",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "inbound is disabled for chain eip155:11155111")
	})
}

// ----- Outbound creation tests -----

// setupOutboundChainEnabledTest mirrors setupInboundInitiatedOutboundTest but
// allows configuring IsOutboundEnabled on the chain config.
func setupOutboundChainEnabledTest(
	t *testing.T,
	numVals int,
	isOutboundEnabled bool,
) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator) {
	t.Helper()

	testApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

	testApp.UregistryKeeper.AddChainConfig(ctx, &uregistrytypes.ChainConfig{
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
			IsOutboundEnabled: isOutboundEnabled,
		},
	})

	testApp.UregistryKeeper.AddTokenConfig(ctx, &uregistrytypes.TokenConfig{
		Chain:        "eip155:11155111",
		Address:      usdcAddress.String(),
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
	})

	universalVals := make([]string, len(validators))
	for i, val := range validators {
		universalValAddr := sdk.AccAddress([]byte(fmt.Sprintf("universal-validator-%d", i))).String()
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		err := testApp.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, network)
		require.NoError(t, err)
		universalVals[i] = universalValAddr
	}

	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		coreValAddr := sdk.AccAddress(accAddr)
		uniValAddr := sdk.MustAccAddressFromBech32(universalVals[i])

		auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteInbound{}))
		exp := ctx.BlockTime().Add(time.Hour)
		err = testApp.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, auth, &exp)
		require.NoError(t, err)
	}

	// Deploy UEA — same as setupInboundInitiatedOutboundTest
	ueModuleAccAddress, _ := testApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	validUA := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          testAddress,
	}
	_, err := testApp.UexecutorKeeper.DeployUEAV2(ctx, ueModuleAccAddress, validUA)
	require.NoError(t, err)

	// FUNDS_AND_PAYLOAD inbound whose payload calls gateway withdraw,
	// emitting a UniversalTxOutbound event for eip155:11155111
	inbound := &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xabcd",
		Sender:      testAddress,
		Recipient:   "",
		Amount:      "1000000",
		AssetAddr:   usdcAddress.String(),
		LogIndex:    "1",
		TxType:      uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
		UniversalPayload: &uexecutortypes.UniversalPayload{
			To:                   utils.GetDefaultAddresses().UniversalGatewayPCAddr.Hex(),
			Value:                "0",
			Data:                 "0xb3ca1fbc000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000c00000000000000000000000000000000000000000000000000000000000000e0600000000000000000000000000000000000000000000000000000000000f4240000000000000000000000000000000000000000000000000000000000007a12000000000000000000000000000000000000000000000000000000000000001000000000000000000000000001234567890abcdef1234567890abcdef1234567800000000000000000000000000000000000000000000000000000000000000141234567890abcdef1234567890abcdef123456780000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "0",
			Deadline:             "0",
			VType:                uexecutortypes.VerificationType(0),
		},
		VerificationData: "0xa7531ada733322bd6708c94cba5a7dbd1ce25bccf010f774777b039713fc330643e23b7ef2a4609244900c6ab9a03d83d3ecf73edf6b451f21cc7dbda625a3211b",
	}

	return testApp, ctx, universalVals, inbound, validators
}

func TestOutbound_ChainEnabled(t *testing.T) {

	t.Run("outbound not created when destination chain outbound is disabled", func(t *testing.T) {
		testApp, ctx, vals, inbound, coreVals := setupOutboundChainEnabledTest(t, 4, false)

		// Reach quorum — VoteInbound itself must succeed (inbound is enabled)
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, testApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := testApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should still be created even when outbound is disabled")
		require.Empty(t, utx.OutboundTx, "no outbound should be attached when destination chain outbound is disabled")
	})

	t.Run("outbound is created when destination chain outbound is enabled", func(t *testing.T) {
		testApp, ctx, vals, inbound, coreVals := setupOutboundChainEnabledTest(t, 4, true)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, testApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := testApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotEmpty(t, utx.OutboundTx, "outbound should be created when destination chain outbound is enabled")
		require.Len(t, utx.OutboundTx, 1)
		require.Equal(t, "eip155:11155111", utx.OutboundTx[0].DestinationChain)
	})
}
