package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
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

// AllChainConfigs implements types.QueryServer.
func (k Querier) AllChainConfigs(goCtx context.Context, req *types.QueryAllChainConfigsRequest) (*types.QueryAllChainConfigsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	var configs []*types.ChainConfig

	err := k.Keeper.ChainConfigs.Walk(ctx, nil, func(key string, value types.ChainConfig) (stop bool, err error) {
		v := value
		configs = append(configs, &v)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryAllChainConfigsResponse{
		Configs: configs,
	}, nil
}

// TokenConfig implements types.QueryServer.
func (k Querier) TokenConfig(goCtx context.Context, req *types.QueryTokenConfigRequest) (*types.QueryTokenConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	config, err := k.Keeper.GetTokenConfig(ctx, req.Chain, req.Address)
	if err != nil {
		return nil, err
	}

	return &types.QueryTokenConfigResponse{
		Config: &config,
	}, nil
}

// AllTokenConfigs implements types.QueryServer.
func (k Querier) AllTokenConfigs(goCtx context.Context, req *types.QueryAllTokenConfigsRequest) (*types.QueryAllTokenConfigsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	var configs []*types.TokenConfig

	err := k.Keeper.TokenConfigs.Walk(ctx, nil, func(key string, value types.TokenConfig) (stop bool, err error) {
		v := value
		configs = append(configs, &v)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryAllTokenConfigsResponse{
		Configs: configs,
	}, nil
}

// TokenConfigsByChain implements types.QueryServer.
func (k Querier) TokenConfigsByChain(goCtx context.Context, req *types.QueryTokenConfigsByChainRequest) (*types.QueryTokenConfigsByChainResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	var configs []*types.TokenConfig

	err := k.Keeper.TokenConfigs.Walk(ctx, nil, func(key string, value types.TokenConfig) (stop bool, err error) {
		if value.Chain == req.Chain {
			v := value
			configs = append(configs, &v)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryTokenConfigsByChainResponse{
		Configs: configs,
	}, nil
}

// SystemConfig implements types.QueryServer.
func (k Querier) SystemConfig(goCtx context.Context, req *types.QuerySystemConfigRequest) (*types.QuerySystemConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	s, err := k.Keeper.SystemConfig.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QuerySystemConfigResponse{SystemConfig: &s}, nil
}
