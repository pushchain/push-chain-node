package ante_test

import (
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
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/pushchain/push-chain-node/app"
)

// testChainID must be a chain ID app.EVMAppOptions knows about, otherwise
// NewChainApp panics while configuring the EVM coin info.
var testChainID = app.ChainID

// setupVestingAnteApp boots a chain app with a single validator and one funded
// genesis account whose private key we keep, so we can sign real txs and push
// them through the full baseapp -> ante pipeline.
func setupVestingAnteApp(t *testing.T) (*app.ChainApp, cryptotypes.PrivKey, sdk.AccAddress) {
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
			// Enough of the EVM denom to actually pay the fee, so that the tx is
			// only ever rejected because of the msg type and not because it is
			// underfunded.
			sdk.NewCoin(app.BaseDenom, sdkmath.NewInt(1).MulRaw(1e18).MulRaw(100)),
		),
	}

	chainApp := app.SetupWithGenesisValSet(
		t, valSet, []authtypes.GenesisAccount{senderAcc}, testChainID, nil, balance,
	)

	return chainApp, senderPrivKey, senderAddr
}

// disableInflation zeroes out the mint module so that the only thing that can
// change total supply across the test block is the tx under test, not block
// inflation. Written through an uncached context so it survives into
// FinalizeBlock.
func disableInflation(t *testing.T, chainApp *app.ChainApp) {
	t.Helper()

	ctx := chainApp.BaseApp.NewUncachedContext(false, cmtproto.Header{})

	params, err := chainApp.MintKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.InflationMin = sdkmath.LegacyZeroDec()
	params.InflationMax = sdkmath.LegacyZeroDec()
	params.InflationRateChange = sdkmath.LegacyZeroDec()
	require.NoError(t, chainApp.MintKeeper.Params.Set(ctx, params))

	minter, err := chainApp.MintKeeper.Minter.Get(ctx)
	require.NoError(t, err)
	minter.Inflation = sdkmath.LegacyZeroDec()
	minter.AnnualProvisions = sdkmath.LegacyZeroDec()
	require.NoError(t, chainApp.MintKeeper.Minter.Set(ctx, minter))
}

func totalSupply(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context) sdk.Coins {
	t.Helper()

	supply := sdk.NewCoins()
	chainApp.BankKeeper.IterateTotalSupply(ctx, func(coin sdk.Coin) bool {
		supply = supply.Add(coin)
		return false
	})

	return supply
}

// TestVestingAccountCreationBlockedEndToEnd is the chain-level regression test
// for F-2026-18201.
//
// The staking-precompile underflow attack needs a vesting account: the EVM state
// view tracks only SPENDABLE balance, while Cosmos lets a vesting account
// DELEGATE locked coins. Delegating more than the spendable balance makes the
// StateDB subtract more than it holds; x/vm/keeper/statedb.go then reconciles
// that bogus view back into bank by MINTING the difference (or by BURNING a
// victim's real coins on the wrap-transfer variant).
//
// Vesting-account creation used to be permissionless: NewAuthzLimiterDecorator
// blocked MsgCreateVestingAccount only INSIDE an authz.MsgExec, so a plain
// top-level tx went straight through - and MsgCreatePermanentLockedAccount /
// MsgCreatePeriodicVestingAccount were not blocked anywhere at all. This test
// submits each of the three as a real signed tx and asserts that it is rejected,
// that no vesting account is created, and that neither the sender's balance nor
// total native supply moves.
func TestVestingAccountCreationBlockedEndToEnd(t *testing.T) {
	// The vesting amount is denominated in the EVM/staking denom, which is what
	// makes the account a usable attack primitive in the first place.
	amount := sdk.NewCoins(sdk.NewCoin(app.BaseDenom, sdkmath.NewInt(1).MulRaw(1e18)))
	future := time.Now().Add(365 * 24 * time.Hour).Unix()

	// Comfortably above the dynamic min gas price so the tx is not rejected by
	// the fee decorators instead of the blocked-msgs decorator.
	fees := sdk.NewCoins(sdk.NewCoin(app.BaseDenom,
		sdkmath.NewInt(1e10).MulRaw(int64(simtestutil.DefaultGenTxGas))))

	testCases := []struct {
		name string
		msg  func(from, to sdk.AccAddress) sdk.Msg
	}{
		{
			"MsgCreateVestingAccount",
			func(from, to sdk.AccAddress) sdk.Msg {
				return sdkvesting.NewMsgCreateVestingAccount(from, to, amount, future, false)
			},
		},
		{
			"MsgCreatePermanentLockedAccount",
			func(from, to sdk.AccAddress) sdk.Msg {
				return sdkvesting.NewMsgCreatePermanentLockedAccount(from, to, amount)
			},
		},
		{
			"MsgCreatePeriodicVestingAccount",
			func(from, to sdk.AccAddress) sdk.Msg {
				return sdkvesting.NewMsgCreatePeriodicVestingAccount(from, to, time.Now().Unix(),
					[]sdkvesting.Period{{Length: 3600, Amount: amount}})
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			chainApp, senderPriv, senderAddr := setupVestingAnteApp(t)
			disableInflation(t, chainApp)
			victimAddr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address().Bytes())

			ctx := chainApp.BaseApp.NewContext(true)
			senderAccount := chainApp.AccountKeeper.GetAccount(ctx, senderAddr)
			require.NotNil(t, senderAccount)

			supplyBefore := totalSupply(t, chainApp, ctx)
			senderBalanceBefore := chainApp.BankKeeper.GetAllBalances(ctx, senderAddr)
			victimBalanceBefore := chainApp.BankKeeper.GetAllBalances(ctx, victimAddr)

			res, err := app.SignAndDeliverWithoutCommit(
				t,
				chainApp.TxConfig(),
				chainApp.BaseApp,
				[]sdk.Msg{tc.msg(senderAddr, victimAddr)},
				fees,
				testChainID,
				[]uint64{senderAccount.GetAccountNumber()},
				[]uint64{senderAccount.GetSequence()},
				time.Now(),
				senderPriv,
			)
			require.NoError(t, err, "block must still be produced")
			require.Len(t, res.TxResults, 1)

			txRes := res.TxResults[0]
			require.NotEqual(t, abci.CodeTypeOK, txRes.Code,
				"vesting account creation must be rejected in ante, got success: %s", txRes.Log)
			require.Contains(t, txRes.Log, "found blocked msg type",
				"tx must be rejected by the blocked-msgs decorator, got: %s", txRes.Log)

			// The tx failed in ante, so nothing it would have done may be visible.
			ctxAfter := chainApp.BaseApp.NewContext(true)

			require.Nil(t, chainApp.AccountKeeper.GetAccount(ctxAfter, victimAddr),
				"no vesting account may be created")
			require.Equal(t, victimBalanceBefore, chainApp.BankKeeper.GetAllBalances(ctxAfter, victimAddr),
				"victim spendable balance must be unchanged")
			require.Equal(t, senderBalanceBefore, chainApp.BankKeeper.GetAllBalances(ctxAfter, senderAddr),
				"sender spendable balance must be unchanged")
			require.Equal(t, supplyBefore, totalSupply(t, chainApp, ctxAfter),
				"total native supply must be unchanged across the tx")
		})
	}
}
