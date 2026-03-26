package integrationtest

import (
	"fmt"
	"math/big"
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

// setupInboundCEAPayloadTest sets up the environment for CEA inbound (FUNDS_AND_PAYLOAD) tests.
// It deploys a UEA and returns an inbound with isCEA=true, recipient set to the deployed UEA address.
func setupInboundCEAPayloadTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator, common.Address) {
	t.Helper()

	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:        "eip155:11155111",
		VmType:       uregistrytypes.VmType_EVM,
		PublicRpcUrl: "https://sepolia.drpc.org",
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

	// Deploy a UEA so we have a valid UEA address to use as recipient
	validUA := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          testAddress,
	}

	ueModuleAccAddress, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	receipt, err := chainApp.UexecutorKeeper.DeployUEAV2(ctx, ueModuleAccAddress, validUA)
	require.NoError(t, err)
	ueaAddrHex := common.BytesToAddress(receipt.Ret)

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

	// isCEA=true: recipient is the deployed UEA address
	inbound := &uexecutortypes.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0xcea01",
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

	return chainApp, ctx, universalVals, inbound, validators, ueaAddrHex
}

func TestInboundCEAFundsAndPayload(t *testing.T) {
	t.Run("less than quorum keeps inbound pending when isCEA is true", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.True(t, isPending, "inbound should still be pending with < quorum votes")
	})

	t.Run("quorum reached executes inbound when isCEA is true and recipient is valid UEA", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

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
		require.True(t, found, "universal tx should exist after quorum is reached")
		require.NotEmpty(t, utx.PcTx, "PcTx entries should be recorded")
	})

	t.Run("deposit pc_tx succeeds when isCEA is true and recipient is a deployed UEA", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

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
		require.True(t, found)
		require.GreaterOrEqual(t, len(utx.PcTx), 1)

		// First PcTx is the deposit step — it should succeed
		depositPcTx := utx.PcTx[0]
		require.Equal(t, "SUCCESS", depositPcTx.Status, "deposit should succeed when isCEA=true and recipient is a valid UEA")
		require.Empty(t, depositPcTx.ErrorMsg)
	})

	t.Run("PRC20 balance is minted into UEA when isCEA is true", func(t *testing.T) {
		prc20ABI, err := uexecutortypes.ParsePRC20ABI()
		require.NoError(t, err)
		prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr

		chainApp, ctx, vals, inbound, coreVals, ueaAddrHex := setupInboundCEAPayloadTest(t, 4)
		ueModuleAccAddress, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		// Check that PRC20 was deposited into the UEA (recipient)
		res, err := chainApp.EVMKeeper.CallEVM(
			ctx,
			prc20ABI,
			ueModuleAccAddress,
			prc20Address,
			false,
			"balanceOf",
			ueaAddrHex,
		)
		require.NoError(t, err)

		balances, err := prc20ABI.Unpack("balanceOf", res.Ret)
		require.NoError(t, err)
		require.Len(t, balances, 1)

		expectedAmount := new(big.Int)
		expectedAmount.SetString(inbound.Amount, 10)
		require.Equal(t, 0, balances[0].(*big.Int).Cmp(expectedAmount),
			"UEA should have received the deposited PRC20 amount")
	})

	t.Run("deposit and executeUniversalTx called for EOA recipient when isCEA is true", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

		// Use a valid hex address that is a plain EOA (no code deployed)
		eoaRecipient := utils.GetDefaultAddresses().TargetAddr2

		validUP := &uexecutortypes.UniversalPayload{
			To:                   eoaRecipient,
			Value:                "1000000",
			Data:                 "0xa9059cbb000000000000000000000000527f3692f5c53cfa83f7689885995606f93b616400000000000000000000000000000000000000000000000000000000000f4240",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}

		inboundWithEOA := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xcea02",
			Sender:           utils.GetDefaultAddresses().DefaultTestAddr,
			Recipient:        eoaRecipient,
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: utils.GetDefaultAddresses().DefaultTestAddr,
			},
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inboundWithEOA)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inboundWithEOA)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist after quorum is reached")

		// Expect 2 PCTxs: deposit + executeUniversalTx (calling to EOA succeeds in EVM)
		require.GreaterOrEqual(t, len(utx.PcTx), 2, "should have deposit and executeUniversalTx PCTxs")
		require.Equal(t, "SUCCESS", utx.PcTx[0].Status, "deposit should succeed for EOA recipient")
		// isCEA path never creates a revert outbound
		require.Empty(t, utx.OutboundTx, "no revert outbound should be created for isCEA inbounds")
	})

	t.Run("no revert outbound created when isCEA is true regardless of recipient type", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		// Plain EOA recipient — deposit + executeUniversalTx called, no INBOUND_REVERT
		eoaRecipient := utils.GetDefaultAddresses().TargetAddr2

		validUP := &uexecutortypes.UniversalPayload{
			To:                   eoaRecipient,
			Value:                "1000000",
			Data:                 "0xa9059cbb000000000000000000000000527f3692f5c53cfa83f7689885995606f93b616400000000000000000000000000000000000000000000000000000000000f4240",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}

		inboundWithEOA := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xcea03",
			Sender:           testAddress,
			Recipient:        eoaRecipient,
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

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inboundWithEOA)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inboundWithEOA)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		// isCEA path never creates a revert outbound
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT outbound should be created for isCEA inbounds")
		}
	})

	t.Run("vote succeeds but UTX has failed PCTx when isCEA is true but recipient has no 0x prefix", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

		validUP := &uexecutortypes.UniversalPayload{
			To:                   utils.GetDefaultAddresses().DefaultTestAddr,
			Value:                "1000000",
			Data:                 "0x",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}

		invalidInbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xcea04",
			Sender:           utils.GetDefaultAddresses().DefaultTestAddr,
			Recipient:        "not-a-hex-address", // no 0x prefix — caught by ValidateForExecution
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
		}

		// Vote from all validators to finalize the ballot
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, invalidInbound)
			require.NoError(t, err, "vote should succeed — validation failure is recorded on UTX, not as a vote error")
		}

		// UTX should exist with a failed PCTx
		utxKey := uexecutortypes.GetInboundUniversalTxKey(*invalidInbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should be created even when execution validation fails")
		require.NotEmpty(t, utx.PcTx, "UTX should have a failed PCTx")
		require.Equal(t, "FAILED", utx.PcTx[0].Status)
		require.Contains(t, utx.PcTx[0].ErrorMsg, "invalid recipient address when isCEA is true")
		// isCEA failures should NOT create an INBOUND_REVERT outbound
		require.Empty(t, utx.OutboundTx, "isCEA failures should not create a revert outbound")
	})

	t.Run("vote succeeds but UTX has failed PCTx when isCEA is true but recipient is empty", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

		validUP := &uexecutortypes.UniversalPayload{
			To:                   utils.GetDefaultAddresses().DefaultTestAddr,
			Value:                "1000000",
			Data:                 "0x",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}

		invalidInbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xcea05",
			Sender:           utils.GetDefaultAddresses().DefaultTestAddr,
			Recipient:        "", // empty recipient — caught by ValidateForExecution
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
		}

		// Vote from all validators to finalize the ballot
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, invalidInbound)
			require.NoError(t, err, "vote should succeed — validation failure is recorded on UTX, not as a vote error")
		}

		// UTX should exist with a failed PCTx
		utxKey := uexecutortypes.GetInboundUniversalTxKey(*invalidInbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should be created even when execution validation fails")
		require.NotEmpty(t, utx.PcTx, "UTX should have a failed PCTx")
		require.Equal(t, "FAILED", utx.PcTx[0].Status)
		require.Contains(t, utx.PcTx[0].ErrorMsg, "recipient cannot be empty when isCEA is true")
		// isCEA failures should NOT create an INBOUND_REVERT outbound
		require.Empty(t, utx.OutboundTx, "isCEA failures should not create a revert outbound")
	})

	t.Run("vote after quorum fails when isCEA is true", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		valAddr, err := sdk.ValAddressFromBech32(coreVals[3].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[3], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})

	t.Run("duplicate vote fails when isCEA is true", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted")
	})

	t.Run("isCEA and non-CEA inbounds from same sender are tracked independently", func(t *testing.T) {
		chainApp, ctx, vals, ceaInbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		// A FUNDS inbound with isCEA=false from the same sender uses the standard path
		nonCEAInbound := &uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0xnonce01",
			Sender:      testAddress,
			Recipient:   testAddress,
			Amount:      "500000",
			AssetAddr:   usdcAddress.String(),
			LogIndex:    "2",
			TxType:      uexecutortypes.TxType_FUNDS,
			IsCEA:       false,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: testAddress,
			},
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, ceaInbound)
			require.NoError(t, err)

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, nonCEAInbound)
			require.NoError(t, err)
		}

		isPendingCEA, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *ceaInbound)
		require.NoError(t, err)
		require.False(t, isPendingCEA, "CEA inbound should be finalised")

		isPendingNonCEA, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *nonCEAInbound)
		require.NoError(t, err)
		require.False(t, isPendingNonCEA, "non-CEA inbound should be finalised independently")
	})

	t.Run("no revert outbound is created when isCEA is true and execution succeeds", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

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
		require.True(t, found)

		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT outbound should be created on successful CEA execution")
		}
	})

	t.Run("PRC20 balance lands at explicitly passed recipient even when recipient is not the sender's UEA", func(t *testing.T) {
		// Setup deploys a UEA for testAddress (person A).
		// This test sends an inbound whose Sender is TargetAddr2 (person B, no UEA deployed).
		// Recipient is person A's UEA — a UEA that has no relation to person B.
		// After execution the PRC20 balance must be at the recipient (person A's UEA), proving
		// that CEA routing is driven purely by the explicit recipient field, not by the sender's identity.
		prc20ABI, err := uexecutortypes.ParsePRC20ABI()
		require.NoError(t, err)
		prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr

		chainApp, ctx, vals, _, coreVals, ueaAddrHex := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		ueModuleAccAddress, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)

		// person B — a sender that has no deployed UEA
		personBSender := utils.GetDefaultAddresses().TargetAddr2

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

		ceaInbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xcea07",
			Sender:           personBSender, // person B — no UEA
			Recipient:        ueaAddrHex.String(), // person A's UEA
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: personBSender,
			},
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, ceaInbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*ceaInbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		require.GreaterOrEqual(t, len(utx.PcTx), 1)

		depositPcTx := utx.PcTx[0]
		require.Equal(t, "SUCCESS", depositPcTx.Status,
			"deposit should succeed: recipient is a valid UEA regardless of sender identity")

		// Confirm the PRC20 balance landed at the explicitly passed recipient (person A's UEA)
		res, err := chainApp.EVMKeeper.CallEVM(
			ctx,
			prc20ABI,
			ueModuleAccAddress,
			prc20Address,
			false,
			"balanceOf",
			ueaAddrHex,
		)
		require.NoError(t, err)

		balances, err := prc20ABI.Unpack("balanceOf", res.Ret)
		require.NoError(t, err)
		require.Len(t, balances, 1)

		expectedAmount := new(big.Int)
		expectedAmount.SetString(ceaInbound.Amount, 10)
		require.Equal(t, 0, balances[0].(*big.Int).Cmp(expectedAmount),
			"PRC20 balance must be at the explicitly passed recipient (person A's UEA), not at sender's address")
	})

	t.Run("isCEA=true uses recipient directly without factory lookup by sender universalAccountId", func(t *testing.T) {
		// The key difference: isCEA=true does NOT look up UEA via sender's UniversalAccountId.
		// Instead it directly validates and uses the explicit recipient address.
		// We demonstrate this by setting Sender to an address that has no deployed UEA —
		// with isCEA=false this would fail, but with isCEA=true it should succeed
		// because the valid UEA is already specified in Recipient.
		chainApp, ctx, vals, ceaInbound, coreVals, ueaAddrHex := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

		// A different sender that has no deployed UEA
		differentSender := utils.GetDefaultAddresses().TargetAddr2

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

		// isCEA=true: sender has no UEA, but recipient is a valid deployed UEA — should succeed
		ceaInboundDifferentSender := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xcea06",
			Sender:           differentSender,
			Recipient:        ueaAddrHex.String(), // valid UEA
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: differentSender,
			},
		}

		_ = ceaInbound // setup already deployed the UEA; we only use ueaAddrHex

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, ceaInboundDifferentSender)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*ceaInboundDifferentSender)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		require.GreaterOrEqual(t, len(utx.PcTx), 1)

		// Deposit should succeed because recipient is a valid UEA regardless of sender
		depositPcTx := utx.PcTx[0]
		require.Equal(t, "SUCCESS", depositPcTx.Status,
			"isCEA=true should succeed using recipient UEA directly, ignoring whether sender has a UEA")
	})

	t.Run("verified payload hash stored under UEA origin chain with UEA owner as sender for CEA FUNDS_AND_PAYLOAD", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, ueaAddrHex := setupInboundCEAPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		// Register a second chain (eip155:97) so we can send a CEA inbound from a different chain
		chainApp.UregistryKeeper.AddChainConfig(ctx, &uregistrytypes.ChainConfig{
			Chain:          "eip155:97",
			VmType:         uregistrytypes.VmType_EVM,
			PublicRpcUrl:    "https://data-seed-prebsc-1-s1.binance.org:8545",
			GatewayAddress: "0x0000000000000000000000000000000000000000",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				FastInbound:     5,
				StandardInbound: 12,
			},
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		})
		chainApp.UregistryKeeper.AddTokenConfig(ctx, &uregistrytypes.TokenConfig{
			Chain:        "eip155:97",
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

		// person B — a different sender on a different chain
		personBSender := utils.GetDefaultAddresses().TargetAddr2

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

		// CEA inbound from eip155:97, but UEA origin is eip155:11155111
		ceaInbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:97",
			TxHash:           "0xcea07",
			Sender:           personBSender,
			Recipient:        ueaAddrHex.String(),
			Amount:           "0",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: personBSender,
			},
		}

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, ceaInbound)
			require.NoError(t, err)
		}

		// Payload hash should be stored under UEA origin chain (eip155:11155111), NOT source chain (eip155:97)
		ueaOriginChain := "eip155:11155111"
		verified, found, err := chainApp.UtxverifierKeeper.GetVerifiedInboundTxMetadata(ctx, ueaOriginChain, ceaInbound.TxHash)
		require.NoError(t, err)
		require.True(t, found, "verified tx metadata should be stored under UEA origin chain")
		require.NotEmpty(t, verified.PayloadHashes, "payload hashes should be stored")

		// Should NOT be found under source chain
		_, foundUnderSource, err := chainApp.UtxverifierKeeper.GetVerifiedInboundTxMetadata(ctx, ceaInbound.SourceChain, ceaInbound.TxHash)
		require.NoError(t, err)
		require.False(t, foundUnderSource, "verified tx metadata should NOT be stored under inbound source chain for CEA")

		// The sender should be the UEA owner (testAddress), NOT personBSender
		require.NotEqual(t, personBSender, verified.Sender,
			"stored sender should NOT be the CEA executor")
		require.Equal(t, common.HexToAddress(testAddress).Hex(), common.HexToAddress(verified.Sender).Hex(),
			"stored sender should be the UEA owner for CEA inbounds")
	})
}
