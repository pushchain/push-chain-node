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
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

const (
	solanaChainID     = "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"
	solanaUSDCAddr    = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	solanaGatewayAddr = "CFVSincHYbETh2k7w6u1ENEkjbSLtveRCEBupKidw2VS"
)

// setupSolanaInboundTest sets up a test environment with Solana chain and token configs.
func setupSolanaInboundTest(t *testing.T, numVals int, txType uexecutortypes.TxType) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	// Solana chain config
	chainConfig := uregistrytypes.ChainConfig{
		Chain:          solanaChainID,
		VmType:         uregistrytypes.VmType_SVM,
		PublicRpcUrl:   "https://api.devnet.solana.com",
		GatewayAddress: solanaGatewayAddr,
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     0,
			StandardInbound: 1,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{
			{
				Name:             "send_funds",
				Identifier:       "54f7d3283f6a0f3b",
				EventIdentifier:  "6c9ad829b5ea1d7c",
				ConfirmationType: 1,
			},
		},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr

	// Solana USDC token config — use test PRC20 contract that's deployed in test setup
	tokenConfig := uregistrytypes.TokenConfig{
		Chain:        solanaChainID,
		Address:      solanaUSDCAddr,
		Name:         "USDC.sol",
		Symbol:       "USDC.sol",
		Decimals:     6,
		Enabled:      true,
		LiquidityCap: "1000000000000000000000000",
		TokenType:    4, // SPL token type
		NativeRepresentation: &uregistrytypes.NativeRepresentation{
			Denom:           "",
			ContractAddress: prc20Address.String(),
		},
	}

	app.UregistryKeeper.AddChainConfig(ctx, &chainConfig)
	app.UregistryKeeper.AddTokenConfig(ctx, &tokenConfig)

	// Register universal validators
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		universalValAddr := sdk.AccAddress([]byte(
			fmt.Sprintf("universal-validator-%d", i),
		)).String()
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, network)
		require.NoError(t, err)
		universalVals[i] = universalValAddr
	}

	// Grant authz permissions
	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		coreValAddr := sdk.AccAddress(accAddr)
		uniValAddr := sdk.MustAccAddressFromBech32(universalVals[i])
		msgType := sdk.MsgTypeURL(&uexecutortypes.MsgVoteInbound{})
		auth := authz.NewGenericAuthorization(msgType)
		exp := ctx.BlockTime().Add(time.Hour)
		err = app.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, auth, &exp)
		require.NoError(t, err)
	}

	inbound := &uexecutortypes.Inbound{
		SourceChain: solanaChainID,
		TxHash:      "5wHu1qwD7q5xMkZxq6z2S3r4y5N7m8P9kL0jH1gF2dE",
		Sender:      testAddress,
		Recipient:   testAddress,
		Amount:      "1000000",
		AssetAddr:   solanaUSDCAddr,
		LogIndex:    "0",
		TxType:      txType,
		RevertInstructions: &uexecutortypes.RevertInstructions{
			FundRecipient: testAddress,
		},
	}

	return app, ctx, universalVals, inbound, validators
}

// voteToQuorum votes on the inbound from 3 out of 4 validators.
func voteToQuorum(t *testing.T, ctx sdk.Context, app *app.ChainApp, vals []string, coreVals []stakingtypes.Validator, inbound *uexecutortypes.Inbound) {
	t.Helper()
	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()
		err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
		require.NoError(t, err)
	}
}

func TestSolanaInboundFunds(t *testing.T) {
	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	prc20ABI, err := uexecutortypes.ParsePRC20ABI()
	require.NoError(t, err)

	t.Run("quorum reached executes solana FUNDS inbound and mints PRC20", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)
		recipient := common.HexToAddress(inbound.Recipient)

		// Check initial balance is 0
		res, err := app.EVMKeeper.CallEVM(ctx, prc20ABI, ueModuleAccAddress, prc20Address, false, "balanceOf", recipient)
		require.NoError(t, err)
		balances, _ := prc20ABI.Unpack("balanceOf", res.Ret)
		require.Equal(t, int64(0), balances[0].(*big.Int).Int64())

		// Vote to quorum
		voteToQuorum(t, ctx, app, vals, coreVals, inbound)

		// Inbound should no longer be pending
		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending)

		// PRC20 balance should equal inbound amount
		res, err = app.EVMKeeper.CallEVM(ctx, prc20ABI, ueModuleAccAddress, prc20Address, false, "balanceOf", recipient)
		require.NoError(t, err)
		balances, _ = prc20ABI.Unpack("balanceOf", res.Ret)
		expected := new(big.Int)
		expected.SetString(inbound.Amount, 10)
		require.Equal(t, 0, balances[0].(*big.Int).Cmp(expected))
	})

	t.Run("multiple solana FUNDS inbounds accumulate balance", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)
		recipient := common.HexToAddress(inbound.Recipient)

		// First inbound
		voteToQuorum(t, ctx, app, vals, coreVals, inbound)

		// Second inbound with different tx hash
		inbound2 := *inbound
		inbound2.TxHash = "3kHu2qwD7q5xMkZxq6z2S3r4y5N7m8P9kL0jH1gF2dE"
		voteToQuorum(t, ctx, app, vals, coreVals, &inbound2)

		// Balance should be 2x
		res, err := app.EVMKeeper.CallEVM(ctx, prc20ABI, ueModuleAccAddress, prc20Address, false, "balanceOf", recipient)
		require.NoError(t, err)
		balances, _ := prc20ABI.Unpack("balanceOf", res.Ret)
		expected := new(big.Int)
		expected.SetString(inbound.Amount, 10)
		expected.Mul(expected, big.NewInt(2))
		require.Equal(t, 0, balances[0].(*big.Int).Cmp(expected))
	})

	t.Run("solana FUNDS inbound with missing token config records FAILED PCTx", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		// Remove token config to trigger failure
		app.UregistryKeeper.RemoveTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)

		voteToQuorum(t, ctx, app, vals, coreVals, inbound)

		// Fetch UTX and check PCTx status
		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotEmpty(t, utx.PcTx)
		require.Equal(t, "FAILED", utx.PcTx[0].Status)
	})
}

func TestSolanaInboundFundsAndPayload(t *testing.T) {
	t.Run("quorum reached executes solana FUNDS_AND_PAYLOAD with Borsh-decoded payload", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS_AND_PAYLOAD)

		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		// Set payload — ExecVoteInbound will Borsh-encode it into RawPayload
		inbound.UniversalPayload = &uexecutortypes.UniversalPayload{
			To:                   testAddress,
			Value:                "1000000",
			Data:                 "0x",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(0),
		}
		inbound.Recipient = ""

		voteToQuorum(t, ctx, app, vals, coreVals, inbound)

		// Inbound should be executed
		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending)

		// Check UTX was created with decoded payload
		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, utx.InboundTx)
		// RawPayload should be cleared after successful decode
		require.Empty(t, utx.InboundTx.RawPayload, "raw_payload should be cleared after decode")
		// UniversalPayload should be populated from Borsh decode
		require.NotNil(t, utx.InboundTx.UniversalPayload, "universal_payload should be populated from Borsh decode")
	})

	t.Run("solana FUNDS_AND_PAYLOAD with ERC20 transfer calldata", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS_AND_PAYLOAD)

		testAddress := utils.GetDefaultAddresses().DefaultTestAddr
		targetAddr2 := utils.GetDefaultAddresses().TargetAddr2

		// transfer(address,uint256) calldata
		inbound.UniversalPayload = &uexecutortypes.UniversalPayload{
			To:                   testAddress,
			Value:                "0",
			Data:                 "0xa9059cbb000000000000000000000000" + targetAddr2[2:] + "00000000000000000000000000000000000000000000000000000000000f4240",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}
		inbound.Recipient = ""

		voteToQuorum(t, ctx, app, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, utx.InboundTx.UniversalPayload)
		require.Equal(t, "1000000000", utx.InboundTx.UniversalPayload.MaxFeePerGas)
	})
}

func TestSolanaInboundGasAndPayload(t *testing.T) {
	t.Run("quorum reached executes solana GAS_AND_PAYLOAD inbound", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_GAS_AND_PAYLOAD)

		testAddress := utils.GetDefaultAddresses().DefaultTestAddr

		inbound.UniversalPayload = &uexecutortypes.UniversalPayload{
			To:                   testAddress,
			Value:                "0",
			Data:                 "0x",
			GasLimit:             "100000",
			MaxFeePerGas:         "25000000000",
			MaxPriorityFeePerGas: "1000000000",
			Nonce:                "0",
			Deadline:             "0",
			VType:                uexecutortypes.VerificationType(0),
		}
		inbound.Recipient = ""

		voteToQuorum(t, ctx, app, vals, coreVals, inbound)

		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := app.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, utx.InboundTx.UniversalPayload)
		require.Equal(t, "100000", utx.InboundTx.UniversalPayload.GasLimit)
	})
}

func TestSolanaInboundVotingBehavior(t *testing.T) {
	t.Run("less than quorum keeps solana inbound pending", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.True(t, isPending)
	})

	t.Run("duplicate vote on solana inbound fails", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted")
	})

	t.Run("vote after quorum on solana inbound fails", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		voteToQuorum(t, ctx, app, vals, coreVals, inbound)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[3].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[3], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})

	t.Run("different solana inbounds tracked separately", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		inboundB := *inbound
		inboundB.TxHash = "7kHu2qwD7q5xMkZxq6z2S3r4y5N7m8P9kL0jH1gF2dE"

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, &inboundB)
		require.NoError(t, err)
	})

	t.Run("pc_tx hashes are unique across solana inbounds", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupSolanaInboundTest(t, 4, uexecutortypes.TxType_FUNDS)

		for i := 0; i < 3; i++ {
			inboundCopy := *inbound
			inboundCopy.TxHash = fmt.Sprintf("tx%dHu1qwD7q5xMkZxq6z2S3r4y5N7m8P9kL0jH1gF2dE", i)
			voteToQuorum(t, ctx, app, vals, coreVals, &inboundCopy)
		}

		q := uexecutorkeeper.Querier{Keeper: app.UexecutorKeeper}
		resp, err := q.AllUniversalTx(sdk.WrapSDKContext(ctx), &uexecutortypes.QueryAllUniversalTxRequest{})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(resp.UniversalTxs), 3)

		txHashes := make(map[string]bool)
		for _, utx := range resp.UniversalTxs {
			for _, pcTx := range utx.PcTx {
				require.NotEmpty(t, pcTx.TxHash)
				require.Falsef(t, txHashes[pcTx.TxHash], "duplicate pc_tx tx_hash: %s", pcTx.TxHash)
				txHashes[pcTx.TxHash] = true
			}
		}
	})
}
