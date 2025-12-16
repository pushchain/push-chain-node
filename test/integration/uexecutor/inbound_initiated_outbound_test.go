package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// This is the SELECTOR for withdraw(address,bytes,address,uint,uint,RevertInstructions)
var withdrawSelector = "0x720b3fbf"

// Hardcoded test event signature of UniversalTxWithdraw
const UniversalTxWithdrawEventSig = "UniversalTxWithdraw"

func setupInboundInitiatedOutboundTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator, common.Address) {
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
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

	tokenConfigTest := uregistrytypes.TokenConfig{
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

	app.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)
	app.UregistryKeeper.AddTokenConfig(ctx, &tokenConfigTest)

	// Register each validator with a universal validator
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		universalValAddr := sdk.AccAddress([]byte(
			fmt.Sprintf("universal-validator-%d", i),
		)).String()

		pubkey := fmt.Sprintf("pubkey-%d", i)
		network := uvalidatortypes.NetworkInfo{Ip: fmt.Sprintf("192.168.0.%d", i+1)}

		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, pubkey, network)
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

		// Define grant for MsgVoteOutbound
		outboundAuth := authz.NewGenericAuthorization(
			sdk.MsgTypeURL(&uexecutortypes.MsgVoteOutbound{}),
		)
		err = app.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, outboundAuth, &exp)
		require.NoError(t, err)
	}

	validUA := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          testAddress,
	}

	ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)
	receipt, err := app.UexecutorKeeper.DeployUEAV2(ctx, ueModuleAccAddress, validUA)
	ueaAddrHex := common.BytesToAddress(receipt.Ret)
	require.NoError(t, err)

	// signature
	validVerificationData := "0x928958fffec8ca9ea8505ed154615be009ecf0818586aed9cd9d6c8b92fcf0e304bdf26b3cdb3317adfc2251bae109ddcf3e4a93deeec137d5ff662ec7ff3c221b"

	validUP := &uexecutortypes.UniversalPayload{
		To:                   utils.GetDefaultAddresses().UniversalGatewayPCAddr.Hex(),
		Value:                "0",
		Data:                 "0x718a35b000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000e0600000000000000000000000000000000000000000000000000000000000f4240000000000000000000000000000000000000000000000000000000000007a1200000000000000000000000001234567890abcdef1234567890abcdef1234567800000000000000000000000000000000000000000000000000000000000000141234567890abcdef1234567890abcdef12345678000000000000000000000000",
		GasLimit:             "21000000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "0",
		Deadline:             "0",
		VType:                uexecutortypes.VerificationType(0),
	}

	inbound := &uexecutortypes.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0xabcd",
		Sender:           testAddress,
		Recipient:        "",
		Amount:           "1000000",
		AssetAddr:        usdcAddress.String(),
		LogIndex:         "1",
		TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
		UniversalPayload: validUP,
		VerificationData: validVerificationData,
	}

	return app, ctx, universalVals, inbound, validators, ueaAddrHex
}

func TestInboundInitiatedOutbound(t *testing.T) {

	t.Run("successfully creates outbound in the UniversalTx when payload invokes Gateway's withdraw fn", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals, _ := setupInboundInitiatedOutboundTest(t, 4)

		// --- Quorum reached ---
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, _, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)

		require.NotEmpty(t, utx.OutboundTx, "OutboundTx should exist after successful withdraw event")
		require.Len(t, utx.OutboundTx, 1, "Only one outbound expected")

		out := utx.OutboundTx[0]

		// Validate outbound params
		require.Equal(t,
			"eip155:11155111",
			out.DestinationChain,
			"Destination chain must be correct",
		)

		require.Equal(t,
			"222",
			out.GasLimit,
			"Gas limit must match event (gasFeeUsed) value",
		)

		// checks
		require.Equal(t, "0x1234567890abcdef1234567890abcdef12345678", out.Recipient)
		require.Equal(t, "1000000", out.Amount)
		require.Equal(t, "0x0000000000000000000000000000000000000e07", out.ExternalAssetAddr)
		require.Equal(t, "0x0000000000000000000000000000000000000e06", out.Prc20AssetAddr)
		require.Equal(t, uexecutortypes.TxType_FUNDS, out.TxType)
		require.Equal(t, uexecutortypes.Status_PENDING, out.OutboundStatus)
	})
}
