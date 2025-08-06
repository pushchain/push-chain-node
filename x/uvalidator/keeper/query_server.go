package keeper

import (
	"context"

	"errors"

	"github.com/rollchains/pchain/x/uvalidator/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
func (k Querier) UniversalValidatorByCore(goCtx context.Context, req *types.QueryUniversalValidatorByCoreRequest) (*types.QueryUniversalValidatorByCoreResponse, error) {
	if req == nil || req.CoreValidatorAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "core validator address is required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	uvAddr, err := k.Keeper.CoreToUniversal.Get(ctx, req.CoreValidatorAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "universal validator not found for this core validator")
		}
		return nil, status.Errorf(codes.Internal, "failed to get universal validator: %v", err)
	}

	return &types.QueryUniversalValidatorByCoreResponse{
		UniversalValidator: uvAddr,
	}, nil
}

func (k Querier) CoreValidatorByUniversal(goCtx context.Context, req *types.QueryCoreValidatorByUniversalRequest) (*types.QueryCoreValidatorByUniversalResponse, error) {
	if req == nil || req.UniversalValidatorAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "universal validator address is required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	var coreAddr string
	found := false

	err := k.Keeper.CoreToUniversal.Walk(ctx, nil, func(key string, value string) (stop bool, err error) {
		if value == req.UniversalValidatorAddress {
			coreAddr = key
			found = true
			return true, nil // stop iteration
		}
		return false, nil
	})

	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to walk CoreToUniversal: %v", err)
	}
	if !found {
		return nil, status.Error(codes.NotFound, "core validator not found for this universal validator")
	}

	return &types.QueryCoreValidatorByUniversalResponse{
		CoreValidatorAddress: coreAddr,
	}, nil
}

func (k Querier) AllUniversalValidators(goCtx context.Context, req *types.QueryUniversalValidatorsSetRequest) (*types.QueryUniversalValidatorsSetResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	var validators []string
	err := k.Keeper.UniversalValidatorSet.Walk(ctx, nil, func(addr string) (stop bool, err error) {
		validators = append(validators, addr)
		return false, nil
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to walk UniversalValidatorSet: %v", err)
	}

	return &types.QueryUniversalValidatorsSetResponse{
		Addresses: validators,
	}, nil
}
