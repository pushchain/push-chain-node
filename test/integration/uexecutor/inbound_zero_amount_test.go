package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

// setupZeroAmountInboundTest sets up the environment for zero-amount inbound tests.
func setupZeroAmountInboundTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, []stakingtypes.Validator, common.Address) {
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

func TestInboundZeroAmountFundsAndPayload(t *testing.T) {
	t.Run("zero amount FUNDS_AND_PAYLOAD skips deposit and executes payload", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddrHex := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		validUP := &uexecutortypes.UniversalPayload{
			To:                   ueaAddrHex.String(),
			Value:                "0",
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
			TxHash:           "0xzeroamt01",
			Sender:           testAddress,
			Recipient:        "",
			Amount:           "0",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after quorum")

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum")

		// No deposit PCTx should be recorded (deposit was skipped)
		// Only payload-related PCTxs should exist
		require.NotEmpty(t, utx.PcTx, "PcTx should not be empty — payload execution should be recorded")

		// No INBOUND_REVERT should be created (deposit was skipped, not failed)
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT should be created for zero-amount FUNDS_AND_PAYLOAD")
		}
	})

	t.Run("zero amount FUNDS_AND_PAYLOAD with isCEA=true skips deposit and executes payload via UEA", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddrHex := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		validUP := &uexecutortypes.UniversalPayload{
			To:                   ueaAddrHex.String(),
			Value:                "0",
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
			TxHash:           "0xzeroamt02",
			Sender:           testAddress,
			Recipient:        ueaAddrHex.String(),
			Amount:           "0",
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

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after quorum")

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum")

		// No INBOUND_REVERT should be created
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT should be created for zero-amount isCEA FUNDS_AND_PAYLOAD")
		}
	})

	t.Run("zero amount FUNDS_AND_PAYLOAD auto-deploys UEA when not deployed", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

		// TargetAddr2 has no pre-deployed UEA — the handler must auto-deploy it
		newSender := utils.GetDefaultAddresses().TargetAddr2

		validUP := &uexecutortypes.UniversalPayload{
			To:                   newSender,
			Value:                "0",
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
			TxHash:           "0xzeroamt03",
			Sender:           newSender,
			Recipient:        "",
			Amount:           "0",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after quorum")

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist")

		// Verify a deploy PCTx was recorded with SUCCESS
		require.NotEmpty(t, utx.PcTx, "PcTx should not be empty")
		deployPcTx := utx.PcTx[0]
		require.Equal(t, "SUCCESS", deployPcTx.Status, "UEA deploy PCTx should succeed even with zero amount")
	})
}

func TestInboundZeroAmountGasAndPayload(t *testing.T) {
	t.Run("zero amount GAS_AND_PAYLOAD skips deposit and executes payload", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddrHex := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		validUP := &uexecutortypes.UniversalPayload{
			To:                   ueaAddrHex.String(),
			Value:                "0",
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
			TxHash:           "0xzerogas01",
			Sender:           testAddress,
			Recipient:        "",
			Amount:           "0",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after quorum")

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum")

		// Payload execution should have occurred (PCTxs recorded)
		require.NotEmpty(t, utx.PcTx, "PcTx should not be empty — payload execution should be recorded")

		// No INBOUND_REVERT should be created
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT should be created for zero-amount GAS_AND_PAYLOAD")
		}
	})

	t.Run("zero amount GAS_AND_PAYLOAD with isCEA=true skips deposit and processes payload", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddrHex := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		validUP := &uexecutortypes.UniversalPayload{
			To:                   ueaAddrHex.String(),
			Value:                "0",
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
			TxHash:           "0xzerogas02",
			Sender:           testAddress,
			Recipient:        ueaAddrHex.String(),
			Amount:           "0",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: testAddress,
			},
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after quorum")

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist")

		// No INBOUND_REVERT should be created
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT should be created for zero-amount isCEA GAS_AND_PAYLOAD")
		}
	})

	t.Run("zero amount GAS_AND_PAYLOAD auto-deploys UEA when not deployed", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

		newSender := utils.GetDefaultAddresses().TargetAddr2

		validUP := &uexecutortypes.UniversalPayload{
			To:                   newSender,
			Value:                "0",
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
			TxHash:           "0xzerogas03",
			Sender:           newSender,
			Recipient:        "",
			Amount:           "0",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after quorum")

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist")

		// Verify a deploy PCTx was recorded with SUCCESS
		require.NotEmpty(t, utx.PcTx, "PcTx should not be empty")
		deployPcTx := utx.PcTx[0]
		require.Equal(t, "SUCCESS", deployPcTx.Status, "UEA deploy PCTx should succeed even with zero amount")
	})

	t.Run("zero amount for FUNDS type: vote succeeds, UTX has failed PCTx with revert outbound", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		inbound := &uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xzerorej01",
			Sender:      testAddress,
			Recipient:   testAddress,
			Amount:      "0",
			AssetAddr:   usdcAddress.String(),
			LogIndex:    "1",
			TxType:      uexecutortypes.TxType_FUNDS,
		}

		// Vote from all validators to finalize the ballot
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err, "vote should succeed — validation failure is recorded on UTX, not as a vote error")
		}

		// UTX should exist with a failed PCTx
		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should be created even when execution validation fails")
		require.NotEmpty(t, utx.PcTx, "UTX should have a failed PCTx")
		require.Equal(t, "FAILED", utx.PcTx[0].Status)
		require.Contains(t, utx.PcTx[0].ErrorMsg, "amount must be positive for this tx type")
		// Non-isCEA should also have a revert outbound
		require.NotEmpty(t, utx.OutboundTx, "UTX should have a revert outbound for non-isCEA")
		require.Equal(t, uexecutortypes.TxType_INBOUND_REVERT, utx.OutboundTx[0].TxType)
	})

	t.Run("zero amount for GAS type: vote succeeds, UTX has failed PCTx with revert outbound", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupZeroAmountInboundTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		inbound := &uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xzerorej02",
			Sender:      testAddress,
			Recipient:   testAddress,
			Amount:      "0",
			AssetAddr:   usdcAddress.String(),
			LogIndex:    "1",
			TxType:      uexecutortypes.TxType_GAS,
		}

		// Vote from all validators to finalize the ballot
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err, "vote should succeed — validation failure is recorded on UTX, not as a vote error")
		}

		// UTX should exist with a failed PCTx
		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should be created even when execution validation fails")
		require.NotEmpty(t, utx.PcTx, "UTX should have a failed PCTx")
		require.Equal(t, "FAILED", utx.PcTx[0].Status)
		require.Contains(t, utx.PcTx[0].ErrorMsg, "amount must be positive for this tx type")
		// Non-isCEA should also have a revert outbound
		require.NotEmpty(t, utx.OutboundTx, "UTX should have a revert outbound for non-isCEA")
		require.Equal(t, uexecutortypes.TxType_INBOUND_REVERT, utx.OutboundTx[0].TxType)
	})
}
