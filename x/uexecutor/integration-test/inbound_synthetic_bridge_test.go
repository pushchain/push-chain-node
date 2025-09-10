package integrationtest

import (
	"fmt"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/testutils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func setupInboundBridgeTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound) {
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
	// --- add universal validators ---
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		universalValAddr := sdk.AccAddress([]byte(
			fmt.Sprintf("universal-validator-%d", i),
		)).String()

		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, universalValAddr)
		require.NoError(t, err)

		universalVals[i] = universalValAddr
	}

	// --- build a base inbound ---
	inbound := &uexecutortypes.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0xabcd",
		Sender:           testAddress,
		Recipient:        testAddress,
		Amount:           "1000000",
		AssetAddr:        prc20Address.String(),
		LogIndex:         "1",
		TxType:           uexecutortypes.InboundTxType_FUNDS_BRIDGE_TX,
		UniversalPayload: nil,
		VerificationData: "",
	}

	return app, ctx, universalVals, inbound
}

func TestInboundSyntheticBridge(t *testing.T) {
	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	// Parse PRC20 ABI
	prc20ABI, err := uexecutortypes.ParsePRC20ABI()
	require.NoError(t, err)

	t.Run("less than quorum votes keeps inbound pending", func(t *testing.T) {
		app, ctx, vals, inbound := setupInboundBridgeTest(t, 4)

		// 1 vote out of 4
		err := app.UexecutorKeeper.VoteInbound(ctx, vals[0], *inbound)
		require.NoError(t, err)

		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.True(t, isPending, "inbound should still be pending with < quorum votes")
	})

	t.Run("quorum reached executes inbound and prc20 gets minted successfully", func(t *testing.T) {
		app, ctx, vals, inbound := setupInboundBridgeTest(t, 4)

		ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)

		recipient := common.HexToAddress(inbound.Recipient)
		fmt.Println(recipient)

		// Cast amount to *big.Int
		amount := new(big.Int)
		amount, ok := amount.SetString(inbound.Amount, 10)
		require.True(t, ok)

		// 3 votes out of 4 (>= 66%)
		for i := 0; i < 3; i++ {
			err := app.UexecutorKeeper.VoteInbound(ctx, vals[i], *inbound)
			require.NoError(t, err)
		}

		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should be executed after reaching quorum")

		// --- Query PRC20 balanceOf(recipient) ---
		res, err := app.EVMKeeper.CallEVM(
			ctx,
			prc20ABI,
			ueModuleAccAddress, // "from" (doesn't matter for view)
			prc20Address,       // contract address
			false,              // commit = false (read-only)
			"balanceOf",
			recipient,
		)
		require.NoError(t, err)

		// Decode return data
		balances, err := prc20ABI.Unpack("balanceOf", res.Ret)
		require.NoError(t, err)

		require.Len(t, balances, 1)
		balance := balances[0].(*big.Int)

		require.Equal(t, 0, balance.Cmp(amount), "recipient balance should equal inbound amount")
	})

	t.Run("vote after quorum fails", func(t *testing.T) {
		app, ctx, vals, inbound := setupInboundBridgeTest(t, 4)

		// reach quorum
		for i := 0; i < 3; i++ {
			require.NoError(t, app.UexecutorKeeper.VoteInbound(ctx, vals[i], *inbound))
		}

		// 4th vote should fail
		err := app.UexecutorKeeper.VoteInbound(ctx, vals[3], *inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists", "should fail because inbound is already executed")
	})

	t.Run("duplicate vote fails", func(t *testing.T) {
		app, ctx, vals, inbound := setupInboundBridgeTest(t, 4)

		require.NoError(t, app.UexecutorKeeper.VoteInbound(ctx, vals[0], *inbound))

		err := app.UexecutorKeeper.VoteInbound(ctx, vals[0], *inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted", "should fail on duplicate vote from same validator")
	})

	t.Run("different inbounds tracked separately", func(t *testing.T) {
		app, ctx, vals, inbound := setupInboundBridgeTest(t, 4)

		inboundB := *inbound
		inboundB.TxHash = "0xabce"

		err := app.UexecutorKeeper.VoteInbound(ctx, vals[0], *inbound)
		require.NoError(t, err)

		err = app.UexecutorKeeper.VoteInbound(ctx, vals[0], inboundB)
		require.NoError(t, err, "votes for different inbounds should be tracked independently")
	})

	t.Run("invalid validator fails", func(t *testing.T) {
		app, ctx, _, inbound := setupInboundBridgeTest(t, 4)

		err := app.UexecutorKeeper.VoteInbound(ctx, "invalid-val", *inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not eligible", "should fail because validator is not registered")
	})

	t.Run("balance is zero before execution and updated after", func(t *testing.T) {
		app, ctx, vals, inbound := setupInboundBridgeTest(t, 4)
		ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)
		recipient := common.HexToAddress(inbound.Recipient)

		// check initial balance == 0
		res, err := app.EVMKeeper.CallEVM(ctx, prc20ABI, ueModuleAccAddress, prc20Address, false, "balanceOf", recipient)
		require.NoError(t, err)
		balances, _ := prc20ABI.Unpack("balanceOf", res.Ret)
		balance := balances[0].(*big.Int)
		require.Equal(t, int64(0), balance.Int64(), "initial balance should be zero")

		// reach quorum
		for i := 0; i < 3; i++ {
			require.NoError(t, app.UexecutorKeeper.VoteInbound(ctx, vals[i], *inbound))
		}

		// balance should equal inbound amount
		res, err = app.EVMKeeper.CallEVM(ctx, prc20ABI, ueModuleAccAddress, prc20Address, false, "balanceOf", recipient)
		require.NoError(t, err)
		balances, _ = prc20ABI.Unpack("balanceOf", res.Ret)
		expected := new(big.Int)
		expected.SetString(inbound.Amount, 10)

		require.Equal(t, 0, balances[0].(*big.Int).Cmp(expected))
	})

	t.Run("multiple inbounds accumulate balances", func(t *testing.T) {
		app, ctx, vals, inbound := setupInboundBridgeTest(t, 4)
		ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)
		recipient := common.HexToAddress(inbound.Recipient)

		// First inbound
		for i := 0; i < 3; i++ {
			require.NoError(t, app.UexecutorKeeper.VoteInbound(ctx, vals[i], *inbound))
		}

		// Second inbound with different TxHash
		inboundB := *inbound
		inboundB.TxHash = "0xabcf"
		for i := 0; i < 3; i++ {
			require.NoError(t, app.UexecutorKeeper.VoteInbound(ctx, vals[i], inboundB))
		}

		// balance should equal 2 * inbound.Amount
		res, err := app.EVMKeeper.CallEVM(ctx, prc20ABI, ueModuleAccAddress, prc20Address, false, "balanceOf", recipient)
		require.NoError(t, err)
		balances, _ := prc20ABI.Unpack("balanceOf", res.Ret)

		expected := new(big.Int)
		expected.SetString(inbound.Amount, 10)
		expected.Mul(expected, big.NewInt(2))

		require.Equal(t, 0, balances[0].(*big.Int).Cmp(expected))
	})
}
