package keeper

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

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

// AllChainConfigs implements types.QueryServer with pagination.
func (k Querier) AllChainConfigs(goCtx context.Context, req *types.QueryAllChainConfigsRequest) (*types.QueryAllChainConfigsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	configs, pageRes, err := query.CollectionPaginate(
		ctx, k.Keeper.ChainConfigs, req.Pagination,
		func(_ string, value types.ChainConfig) (*types.ChainConfig, error) {
			v := value
			return &v, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllChainConfigsResponse{
		Configs:    configs,
		Pagination: pageRes,
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

// AllTokenConfigs implements types.QueryServer with pagination.
func (k Querier) AllTokenConfigs(goCtx context.Context, req *types.QueryAllTokenConfigsRequest) (*types.QueryAllTokenConfigsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	configs, pageRes, err := query.CollectionPaginate(
		ctx, k.Keeper.TokenConfigs, req.Pagination,
		func(_ string, value types.TokenConfig) (*types.TokenConfig, error) {
			v := value
			return &v, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllTokenConfigsResponse{
		Configs:    configs,
		Pagination: pageRes,
	}, nil
}

// TokenConfigsByChain implements types.QueryServer with pagination + key-prefix
// filter. Storage key is "<chain>:<address>", so we filter at the key level.
func (k Querier) TokenConfigsByChain(goCtx context.Context, req *types.QueryTokenConfigsByChainRequest) (*types.QueryTokenConfigsByChainResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	chainPrefix := req.Chain + ":"

	configs, pageRes, err := query.CollectionFilteredPaginate(
		ctx, k.Keeper.TokenConfigs, req.Pagination,
		func(key string, _ types.TokenConfig) (bool, error) {
			return strings.HasPrefix(key, chainPrefix), nil
		},
		func(_ string, value types.TokenConfig) (*types.TokenConfig, error) {
			v := value
			return &v, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryTokenConfigsByChainResponse{
		Configs:    configs,
		Pagination: pageRes,
	}, nil
}
