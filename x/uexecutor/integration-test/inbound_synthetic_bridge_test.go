package integrationtest

import (
	"fmt"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/testutils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"

	"time"

	authz "github.com/cosmos/cosmos-sdk/x/authz"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func setupInboundBridgeTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator) {
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

	app.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)
	app.UregistryKeeper.AddTokenConfig(ctx, &tokenConfigTest)

	// Register each validator with a universal validator
	// --- add universal validators ---
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

	// --- build a base inbound ---
	inbound := &uexecutortypes.Inbound{
		SourceChain:      "eip155:11155111",
		TxHash:           "0xabcd",
		Sender:           testAddress,
		Recipient:        testAddress,
		Amount:           "1000000",
		AssetAddr:        prc20Address.String(),
		LogIndex:         "1",
		TxType:           uexecutortypes.InboundTxType_FUNDS,
		UniversalPayload: nil,
		VerificationData: "",
	}

	return app, ctx, universalVals, inbound, validators
}

func TestInboundSyntheticBridge(t *testing.T) {
	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	// Parse PRC20 ABI
	prc20ABI, err := uexecutortypes.ParsePRC20ABI()
	require.NoError(t, err)

	t.Run("less than quorum votes keeps inbound pending", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		// 1 vote out of 4
		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		isPending, err := app.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.True(t, isPending, "inbound should still be pending with < quorum votes")
	})

	t.Run("quorum reached executes inbound and prc20 gets minted successfully", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)

		recipient := common.HexToAddress(inbound.Recipient)
		fmt.Println(recipient)

		// Cast amount to *big.Int
		amount := new(big.Int)
		amount, ok := amount.SetString(inbound.Amount, 10)
		require.True(t, ok)

		// 3 votes out of 4 (>= 66%)
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
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
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		// reach quorum
		for i := 0; i < 3; i++ {

			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		// 4th vote should fail
		valAddr, err := sdk.ValAddressFromBech32(coreVals[3].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[3], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists", "should fail because inbound is already executed")
	})

	t.Run("duplicate vote fails", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted", "should fail on duplicate vote from same validator")
	})

	t.Run("different inbounds tracked separately", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		inboundB := *inbound
		inboundB.TxHash = "0xabce"

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		err = utils.ExecVoteInbound(t, ctx, app, vals[0], coreValAcc, &inboundB)
		require.NoError(t, err, "votes for different inbounds should be tracked independently")
	})

	t.Run("balance is zero before execution and updated after", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)
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
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
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
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)
		ueModuleAccAddress, _ := app.UexecutorKeeper.GetUeModuleAddress(ctx)
		recipient := common.HexToAddress(inbound.Recipient)

		// First inbound
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, inbound)
			require.NoError(t, err)
		}

		// Second inbound with different TxHash
		inboundB := *inbound
		inboundB.TxHash = "0xabcf"
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreValAcc := sdk.AccAddress(valAddr).String()

			err = utils.ExecVoteInbound(t, ctx, app, vals[i], coreValAcc, &inboundB)
			require.NoError(t, err)
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

	t.Run("pc_tx tx hashes are unique across multiple inbounds", func(t *testing.T) {
		app, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		// Send 5 inbounds with different TxHash values
		for i := 0; i < 5; i++ {
			inboundCopy := *inbound
			inboundCopy.TxHash = fmt.Sprintf("0xabc%d", i)

			for j := 0; j < 3; j++ { // simulate 3 validators voting
				valAddr, err := sdk.ValAddressFromBech32(coreVals[j].OperatorAddress)
				require.NoError(t, err)
				coreValAcc := sdk.AccAddress(valAddr).String()

				err = utils.ExecVoteInbound(t, ctx, app, vals[j], coreValAcc, &inboundCopy)
				require.NoError(t, err)
			}
		}

		// Fetch all universal txs
		q := uexecutorkeeper.Querier{Keeper: app.UexecutorKeeper}
		utsResp, err := q.AllUniversalTx(sdk.WrapSDKContext(ctx), &uexecutortypes.QueryAllUniversalTxRequest{})
		fmt.Println(utsResp)
		require.NoError(t, err)
		require.NotNil(t, utsResp)
		require.GreaterOrEqual(t, len(utsResp.UniversalTxs), 5, "expected at least 5 universal txs")

		// Track and ensure all pc_tx.tx_hash are unique
		txHashes := make(map[string]bool)
		for _, utx := range utsResp.UniversalTxs {
			for _, pcTx := range utx.PcTx {
				require.NotEmpty(t, pcTx.TxHash, "pc_tx tx_hash should not be empty")
				_, exists := txHashes[pcTx.TxHash]
				require.Falsef(t, exists, "duplicate pc_tx tx_hash found: %s", pcTx.TxHash)
				txHashes[pcTx.TxHash] = true
			}
		}
		t.Logf("All %d pc_tx.tx_hash values are unique", len(txHashes))
	})
}
