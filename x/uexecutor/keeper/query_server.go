package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// AllUniversalTx implements types.QueryServer.
func (k Querier) AllUniversalTx(goCtx context.Context, req *types.QueryAllUniversalTxRequest) (*types.QueryAllUniversalTxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	var txs []*types.UniversalTx

	items, pageRes, err := query.CollectionPaginate(ctx, k.UniversalTx, req.Pagination, func(key string, value types.UniversalTx) (*types.UniversalTx, error) {
		return &value, nil
	})
	if err != nil {
		return nil, err
	}

	txs = append(txs, items...)

	return &types.QueryAllUniversalTxResponse{
		UniversalTxs: txs,
		Pagination:   pageRes,
	}, nil
}
