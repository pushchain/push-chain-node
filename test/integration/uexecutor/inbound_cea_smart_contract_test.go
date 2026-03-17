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

// mockRecipientContractAddr is a fixed address for the minimal mock recipient contract.
// This is a deterministic address used only in tests.
var mockRecipientContractAddr = common.HexToAddress("0x00000000000000000000000000000000000000C3")

// deployMockRecipientContract deploys a minimal smart contract that accepts any call
// without reverting. Bytecode "00" = STOP opcode — succeeds with empty output.
func deployMockRecipientContract(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context) common.Address {
	t.Helper()
	// "00" = STOP opcode: accepts any function call, returns success with empty data
	return utils.DeployContract(t, chainApp, ctx, mockRecipientContractAddr, "00")
}

// setupInboundCEASmartContractTest mirrors setupInboundCEAPayloadTest but deploys
// a mock smart-contract recipient instead of a UEA.
func setupInboundCEASmartContractTest(
	t *testing.T,
	numVals int,
) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator, common.Address) {
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

	// Deploy the mock smart contract recipient
	contractAddr := deployMockRecipientContract(t, chainApp, ctx)

	validUP := &uexecutortypes.UniversalPayload{
		To:                   contractAddr.String(),
		Value:                "1000000",
		Data:                 "0xdeadbeef",
		GasLimit:             "21000000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "1",
		Deadline:             "9999999999",
		VType:                uexecutortypes.VerificationType(1),
	}

	inbound := &uexecutortypes.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0xsc01",
		Sender:           testAddress,
		Recipient:        contractAddr.String(),
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

	return chainApp, ctx, universalVals, inbound, validators, contractAddr
}

func TestInboundCEASmartContractRecipient(t *testing.T) {
	t.Run("quorum reached executes deposit and calls executeUniversalTx on smart contract", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEASmartContractTest(t, 4)

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
		require.True(t, found)

		// Expect 2 PCTxs: deposit + executeUniversalTx
		require.GreaterOrEqual(t, len(utx.PcTx), 2, "should have at least 2 PCTxs: deposit and executeUniversalTx")
	})

	t.Run("deposit PCTx succeeds for smart contract recipient", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEASmartContractTest(t, 4)

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

		depositPcTx := utx.PcTx[0]
		require.Equal(t, "SUCCESS", depositPcTx.Status, "deposit PCTx should succeed for smart contract recipient")
		require.Empty(t, depositPcTx.ErrorMsg)
	})

	t.Run("PRC20 balance is deposited into smart contract", func(t *testing.T) {
		prc20ABI, err := uexecutortypes.ParsePRC20ABI()
		require.NoError(t, err)
		prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr

		chainApp, ctx, vals, inbound, coreVals, contractAddr := setupInboundCEASmartContractTest(t, 4)
		ueModuleAccAddress, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		res, err := chainApp.EVMKeeper.CallEVM(
			ctx,
			prc20ABI,
			ueModuleAccAddress,
			prc20Address,
			false,
			"balanceOf",
			contractAddr,
		)
		require.NoError(t, err)

		balances, err := prc20ABI.Unpack("balanceOf", res.Ret)
		require.NoError(t, err)
		require.Len(t, balances, 1)

		expectedAmount := new(big.Int)
		expectedAmount.SetString(inbound.Amount, 10)
		require.Equal(t, 0, balances[0].(*big.Int).Cmp(expectedAmount),
			"smart contract should have received the deposited PRC20 amount")
	})

	t.Run("no INBOUND_REVERT outbound created for smart contract path", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEASmartContractTest(t, 4)

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
				"no INBOUND_REVERT outbound should be created for the smart contract path")
		}
	})

	t.Run("executeUniversalTx PCTx is recorded for smart contract recipient", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEASmartContractTest(t, 4)

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

		// Second PCTx is the executeUniversalTx call
		require.GreaterOrEqual(t, len(utx.PcTx), 2)
		callPcTx := utx.PcTx[1]
		require.Equal(t, "SUCCESS", callPcTx.Status, "executeUniversalTx PCTx should succeed")
		require.Empty(t, callPcTx.ErrorMsg)
	})

	t.Run("EOA recipient receives deposit and executeUniversalTx call", func(t *testing.T) {
		chainApp, ctx, vals, _, coreVals, _ := setupInboundCEASmartContractTest(t, 4)
		usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		// TargetAddr2 is a plain EOA — deposit lands there and executeUniversalTx is called
		// (calling to an EOA in EVM succeeds with empty output)
		eoaRecipient := utils.GetDefaultAddresses().TargetAddr2

		validUP := &uexecutortypes.UniversalPayload{
			To:                   eoaRecipient,
			Value:                "1000000",
			Data:                 "0xdeadbeef",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}

		eoaInbound := &uexecutortypes.Inbound{
			SourceChain:      "eip155:11155111",
			TxHash:           "0xsc02",
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

			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, eoaInbound)
			require.NoError(t, err)
		}

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*eoaInbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		// Expect 2 PCTxs: deposit + executeUniversalTx
		require.GreaterOrEqual(t, len(utx.PcTx), 2, "should have deposit and executeUniversalTx PCTxs")
		require.Equal(t, "SUCCESS", utx.PcTx[0].Status, "deposit should succeed for EOA recipient")

		// isCEA path never creates a revert outbound
		for _, ob := range utx.OutboundTx {
			require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, ob.TxType,
				"EOA isCEA inbound should NOT create INBOUND_REVERT")
		}
	})
}
