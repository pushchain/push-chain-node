package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollchains/pchain/x/ue/types"
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

// AdminParams implements types.QueryServer.
func (k Querier) AdminParams(goCtx context.Context, req *types.QueryAdminParamsRequest) (*types.QueryAdminParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	ap, err := k.Keeper.AdminParams.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryAdminParamsResponse{AdminParams: &ap}, nil
}

// ChainConfig implements types.QueryServer.
func (k Querier) ChainConfig(goCtx context.Context, req *types.QueryChainConfigRequest) (*types.QueryChainConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	cc, err := k.Keeper.ChainConfigs.Get(ctx, req.ChainId)
	if err != nil {
		return nil, err
	}

	return &types.QueryChainConfigResponse{Config: &cc}, nil
}
