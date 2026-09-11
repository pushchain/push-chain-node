package ante_test

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/pushchain/push-chain-node/app"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ---------------------------------------------------------------------------
// F-2026-18144 — Unchecked Cumulative GasWanted Can Fail FinalizeBlock Under
// Unbounded Block Gas.
//
// Fee-paying txs are self-limiting: the ante handler requires
// ceil(minGasPrice * gasLimit), so a huge declared gas costs huge money.
// Gasless txs pay nothing, so the declared gas is free — and it is still added
// to the fee market's cumulative gas wanted for the block. Two gasless txs each
// declaring MaxInt64 sum to a value that x/feemarket EndBlock cannot convert
// back to int64; it returns an error, and that error comes out of
// FinalizeBlock, after the block has already been decided.
//
// These tests run real signed txs through the whole baseapp -> ante ->
// EndBlock pipeline with the block gas limit set to the unbounded value donut
// runs (max_gas: -1), which is what makes the per-tx block-limit check in the
// EVM ante inert.
// ---------------------------------------------------------------------------

// setupGaslessAnteApp boots a chain app with a single validator and one funded
// genesis account whose private key we keep, so we can sign real gasless txs.
func setupGaslessAnteApp(t *testing.T) (*app.ChainApp, cryptotypes.PrivKey, sdk.AccAddress) {
	t.Helper()

	privVal := mock.NewPV()
	valPubKey, err := privVal.GetPubKey()
	require.NoError(t, err)

	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(valPubKey, 1)})

	senderPrivKey := secp256k1.GenPrivKey()
	senderAcc := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)
	senderAddr := senderAcc.GetAddress()

	balance := banktypes.Balance{
		Address: senderAddr.String(),
		Coins: sdk.NewCoins(
			sdk.NewCoin(sdk.DefaultBondDenom, sdkmath.NewInt(100_000_000_000_000)),
			sdk.NewCoin(app.BaseDenom, sdkmath.NewInt(1).MulRaw(1e18).MulRaw(100)),
		),
	}

	chainApp := app.SetupWithGenesisValSet(
		t, valSet, []authtypes.GenesisAccount{senderAcc}, testChainID, nil, balance,
	)

	setUnboundedBlockGas(t, chainApp)

	return chainApp, senderPrivKey, senderAddr
}

// setUnboundedBlockGas reproduces donut's consensus configuration
// (update_max_block_gas.json sets max_gas to -1), under which
// ante/types.BlockGasLimit returns math.MaxUint64 and the per-tx block gas
// check can never fire. Written through an uncached context so it survives
// into FinalizeBlock.
func setUnboundedBlockGas(t *testing.T, chainApp *app.ChainApp) {
	t.Helper()

	ctx := chainApp.BaseApp.NewUncachedContext(false, cmtproto.Header{})
	cp, err := chainApp.ConsensusParamsKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	cp.Block.MaxGas = -1
	require.NoError(t, chainApp.ConsensusParamsKeeper.ParamsStore.Set(ctx, cp))
}

// gaslessVoteMsg builds a MsgVoteInbound — one of the fee-exempt message types
// in app/txpolicy/gasless.go — that passes ValidateBasic, so the tx is only
// ever stopped by a gas decision and not by message validation.
func gaslessVoteMsg(signer sdk.AccAddress, nonce int) sdk.Msg {
	return &uexecutortypes.MsgVoteInbound{
		Signer: signer.String(),
		Inbound: &uexecutortypes.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      fmt.Sprintf("0xf18144000000000000000000000000000000000000000000000000000000%04d", nonce),
			Sender:      "0x1111111111111111111111111111111111111111",
			Recipient:   "0x2222222222222222222222222222222222222222",
			Amount:      "1",
			AssetAddr:   "0x3333333333333333333333333333333333333333",
			LogIndex:    "0",
			TxType:      uexecutortypes.TxType_FUNDS,
		},
	}
}

// deliverGaslessBlock signs one gasless tx per entry in gasLimits and delivers
// them as a single block.
func deliverGaslessBlock(
	t *testing.T,
	chainApp *app.ChainApp,
	priv cryptotypes.PrivKey,
	addr sdk.AccAddress,
	gasLimits []uint64,
) (*abci.ResponseFinalizeBlock, error) {
	t.Helper()

	ctx := chainApp.BaseApp.NewContext(true)
	acc := chainApp.AccountKeeper.GetAccount(ctx, addr)
	require.NotNil(t, acc)

	txBytes := make([][]byte, 0, len(gasLimits))
	for i, gas := range gasLimits {
		tx, err := simtestutil.GenSignedMockTx(
			rand.New(rand.NewSource(int64(i)+1)),
			chainApp.TxConfig(),
			[]sdk.Msg{gaslessVoteMsg(addr, i)},
			sdk.NewCoins(), // gasless: no fee is offered at all
			gas,
			testChainID,
			[]uint64{acc.GetAccountNumber()},
			[]uint64{acc.GetSequence() + uint64(i)},
			priv,
		)
		require.NoError(t, err)

		bz, err := chainApp.TxConfig().TxEncoder()(tx)
		require.NoError(t, err)
		txBytes = append(txBytes, bz)
	}

	return chainApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: chainApp.LastBlockHeight() + 1,
		Time:   time.Now(),
		Txs:    txBytes,
	})
}

func blockGasWanted(t *testing.T, chainApp *app.ChainApp) uint64 {
	t.Helper()
	return chainApp.FeeMarketKeeper.GetBlockGasWanted(chainApp.BaseApp.NewContext(true))
}

// TestGaslessCumulativeGasWantedCannotFailFinalizeBlock is the chain-level
// regression for the finding itself. Two gasless txs each declaring MaxInt64
// sum to 2^64-2 — a valid uint64 that x/feemarket EndBlock cannot convert to
// int64. Neither tx is individually rejectable without the cap.
func TestGaslessCumulativeGasWantedCannotFailFinalizeBlock(t *testing.T) {
	chainApp, priv, addr := setupGaslessAnteApp(t)

	res, err := deliverGaslessBlock(t, chainApp, priv, addr, []uint64{math.MaxInt64, math.MaxInt64})

	// The block must still be produced. Without the cap, x/feemarket EndBlock
	// errors on the cumulative total and that error surfaces through
	// FinalizeBlock, leaving CometBFT at height H and the app at H-1.
	require.NoError(t, err, "FinalizeBlock must survive the cumulative gas wanted")
	require.NotNil(t, res)
	require.Len(t, res.TxResults, 2)

	require.Less(t, blockGasWanted(t, chainApp), uint64(1_000_000),
		"rejected txs must not contribute their declared gas to the block")

	for i, txRes := range res.TxResults {
		require.NotEqual(t, abci.CodeTypeOK, txRes.Code, "tx %d must be rejected, got success", i)
		require.Contains(t, txRes.Log, "exceeds the maximum allowed",
			"tx %d must be rejected by the gasless gas cap, got: %s", i, txRes.Log)
	}
}

// TestGaslessTxAboveCapRejected covers a single tx one unit over the cap.
func TestGaslessTxAboveCapRejected(t *testing.T) {
	chainApp, priv, addr := setupGaslessAnteApp(t)

	res, err := deliverGaslessBlock(t, chainApp, priv, addr,
		[]uint64{uexecutortypes.DefaultMaxGaslessTxGas + 1})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)

	require.Less(t, blockGasWanted(t, chainApp), uint64(1_000_000),
		"an over-cap tx must not have its declared gas counted")

	txRes := res.TxResults[0]
	require.NotEqual(t, abci.CodeTypeOK, txRes.Code, "over-cap gasless tx must be rejected")
	require.Contains(t, txRes.Log, "exceeds the maximum allowed", "got: %s", txRes.Log)
}

// TestGaslessTxAtCapAccepted pins the other side of the boundary: a tx at
// exactly the cap passes the ante chain and its gas is counted normally.
// x/feemarket EndBlock records max(gasWanted*MinGasMultiplier, gasUsed), and
// MinGasMultiplier defaults to 0.5, so a 100,000,000 declaration must show up
// as at least 50,000,000.
func TestGaslessTxAtCapAccepted(t *testing.T) {
	chainApp, priv, addr := setupGaslessAnteApp(t)

	res, err := deliverGaslessBlock(t, chainApp, priv, addr,
		[]uint64{uexecutortypes.DefaultMaxGaslessTxGas})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)

	require.GreaterOrEqual(t, blockGasWanted(t, chainApp), uint64(50_000_000),
		"a tx at the cap must reach the fee market and have its gas counted")
	require.NotContains(t, res.TxResults[0].Log, "exceeds the maximum allowed",
		"a tx at the cap must not be rejected by the cap")
}

// TestGaslessTxAtUniversalValidatorGasAccepted uses the gas limit the universal
// validators now declare (universalClient/pushsigner/vote.go). The fleet must
// keep voting under the cap.
func TestGaslessTxAtUniversalValidatorGasAccepted(t *testing.T) {
	chainApp, priv, addr := setupGaslessAnteApp(t)

	res, err := deliverGaslessBlock(t, chainApp, priv, addr, []uint64{100_000_000})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)

	require.GreaterOrEqual(t, blockGasWanted(t, chainApp), uint64(50_000_000),
		"the universal validator's declared gas must still be accepted")
	require.NotContains(t, res.TxResults[0].Log, "exceeds the maximum allowed", "got: %s", res.TxResults[0].Log)
}

// TestGaslessCapIsAGovernanceParameter proves the cap is state, not a compiled
// constant: a governance update to uexecutor params changes what the ante
// handler accepts on the very next block.
func TestGaslessCapIsAGovernanceParameter(t *testing.T) {
	const loweredCap = uint64(30_000_000)
	// Comfortably under the 100,000,000 default and comfortably over the
	// lowered cap, so only the parameter can decide the outcome.
	const declaredGas = uint64(40_000_000)

	t.Run("default cap admits 40M", func(t *testing.T) {
		chainApp, priv, addr := setupGaslessAnteApp(t)

		res, err := deliverGaslessBlock(t, chainApp, priv, addr, []uint64{declaredGas})
		require.NoError(t, err)

		require.GreaterOrEqual(t, blockGasWanted(t, chainApp), uint64(20_000_000),
			"40M must be accepted under the default cap")
		require.NotContains(t, res.TxResults[0].Log, "exceeds the maximum allowed", "got: %s", res.TxResults[0].Log)
	})

	t.Run("governance lowers the cap and 40M is rejected", func(t *testing.T) {
		chainApp, priv, addr := setupGaslessAnteApp(t)
		setGaslessCapByGovernance(t, chainApp, loweredCap)

		res, err := deliverGaslessBlock(t, chainApp, priv, addr, []uint64{declaredGas})
		require.NoError(t, err)

		require.Less(t, blockGasWanted(t, chainApp), uint64(1_000_000),
			"a tx over the lowered cap must not have its gas counted")

		txRes := res.TxResults[0]
		require.NotEqual(t, abci.CodeTypeOK, txRes.Code)
		require.Contains(t, txRes.Log, "exceeds the maximum allowed", "got: %s", txRes.Log)
		require.Contains(t, txRes.Log, "30000000", "the error must quote the governance-set cap, got: %s", txRes.Log)
	})

	t.Run("governance lowers the cap and 30M is still accepted", func(t *testing.T) {
		chainApp, priv, addr := setupGaslessAnteApp(t)
		setGaslessCapByGovernance(t, chainApp, loweredCap)

		res, err := deliverGaslessBlock(t, chainApp, priv, addr, []uint64{loweredCap})
		require.NoError(t, err)

		require.GreaterOrEqual(t, blockGasWanted(t, chainApp), uint64(15_000_000),
			"a tx at the lowered cap must still be accepted")
		require.NotContains(t, res.TxResults[0].Log, "exceeds the maximum allowed", "got: %s", res.TxResults[0].Log)
	})

	t.Run("governance cannot brick voting with a zero cap", func(t *testing.T) {
		chainApp, _, _ := setupGaslessAnteApp(t)

		ctx := chainApp.BaseApp.NewUncachedContext(false, cmtproto.Header{})
		params, err := chainApp.UexecutorKeeper.GetParams(ctx)
		require.NoError(t, err)
		params.MaxGaslessTxGas = 0

		_, err = uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper).UpdateParams(ctx,
			&uexecutortypes.MsgUpdateParams{
				Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Params:    params,
			})

		stored, getErr := chainApp.UexecutorKeeper.GetParams(ctx)
		require.NoError(t, getErr)
		require.Equal(t, uexecutortypes.DefaultMaxGaslessTxGas, stored.MaxGaslessTxGas,
			"a rejected update must leave the cap untouched")
		require.Error(t, err, "a zero cap must be rejected")
		require.Contains(t, err.Error(), "max_gasless_tx_gas")
	})
}

// setGaslessCapByGovernance applies a params update through the module's
// MsgServer with the real gov authority, i.e. exactly what a passed proposal
// executes.
func setGaslessCapByGovernance(t *testing.T, chainApp *app.ChainApp, cap uint64) {
	t.Helper()

	ctx := chainApp.BaseApp.NewUncachedContext(false, cmtproto.Header{})

	params, err := chainApp.UexecutorKeeper.GetParams(ctx)
	require.NoError(t, err)
	params.MaxGaslessTxGas = cap

	_, err = uexecutorkeeper.NewMsgServerImpl(chainApp.UexecutorKeeper).UpdateParams(ctx,
		&uexecutortypes.MsgUpdateParams{
			Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			Params:    params,
		})
	require.NoError(t, err)

	stored, err := chainApp.UexecutorKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, cap, stored.MaxGaslessTxGas)
}
