package integrationtest

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"

	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func setupUniversalValidatorTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []stakingtypes.Validator) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)
	return app, ctx, validators
}

func TestAddUniversalValidator(t *testing.T) {
	t.Run("Successfully adds multiple bonded validators", func(t *testing.T) {
		app, ctx, validators := setupUniversalValidatorTest(t, 3)

		for i, val := range validators {
			coreValAddr := val.OperatorAddress
			pubkey := fmt.Sprintf("pubkey-%d", i)
			network := uvalidatortypes.NetworkInfo{Ip: fmt.Sprintf("192.168.1.%d", i+1)}

			err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, pubkey, network)
			require.NoError(t, err)

			valAddr, err := sdk.ValAddressFromBech32(coreValAddr)
			require.NoError(t, err)

			exists, err := app.UvalidatorKeeper.UniversalValidatorSet.Has(ctx, valAddr)
			require.NoError(t, err)
			require.True(t, exists, "validator should exist in universal set")

			uv, _ := app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
			require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN, uv.LifecycleInfo.CurrentStatus)
			require.Equal(t, pubkey, uv.IdentifyInfo.Pubkey)
		}
	})

	t.Run("Reactivates an inactive validator", func(t *testing.T) {
		app, ctx, validators := setupUniversalValidatorTest(t, 1)
		k := app.UvalidatorKeeper
		val := validators[0]
		valAddr, _ := sdk.ValAddressFromBech32(val.OperatorAddress)

		// pre-store inactive
		old := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: val.OperatorAddress, Pubkey: "old-pub"},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_INACTIVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, old))

		pubkey := "new-pub"
		network := uvalidatortypes.NetworkInfo{Ip: "10.0.0.1"}
		err := k.AddUniversalValidator(ctx, val.OperatorAddress, pubkey, network)
		require.NoError(t, err)

		uv, _ := k.UniversalValidatorSet.Get(ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN, uv.LifecycleInfo.CurrentStatus)
		require.Equal(t, pubkey, uv.IdentifyInfo.Pubkey)
	})

	t.Run("Adding already active validator fails", func(t *testing.T) {
		app, ctx, validators := setupUniversalValidatorTest(t, 1)
		k := app.UvalidatorKeeper
		val := validators[0]
		valAddr, _ := sdk.ValAddressFromBech32(val.OperatorAddress)

		active := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: val.OperatorAddress},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, active))

		err := k.AddUniversalValidator(ctx, val.OperatorAddress, "pub", uvalidatortypes.NetworkInfo{})
		require.ErrorContains(t, err, "already registered")
	})

	t.Run("Unbonded validator cannot join", func(t *testing.T) {
		app, ctx, validators := setupUniversalValidatorTest(t, 1)
		val := validators[0]
		valAddr, _ := sdk.ValAddressFromBech32(val.OperatorAddress)

		// make validator unbonded manually
		valBonded := val
		valBonded.Status = stakingtypes.Unbonded
		app.StakingKeeper.SetValidator(ctx, valBonded)

		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, valAddr.String(), "pub", uvalidatortypes.NetworkInfo{})
		require.ErrorContains(t, err, "not bonded")
	})

	t.Run("Invalid validator address format fails", func(t *testing.T) {
		app, ctx, _ := setupUniversalValidatorTest(t, 1)

		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, "invalid_bech32", "pub", uvalidatortypes.NetworkInfo{})
		require.ErrorContains(t, err, "invalid core validator address")
	})
}
