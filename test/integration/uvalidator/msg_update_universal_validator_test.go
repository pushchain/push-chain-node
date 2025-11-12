package integrationtest

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// setupUpdateUniversalValidatorTest initializes an app with multiple validators
// and registers one universal validator entry.
func setupUpdateUniversalValidatorTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	universalVals := make([]string, len(validators))

	// add one universal validator to update later
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		pubkey := fmt.Sprintf("pubkey-%d", i)
		network := uvalidatortypes.NetworkInfo{Ip: fmt.Sprintf("192.168.1.%d", i+1)}

		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, pubkey, network)
		require.NoError(t, err)

		universalVals[i] = coreValAddr
	}

	return app, ctx, universalVals
}

func TestUpdateUniversalValidator(t *testing.T) {
	t.Run("successfully updates existing universal validator metadata", func(t *testing.T) {
		app, ctx, vals := setupUpdateUniversalValidatorTest(t, 1)
		coreVal := vals[0]

		newPubkey := "updated-pubkey"
		newNetwork := uvalidatortypes.NetworkInfo{Ip: "10.0.0.1"}

		err := app.UvalidatorKeeper.UpdateUniversalValidator(ctx, coreVal, newPubkey, newNetwork)
		require.NoError(t, err)

		valAddr, err := sdk.ValAddressFromBech32(coreVal)
		require.NoError(t, err)

		updatedVal, err := app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
		require.NoError(t, err)

		require.Equal(t, newPubkey, updatedVal.IdentifyInfo.Pubkey)
		require.Equal(t, newNetwork.Ip, updatedVal.NetworkInfo.Ip)
	})

	t.Run("fails when validator does not exist", func(t *testing.T) {
		app, ctx, _ := setupUpdateUniversalValidatorTest(t, 1)

		coreVal := "pushvaloper1invalidaddress00000000000000000000000"
		pubkey := "somekey"
		network := uvalidatortypes.NetworkInfo{Ip: "10.0.0.2"}

		err := app.UvalidatorKeeper.UpdateUniversalValidator(ctx, coreVal, pubkey, network)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid core validator address")
	})

	t.Run("fails when validator not registered as universal", func(t *testing.T) {
		app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 1)
		coreVal := validators[0].OperatorAddress

		pubkey := "random"
		network := uvalidatortypes.NetworkInfo{Ip: "10.0.0.4"}

		err := app.UvalidatorKeeper.UpdateUniversalValidator(ctx, coreVal, pubkey, network)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})
}
