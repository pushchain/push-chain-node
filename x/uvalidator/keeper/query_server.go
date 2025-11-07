package keeper

import (
	"context"

	"errors"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
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

func (k Querier) AllUniversalValidators(goCtx context.Context, req *types.QueryUniversalValidatorsSetRequest) (*types.QueryUniversalValidatorsSetResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	var validators []*types.UniversalValidator
	err := k.Keeper.UniversalValidatorSet.Walk(ctx, nil, func(addr sdk.ValAddress, val types.UniversalValidator) (stop bool, err error) {
		validators = append(validators, &val)
		return false, nil
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to walk UniversalValidatorSet: %v", err)
	}

	return &types.QueryUniversalValidatorsSetResponse{
		UniversalValidator: validators,
	}, nil
}

func (k Querier) UniversalValidator(goCtx context.Context, req *types.QueryUniversalValidatorRequest) (*types.QueryUniversalValidatorResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if req == nil || req.CoreValidatorAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "core validator address is required")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.CoreValidatorAddress)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address: %v", err)
	}

	val, err := k.Keeper.UniversalValidatorSet.Get(ctx, valAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "universal validator %s not found", req.CoreValidatorAddress)
		}
		return nil, status.Errorf(codes.Internal, "failed to fetch validator: %v", err)
	}

	return &types.QueryUniversalValidatorResponse{
		UniversalValidator: &val,
	}, nil
}

// Ballot implements types.QueryServer.
func (k Querier) Ballot(goCtx context.Context, req *types.QueryBallotRequest) (*types.QueryBallotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Fetch ballot by ID from collections
	ballot, err := k.Keeper.Ballots.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "ballot %s not found", req.Id)
		}
		return nil, status.Errorf(codes.Internal, "failed to get ballot: %v", err)
	}

	return &types.QueryBallotResponse{
		Ballot: &ballot,
	}, nil
}

// AllBallots implements types.QueryServer.
func (k Querier) AllBallots(goCtx context.Context, req *types.QueryBallotsRequest) (*types.QueryBallotsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Use CollectionPaginate
	ballots, pageRes, err := query.CollectionPaginate(ctx, k.Ballots, req.Pagination,
		func(key string, ballot types.Ballot) (*types.Ballot, error) {
			return &ballot, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryBallotsResponse{
		Ballots:    ballots,
		Pagination: pageRes,
	}, nil
}

// AllActiveBallotIDs implements types.QueryServer.
func (k Keeper) AllActiveBallotIDs(goCtx context.Context, req *types.QueryActiveBallotIDsRequest) (*types.QueryActiveBallotIDsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	ids, pageRes, err := query.CollectionPaginate(ctx, k.ActiveBallotIDs, req.Pagination,
		func(id string, _ collections.NoValue) (string, error) {
			return id, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryActiveBallotIDsResponse{
		Ids:        ids,     // []string
		Pagination: pageRes, // PageResponse
	}, nil
}

// ActiveBallots implements types.QueryServer.
func (k Querier) AllActiveBallots(goCtx context.Context, req *types.QueryActiveBallotsRequest) (*types.QueryActiveBallotsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Paginate over ActiveBallotIDs and resolve full Ballot objects
	res, pageRes, err := query.CollectionPaginate(ctx, k.ActiveBallotIDs, req.Pagination,
		func(id string, _ collections.NoValue) (*types.Ballot, error) {
			ballot, err := k.Ballots.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return &ballot, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryActiveBallotsResponse{
		Ballots:    res,     // []*types.Ballot
		Pagination: pageRes, // PageResponse
	}, nil
}

// AllExpiredBallotIDs implements types.QueryServer.
func (k Querier) AllExpiredBallotIDs(goCtx context.Context, req *types.QueryExpiredBallotIDsRequest) (*types.QueryExpiredBallotIDsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	ids, pageRes, err := query.CollectionPaginate(ctx, k.ExpiredBallotIDs, req.Pagination,
		func(id string, _ collections.NoValue) (string, error) {
			return id, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryExpiredBallotIDsResponse{
		Ids:        ids,     // []string
		Pagination: pageRes, // PageResponse
	}, nil
}

// AllExpiredBallots implements types.QueryServer.
func (k Querier) AllExpiredBallots(goCtx context.Context, req *types.QueryExpiredBallotsRequest) (*types.QueryExpiredBallotsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Paginate over ExpiredBallotIDs and resolve full Ballot objects
	res, pageRes, err := query.CollectionPaginate(ctx, k.ExpiredBallotIDs, req.Pagination,
		func(id string, _ collections.NoValue) (*types.Ballot, error) {
			ballot, err := k.Ballots.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return &ballot, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryExpiredBallotsResponse{
		Ballots:    res,     // []*types.Ballot
		Pagination: pageRes, // PageResponse
	}, nil
}

// FinalizedBallotIDs implements types.QueryServer.
func (k Querier) AllFinalizedBallotIDs(goCtx context.Context, req *types.QueryFinalizedBallotIDsRequest) (*types.QueryFinalizedBallotIDsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	ids, pageRes, err := query.CollectionPaginate(ctx, k.FinalizedBallotIDs, req.Pagination,
		func(id string, _ collections.NoValue) (string, error) {
			return id, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryFinalizedBallotIDsResponse{
		Ids:        ids,     // []string
		Pagination: pageRes, // PageResponse
	}, nil
}

// FinalizedBallots implements types.QueryServer.
func (k Querier) AllFinalizedBallots(goCtx context.Context, req *types.QueryFinalizedBallotsRequest) (*types.QueryFinalizedBallotsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Paginate over FinalizedBallotIDs and resolve full Ballot objects
	res, pageRes, err := query.CollectionPaginate(ctx, k.FinalizedBallotIDs, req.Pagination,
		func(id string, _ collections.NoValue) (*types.Ballot, error) {
			ballot, err := k.Ballots.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return &ballot, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryFinalizedBallotsResponse{
		Ballots:    res,     // []*types.Ballot
		Pagination: pageRes, // PageResponse
	}, nil
}
