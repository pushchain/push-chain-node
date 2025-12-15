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
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}

		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, network)
		require.NoError(t, err)

		universalVals[i] = coreValAddr
	}

	return app, ctx, universalVals
}

func TestUpdateUniversalValidator(t *testing.T) {
	t.Run("successfully updates existing universal validator metadata", func(t *testing.T) {
		app, ctx, vals := setupUpdateUniversalValidatorTest(t, 1)
		coreVal := vals[0]

		// newPubkey := "updated-pubkey"
		// newNetwork := uvalidatortypes.NetworkInfo{Ip: "10.0.0.1"}
		newNetwork := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("updated-peerId"), MultiAddrs: []string{"updated-multi_addrs"}}

		err := app.UvalidatorKeeper.UpdateUniversalValidator(ctx, coreVal, newNetwork)
		require.NoError(t, err)

		valAddr, err := sdk.ValAddressFromBech32(coreVal)
		require.NoError(t, err)

		updatedVal, err := app.UvalidatorKeeper.UniversalValidatorSet.Get(ctx, valAddr)
		require.NoError(t, err)

		require.Equal(t, newNetwork, *updatedVal.NetworkInfo)
	})

	t.Run("fails when validator does not exist", func(t *testing.T) {
		app, ctx, _ := setupUpdateUniversalValidatorTest(t, 1)

		coreVal := "pushvaloper1invalidaddress00000000000000000000000"
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp peerId"), MultiAddrs: []string{"temp multi_addrs"}}

		err := app.UvalidatorKeeper.UpdateUniversalValidator(ctx, coreVal, network)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid core validator address")
	})

	t.Run("fails when validator not registered as universal", func(t *testing.T) {
		app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, 1)
		coreVal := validators[0].OperatorAddress

		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp peerId"), MultiAddrs: []string{"temp multi_addrs"}}

		err := app.UvalidatorKeeper.UpdateUniversalValidator(ctx, coreVal, network)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})
}
