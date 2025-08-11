package testutils

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/app"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type TestAccounts struct {
	DefaultAccount sdk.AccountI
	CosmosAccount  sdk.AccountI
	TargetAccount  sdk.AccountI
}

func SetupTestAccounts(t *testing.T, app *app.ChainApp, ctx sdk.Context, opt AppSetupOptions) TestAccounts {
	accounts := TestAccounts{}

	default_addr := sdk.AccAddress([]byte(opt.Addresses.DefaultTestAddr))
	accounts.DefaultAccount = createAndFundAccount(t, app, ctx, default_addr, opt.TestConfig.DefaultCoinAmt, opt.TestConfig.BaseCoinDenom)

	cosmos_addr := sdk.MustAccAddressFromBech32(opt.Addresses.CosmosTestAddr)
	accounts.CosmosAccount = createAccount(app, ctx, cosmos_addr)

	target_addr := sdk.AccAddress([]byte(opt.Addresses.TargetAddr))
	accounts.TargetAccount = createAndFundAccount(t, app, ctx, target_addr, opt.TestConfig.DefaultCoinAmt, opt.TestConfig.BaseCoinDenom)

	return accounts
}

func createAccount(app *app.ChainApp, ctx sdk.Context, addr sdk.AccAddress) sdk.AccountI {
	acc := app.AccountKeeper.NewAccountWithAddress(ctx, addr)
	app.AccountKeeper.SetAccount(ctx, acc)
	return acc
}

func createAndFundAccount(t *testing.T, app *app.ChainApp, ctx sdk.Context, addr sdk.AccAddress, amount int64, denom string) sdk.AccountI {
	coins := sdk.NewCoins(sdk.NewInt64Coin(denom, amount))
	acc := createAccount(app, ctx, addr)

	// Mint coins to the mint module
	err := app.BankKeeper.MintCoins(ctx, MintModule, coins)
	require.NoError(t, err)

	// Send coins from mint module to account
	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, MintModule, addr, coins)
	require.NoError(t, err)

	return acc
}

func setupValidator(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, account sdk.AccountI) (stakingtypes.Validator, cryptotypes.PubKey) {
	pk := ed25519.GenPrivKey().PubKey()
	valAddr := sdk.ValAddress(account.GetAddress())

	validator, err := stakingtypes.NewValidator(valAddr.String(), pk, stakingtypes.Description{})
	require.NoError(t, err)

	// Set validator in staking keeper
	chainApp.StakingKeeper.SetValidator(ctx, validator)
	chainApp.StakingKeeper.SetValidatorByConsAddr(ctx, validator)
	chainApp.StakingKeeper.SetNewValidatorByPowerIndex(ctx, validator)

	// Verify validator setup (optional logging)
	retrievedValidator, err := chainApp.StakingKeeper.GetValidatorByConsAddr(ctx, sdk.ConsAddress(pk.Address()))
	if err != nil {
		t.Logf("Validator successfully set up: %s", retrievedValidator.OperatorAddress)
	}

	return validator, pk
}
