package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
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

// AllPendingReadRequests implements types.QueryServer.
//
// Paginates the in-flight set (PendingByExpiry), which already excludes settled
// reads. Requests whose expiry height has passed are filtered out here too, rather
// than waiting for the sweeper to retire them: a validator that picked one up would
// spend a destination-chain read on work the contract may no longer accept. That
// makes the visible set correct regardless of how often the sweeper runs.
func (k Querier) AllPendingReadRequests(goCtx context.Context, req *types.QueryAllPendingReadRequestsRequest) (*types.QueryAllPendingReadRequestsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	height := uint64(ctx.BlockHeight())

	reads, pageRes, err := query.CollectionFilteredPaginate(
		ctx, k.Keeper.PendingByExpiry, req.Pagination,
		func(key collections.Pair[uint64, string], _ collections.NoValue) (bool, error) {
			return key.K1() > height, nil
		},
		func(key collections.Pair[uint64, string], _ collections.NoValue) (types.UniversalRead, error) {
			ur, found := k.Keeper.GetUniversalRead(ctx, key.K2())
			if !found {
				// index entry with no record — skip rather than fail the page
				return types.UniversalRead{}, nil
			}
			return ur, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllPendingReadRequestsResponse{
		Reads:      reads,
		Pagination: pageRes,
	}, nil
}
