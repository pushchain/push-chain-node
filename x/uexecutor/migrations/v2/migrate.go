package v2

import (
	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func MigrateParamsFromAdminToBool(ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec) error {
	sb := k.SchemaBuilder()

	// Create the collection item
	item := collections.NewItem(sb, uexecutortypes.ParamsKey, uexecutortypes.ParamsName, codec.CollValue[uexecutortypes.Params](cdc))

	// Set the new params with SomeValue = true
	newParams := uexecutortypes.Params{
		SomeValue: true,
	}

	if err := item.Set(ctx, newParams); err != nil {
		return err
	}

	return nil
}
