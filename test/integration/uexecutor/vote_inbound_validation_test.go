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

// setupInboundValidationTest sets up the environment for inbound validation failure tests.
// It deploys a UEA and returns the app, context, universal validator addresses, core validators,
// and the UEA address. Callers construct their own inbound with the desired failure condition.
func setupInboundValidationTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, []stakingtypes.Validator, common.Address) {
	t.Helper()

	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

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

	chainApp.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)
	chainApp.UregistryKeeper.AddTokenConfig(ctx, &tokenConfigTest)

	// Register each validator with a universal validator
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		universalValAddr := sdk.AccAddress([]byte(
			fmt.Sprintf("universal-validator-%d", i),
		)).String()

		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}

		err := chainApp.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, network)
		require.NoError(t, err)

		universalVals[i] = universalValAddr
	}

	// Grant authz permission: core validator -> universal validator for MsgVoteInbound
	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)

		coreValAddr := sdk.AccAddress(accAddr)
		uniValAddr := sdk.MustAccAddressFromBech32(universalVals[i])

		msgType := sdk.MsgTypeURL(&uexecutortypes.MsgVoteInbound{})
		auth := authz.NewGenericAuthorization(msgType)
		exp := ctx.BlockTime().Add(time.Hour)

		err = chainApp.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, auth, &exp)
		require.NoError(t, err)
	}

	// Deploy a UEA
	validUA := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          testAddress,
	}

	ueModuleAccAddress, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	receipt, err := chainApp.UexecutorKeeper.DeployUEAV2(ctx, ueModuleAccAddress, validUA)
	require.NoError(t, err)
	ueaAddrHex := common.BytesToAddress(receipt.Ret)

	return chainApp, ctx, universalVals, validators, ueaAddrHex
}

func TestVoteInboundValidation(t *testing.T) {
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr

	t.Run("inbound with invalid payload records failed PCTx", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupInboundValidationTest(t, 4)

		// Construct a FUNDS_AND_PAYLOAD inbound with a malformed UniversalPayload:
		// empty "to" encodes as zero address in RawPayload, passes ValidateForExecution,
		// but fails at EVM execution level. The failure is recorded as a failed PCTx.
		malformedPayload := &uexecutortypes.UniversalPayload{
			To:                   "", // empty "to" address -- fails at EVM execution
			Value:                "0",
			Data:                 "0x",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "0",
			Deadline:             "0",
			VType:                uexecutortypes.VerificationType(0),
		}

		inbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xmalformed01",
			Sender:           testAddress,
			Recipient:        "",
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: malformedPayload,
			VerificationData: "",
		}

		// Reach quorum -- 3 out of 4 validators
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum")

		// Verify PCTx recorded the failure
		require.NotEmpty(t, utx.PcTx, "PcTx should be recorded")
		hasFailed := false
		for _, pcTx := range utx.PcTx {
			if pcTx.Status == "FAILED" {
				hasFailed = true
				break
			}
		}
		require.True(t, hasFailed, "should have a FAILED PCTx for invalid payload")
	})

	t.Run("inbound with empty recipient fails validation for FUNDS type", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupInboundValidationTest(t, 4)

		// FUNDS type requires a non-empty recipient -- validated post-quorum
		inbound := &uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xemptyrecipient01",
			Sender:      testAddress,
			Recipient:   "", // empty recipient -- fails for FUNDS type post-quorum
			Amount:      "1000000",
			AssetAddr:   usdcAddress.String(),
			LogIndex:    "1",
			TxType:      uexecutortypes.TxType_FUNDS,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: testAddress,
			},
		}

		// Reach quorum -- 3 out of 4 validators
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum")

		// Verify PCTx recorded the validation failure
		require.NotEmpty(t, utx.PcTx, "PcTx should be recorded")
		hasFailed := false
		for _, pcTx := range utx.PcTx {
			if pcTx.Status == "FAILED" {
				hasFailed = true
				require.Contains(t, pcTx.ErrorMsg, "recipient cannot be empty")
				break
			}
		}
		require.True(t, hasFailed, "should have a FAILED PCTx for empty recipient on FUNDS type")

		// Non-isCEA failure should create an INBOUND_REVERT outbound
		foundRevert := false
		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				foundRevert = true
				require.Equal(t, inbound.SourceChain, ob.DestinationChain)
				require.Equal(t, inbound.Amount, ob.Amount)
				require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus)
				break
			}
		}
		require.True(t, foundRevert, "INBOUND_REVERT outbound should be created for non-isCEA FUNDS empty recipient failure")
	})

	t.Run("isCEA FUNDS_AND_PAYLOAD inbound deposit failure does NOT create revert outbound", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddrHex := setupInboundValidationTest(t, 4)

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

		// isCEA FUNDS_AND_PAYLOAD inbound -- remove token config to force deposit failure
		inbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xceafunds01",
			Sender:           testAddress,
			Recipient:        ueaAddrHex.String(),
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: testAddress,
			},
		}

		// Remove token config to force the deposit step to fail
		chainApp.UregistryKeeper.RemoveTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)

		// Reach quorum -- 3 out of 4 validators
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum")

		// Verify PCTx recorded the failure
		require.NotEmpty(t, utx.PcTx, "PcTx should be recorded")
		hasFailed := false
		for _, pcTx := range utx.PcTx {
			if pcTx.Status == "FAILED" {
				hasFailed = true
				break
			}
		}
		require.True(t, hasFailed, "at least one PCTx should have FAILED status")

		// isCEA path should NOT create an INBOUND_REVERT outbound
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT outbound should be created for isCEA inbound deposit failure")
		}
	})

	t.Run("non-isCEA FUNDS inbound deposit failure DOES create revert outbound", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupInboundValidationTest(t, 4)

		// non-isCEA FUNDS inbound
		inbound := &uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xrevert01",
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

		// Remove token config to force the deposit step to fail
		chainApp.UregistryKeeper.RemoveTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)

		// Reach quorum
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum")

		// Verify PCTx recorded the failure
		require.NotEmpty(t, utx.PcTx, "PcTx should be recorded")

		// Non-isCEA FUNDS failure should create an INBOUND_REVERT outbound
		foundRevert := false
		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				foundRevert = true
				require.Equal(t, inbound.SourceChain, ob.DestinationChain)
				require.Equal(t, inbound.Amount, ob.Amount)
				require.Equal(t, inbound.AssetAddr, ob.ExternalAssetAddr)
				require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus)
				break
			}
		}
		require.True(t, foundRevert, "INBOUND_REVERT outbound should be created for non-isCEA FUNDS deposit failure")
	})

	t.Run("inbound with invalid hex data in payload fails validation", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupInboundValidationTest(t, 4)

		invalidPayload := &uexecutortypes.UniversalPayload{
			To:                   utils.GetDefaultAddresses().DefaultTestAddr,
			Value:                "0",
			Data:                 "not-valid-hex", // invalid hex data
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "0",
			Deadline:             "0",
			VType:                uexecutortypes.VerificationType(0),
		}

		inbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xinvalidhex01",
			Sender:           testAddress,
			Recipient:        "",
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: invalidPayload,
			VerificationData: "",
		}

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.Error(t, err, "vote with invalid hex data should be rejected")
		require.Contains(t, err.Error(), "invalid data hex")
	})
}
