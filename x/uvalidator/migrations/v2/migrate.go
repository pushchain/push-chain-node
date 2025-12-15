package v2

import (
	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

func MigrateUniversalValidatorSet(ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec) error {
	sb := k.SchemaBuilder()

	// Old KeySet -> only stored validator addresses
	oldKeySet := collections.NewKeySet(
		sb,
		types.CoreValidatorSetKey,
		types.CoreValidatorSetName,
		sdk.ValAddressKey, // ValAddressKey
	)

	iter, err := oldKeySet.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		valAddr, err := iter.Key()
		if err != nil {
			return err
		}

		// Build new UniversalValidator struct here with temporary params
		newVal := types.UniversalValidator{
			IdentifyInfo: &types.IdentityInfo{
				CoreValidatorAddress: valAddr.String(),
			},
			NetworkInfo: &types.NetworkInfo{
				PeerId:     "12D3KooWFNC8BxiPoHyTJtiN1u1ctw3nSexuJHUBv4mMMmqEtQgg",
				MultiAddrs: []string{"/ip4/127.0.0.1/tcp/39001/p2p/12D3KooWFNC8BxiPoHyTJtiN1u1ctw3nSexuJHUBv4mMMmqEtQgg"},
			},
			LifecycleInfo: &types.LifecycleInfo{
				CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN,
				History: []*types.LifecycleEvent{
					{
						Status:      types.UVStatus_UV_STATUS_PENDING_JOIN,
						BlockHeight: ctx.BlockHeight(),
					},
				},
			},
		}

		// Write into new Map
		if err := k.UniversalValidatorSet.Set(ctx, valAddr, newVal); err != nil {
			return err
		}
	}

	return nil
}
