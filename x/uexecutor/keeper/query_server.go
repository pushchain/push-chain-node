package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

var _ types.QueryServer = Querier{}

type Querier struct {
	Keeper
}

func NewQuerier(keeper Keeper) Querier {
	return Querier{Keeper: keeper}
}

func (k Querier) Params(c context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	p, err := k.Keeper.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryParamsResponse{Params: &p}, nil
}

// ChainConfig implements types.QueryServer.
func (k Querier) ChainConfig(goCtx context.Context, req *types.QueryChainConfigRequest) (*types.QueryChainConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	cc, err := k.Keeper.GetChainConfig(ctx, req.Chain)
	if err != nil {
		return nil, err
	}

	return &types.QueryChainConfigResponse{Config: &cc}, nil
}
