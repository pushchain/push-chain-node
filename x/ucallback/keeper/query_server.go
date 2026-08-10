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

// UniversalRead implements types.QueryServer.
//
// Serves a read at any point in its lifecycle, settled or not — this is the
// endpoint for "what happened to my request", so it must not filter the way
// AllPendingReadRequests does.
func (k Querier) UniversalRead(goCtx context.Context, req *types.QueryUniversalReadRequest) (*types.QueryUniversalReadResponse, error) {
	if req == nil || req.RequestId == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	ur, found := k.Keeper.GetUniversalRead(ctx, req.RequestId)
	if !found {
		return nil, status.Errorf(codes.NotFound, "no read request with id %s", req.RequestId)
	}

	return &types.QueryUniversalReadResponse{Read: ur}, nil
}

// ReadsByTx implements types.QueryServer.
//
// Returns every read a single Push transaction requested, settled or not. Batches
// are the reason this exists: one transaction can emit several ReadRequested logs,
// each becoming an independent record that settles on its own schedule.
//
// Unpaginated by design — the fan-out is bounded by what fits in one transaction.
func (k Querier) ReadsByTx(goCtx context.Context, req *types.QueryReadsByTxRequest) (*types.QueryReadsByTxResponse, error) {
	if req == nil || req.TxHash == "" {
		return nil, status.Error(codes.InvalidArgument, "tx_hash is required")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	reads := []types.UniversalRead{}
	if err := k.Keeper.IterateReadsByTxHash(ctx, req.TxHash, func(ur types.UniversalRead) bool {
		reads = append(reads, ur)
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryReadsByTxResponse{Reads: reads}, nil
}

// AllAbortedReadRequests implements types.QueryServer.
//
// Paginates the AbortedReads index rather than filtering UniversalReads. Abandoned
// reads should be rare, so a status filter over the full history could walk every
// read the chain has ever seen just to fill one page — a soft DoS on a public
// endpoint. The index holds only the abandoned ones.
func (k Querier) AllAbortedReadRequests(goCtx context.Context, req *types.QueryAllAbortedReadRequestsRequest) (*types.QueryAllAbortedReadRequestsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	reads, pageRes, err := query.CollectionPaginate(
		ctx, k.Keeper.AbortedReads, req.Pagination,
		func(requestID string, _ collections.NoValue) (types.UniversalRead, error) {
			ur, found := k.Keeper.GetUniversalRead(ctx, requestID)
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

	return &types.QueryAllAbortedReadRequestsResponse{
		Reads:      reads,
		Pagination: pageRes,
	}, nil
}
