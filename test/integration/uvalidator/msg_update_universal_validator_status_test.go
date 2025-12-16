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

func setupUpdateUVStatusTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []stakingtypes.Validator) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)
	return app, ctx, validators
}

func TestUpdateUniversalValidatorStatus(t *testing.T) {

	t.Run("PENDING_LEAVE -> ACTIVE (valid) and MUST NOT start new TSS process", func(t *testing.T) {
		app, ctx, validators := setupUpdateUVStatusTest(t, 1)
		k := app.UvalidatorKeeper
		tss := app.UtssKeeper

		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		// register validator in PENDING_LEAVE
		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		// ensure no existing TSS process
		_ = tss.CurrentTssProcess.Remove(ctx)

		// perform update
		err := k.UpdateUniversalValidatorStatus(ctx, valAddr.String(),
			uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		require.NoError(t, err)

		// verify status changed correctly
		updated, _ := k.UniversalValidatorSet.Get(ctx, valAddr)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE, updated.LifecycleInfo.CurrentStatus)

		// verify NO TSS PROCESS was created
		_, err = tss.CurrentTssProcess.Get(ctx)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("PENDING_LEAVE -> INVALID new status fails", func(t *testing.T) {
		app, ctx, validators := setupUpdateUVStatusTest(t, 1)
		k := app.UvalidatorKeeper

		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		err := k.UpdateUniversalValidatorStatus(ctx, valAddr.String(),
			uvalidatortypes.UVStatus_UV_STATUS_INACTIVE)
		require.ErrorContains(t, err, "invalid new status")
	})

	t.Run("Not in PENDING_LEAVE -> error", func(t *testing.T) {
		app, ctx, validators := setupUpdateUVStatusTest(t, 1)
		k := app.UvalidatorKeeper

		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		// ACTIVE -> cannot call
		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		err := k.UpdateUniversalValidatorStatus(ctx, valAddr.String(),
			uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		require.ErrorContains(t, err, "current status must be PENDING_LEAVE")
	})

	t.Run("Fails if TSS process is currently running", func(t *testing.T) {
		app, ctx, validators := setupUpdateUVStatusTest(t, 1)
		k := app.UvalidatorKeeper
		tss := app.UtssKeeper

		valAddr, _ := sdk.ValAddressFromBech32(validators[0].OperatorAddress)

		// create active TSS process
		process := utsstypes.TssKeyProcess{
			Participants: []string{valAddr.String()},
			ExpiryHeight: ctx.BlockHeight() + 100,
		}
		require.NoError(t, tss.CurrentTssProcess.Set(ctx, process))

		uv := uvalidatortypes.UniversalValidator{
			IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: valAddr.String()},
			LifecycleInfo: &uvalidatortypes.LifecycleInfo{
				CurrentStatus: uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			},
		}
		require.NoError(t, k.UniversalValidatorSet.Set(ctx, valAddr, uv))

		err := k.UpdateUniversalValidatorStatus(ctx, valAddr.String(),
			uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		require.ErrorContains(t, err, "TSS process is ongoing")
	})
}
