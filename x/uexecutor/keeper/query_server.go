package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
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

// GetUniversalTx implements types.QueryServer.
func (k Querier) GetUniversalTx(goCtx context.Context, req *types.QueryGetUniversalTxRequest) (*types.QueryGetUniversalTxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Fetch from the collection
	tx, err := k.UniversalTx.Get(ctx, req.Id) // req.Id is the string key
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "UniversalTx not found")
		}
		return nil, err
	}

	return &types.QueryGetUniversalTxResponse{
		UniversalTx: &tx,
	}, nil
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

// AllPendingInbounds implements types.QueryServer.
func (k Keeper) AllPendingInbounds(goCtx context.Context, req *types.QueryAllPendingInboundsRequest) (*types.QueryAllPendingInboundsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	inbounds, pageRes, err := query.CollectionPaginate(ctx, k.PendingInbounds, req.Pagination, func(key string, _ collections.NoValue) (string, error) {
		return key, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllPendingInboundsResponse{
		InboundIds: inbounds,
		Pagination: pageRes,
	}, nil
}
