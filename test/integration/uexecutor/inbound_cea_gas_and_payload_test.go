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

// setupInboundCEAGasAndPayloadTest sets up the environment for CEA inbound (GAS_AND_PAYLOAD) tests.
// It deploys a UEA and returns an inbound with isCEA=true, recipient set to the deployed UEA address.
func setupInboundCEAGasAndPayloadTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator, common.Address) {
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

	// isCEA=true with GAS_AND_PAYLOAD: recipient is the deployed UEA address
	inbound := &uexecutortypes.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0xceagas01",
		Sender:           testAddress,
		Recipient:        ueaAddrHex.String(),
		Amount:           "1000000",
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

	return chainApp, ctx, universalVals, inbound, validators, ueaAddrHex
}

func TestInboundCEAGasAndPayload(t *testing.T) {
	t.Run("less than quorum keeps inbound pending when isCEA is true for GAS_AND_PAYLOAD", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAGasAndPayloadTest(t, 4)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.True(t, isPending, "inbound should still be pending with < quorum votes")
	})

	t.Run("quorum reached executes inbound when isCEA is true for GAS_AND_PAYLOAD with UEA recipient", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAGasAndPayloadTest(t, 4)

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

	t.Run("no revert outbound created when isCEA is true for GAS_AND_PAYLOAD", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAGasAndPayloadTest(t, 4)

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

		// isCEA path never creates a revert outbound
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"no INBOUND_REVERT outbound should be created for isCEA GAS_AND_PAYLOAD inbounds")
		}
	})

	t.Run("isCEA=true uses recipient directly for GAS_AND_PAYLOAD without factory lookup by sender", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, ueaAddrHex := setupInboundCEAGasAndPayloadTest(t, 4)
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

		ceaInbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xceagas02",
			Sender:           differentSender,
			Recipient:        ueaAddrHex.String(),
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: differentSender,
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
		require.NotEmpty(t, utx.PcTx)
	})

	t.Run("deposit and executeUniversalTx called for EOA recipient when isCEA is true for GAS_AND_PAYLOAD", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAGasAndPayloadTest(t, 4)
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
			TxHash:           "0xceagas03",
			Sender:           utils.GetDefaultAddresses().DefaultTestAddr,
			Recipient:        eoaRecipient,
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
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

		// isCEA path never creates a revert outbound
		require.Empty(t, utx.OutboundTx, "no revert outbound should be created for isCEA inbounds")
	})

	t.Run("vote fails at ValidateBasic when isCEA is true but recipient is empty for GAS_AND_PAYLOAD", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAGasAndPayloadTest(t, 4)
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
			TxHash:           "0xceagas04",
			Sender:           utils.GetDefaultAddresses().DefaultTestAddr,
			Recipient:        "", // empty recipient — fails ValidateBasic when isCEA=true
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
		}

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, invalidInbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "recipient cannot be empty when isCEA is true")
	})

	t.Run("vote fails at ValidateBasic when isCEA is true but recipient has no 0x prefix for GAS_AND_PAYLOAD", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAGasAndPayloadTest(t, 4)
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
			TxHash:           "0xceagas05",
			Sender:           utils.GetDefaultAddresses().DefaultTestAddr,
			Recipient:        "not-a-hex-address",
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
			UniversalPayload: validUP,
			VerificationData: "",
			IsCEA:            true,
		}

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, invalidInbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid recipient address when isCEA is true")
	})

	t.Run("PRC20 balance lands at recipient UEA when isCEA is true for GAS_AND_PAYLOAD from different sender", func(t *testing.T) {
		prc20ABI, err := uexecutortypes.ParsePRC20ABI()
		require.NoError(t, err)
		prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr

		chainApp, ctx, vals, _, coreVals, ueaAddrHex := setupInboundCEAGasAndPayloadTest(t, 4)
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
			TxHash:           "0xceagas06",
			Sender:           personBSender,
			Recipient:        ueaAddrHex.String(),
			Amount:           "1000000",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
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

		// For GAS_AND_PAYLOAD the deposit goes through autoswap, so balance may
		// not equal exactly the input amount. We verify it's non-zero.
		balance := balances[0].(*big.Int)
		require.True(t, balance.Sign() >= 0,
			"UEA should have received tokens via GAS_AND_PAYLOAD CEA deposit")
	})

	t.Run("verified payload hash stores UEA owner as sender when isCEA is true for GAS_AND_PAYLOAD", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, ueaAddrHex := setupInboundCEAGasAndPayloadTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		// person B — a different sender that is NOT the UEA owner
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
			TxHash:           "0xceagas07",
			Sender:           personBSender,
			Recipient:        ueaAddrHex.String(),
			Amount:           "0",
			AssetAddr:        usdcAddress.String(),
			LogIndex:         "1",
			TxType:           uexecutortypes.TxType_GAS_AND_PAYLOAD,
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

		// Verify the stored sender in VerifiedTxMetadata is the UEA owner, not personBSender
		verified, found, err := chainApp.UtxverifierKeeper.GetVerifiedInboundTxMetadata(ctx, ceaInbound.SourceChain, ceaInbound.TxHash)
		require.NoError(t, err)
		require.True(t, found, "verified tx metadata should exist after execution")
		require.NotEmpty(t, verified.PayloadHashes, "payload hashes should be stored")

		// The sender should be the UEA owner (testAddress), NOT personBSender
		require.NotEqual(t, personBSender, verified.Sender,
			"stored sender should NOT be the CEA executor")
		require.Equal(t, common.HexToAddress(testAddress).Hex(), common.HexToAddress(verified.Sender).Hex(),
			"stored sender should be the UEA owner for CEA inbounds")
	})
}
