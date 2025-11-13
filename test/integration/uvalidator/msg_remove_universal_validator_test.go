package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// setup function (same as before)
func setupRemoveUniversalValidatorTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []stakingtypes.Validator) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)
	return app, ctx, validators
}

func TestRemoveUniversalValidator(t *testing.T) {
	t.Run("ACTIVE -> PENDING_LEAVE", func(t *testing.T) {
		app, ctx, validators := setupRemoveUniversalValidatorTest(t, 1)
		k := app.UvalidatorKeeper

		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		err := k.RemoveUniversalValidator(ctx, valAddr.String())
		require.NoError(t, err)

		updated, _ := k.UniversalValidatorSet.Get(ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE, updated.LifecycleInfo.CurrentStatus)
	})

	t.Run("PENDING_JOIN -> INACTIVE (not in TSS)", func(t *testing.T) {
		app, ctx, validators := setupRemoveUniversalValidatorTest(t, 1)
		k := app.UvalidatorKeeper
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		// ensure utssKeeper has no current TSS process set
		err := app.UtssKeeper.CurrentTssProcess.Remove(ctx)
		require.NoError(t, err)

		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		err = k.RemoveUniversalValidator(ctx, valAddr.String())
		require.NoError(t, err)

		updated, _ := k.UniversalValidatorSet.Get(ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE, updated.LifecycleInfo.CurrentStatus)
	})

	t.Run("PENDING_JOIN -> REVERT if in current TSS process", func(t *testing.T) {
		app, ctx, validators := setupRemoveUniversalValidatorTest(t, 1)
		k := app.UvalidatorKeeper
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		// add fake current TSS process with this validator as participant
		process := utsstypes.TssKeyProcess{
			Participants: []string{valAddr.String()},
		}
		require.NoError(t, app.UtssKeeper.CurrentTssProcess.Set(ctx, process))

		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		err := k.RemoveUniversalValidator(ctx, valAddr.String())
		require.ErrorContains(t, err, "cannot be removed")
	})

	t.Run("Already INACTIVE -> error", func(t *testing.T) {
		app, ctx, validators := setupRemoveUniversalValidatorTest(t, 1)
		k := app.UvalidatorKeeper
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_INACTIVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		err := k.RemoveUniversalValidator(ctx, valAddr.String())
		require.ErrorContains(t, err, "already in UV_STATUS_INACTIVE")
	})

	t.Run("Invalid validator address format fails", func(t *testing.T) {
		app, ctx, _ := setupRemoveUniversalValidatorTest(t, 1)
		err := app.UvalidatorKeeper.RemoveUniversalValidator(ctx, "invalid_bech32")
		require.ErrorContains(t, err, "invalid universal validator address")
	})

	t.Run("Non-existent validator fails", func(t *testing.T) {
		app, ctx, validators := setupRemoveUniversalValidatorTest(t, 1)
		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		err := app.UvalidatorKeeper.RemoveUniversalValidator(ctx, valAddr.String())
		require.ErrorContains(t, err, "not found")
	})
}
