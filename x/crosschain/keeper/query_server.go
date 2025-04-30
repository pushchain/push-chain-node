package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollchains/pchain/x/crosschain/types"
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

// FactoryAddress implements types.QueryServer.
func (k Querier) FactoryAddress(goCtx context.Context, req *types.QueryFactoryAddressRequest) (*types.QueryFactoryAddressResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("FactoryAddress is unimplemented")
	return &types.QueryFactoryAddressResponse{}, nil
}

// VerifierPrecompile implements types.QueryServer.
func (k Querier) VerifierPrecompile(goCtx context.Context, req *types.QueryVerifierPrecompileRequest) (*types.QueryVerifierPrecompileResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("VerifierPrecompile is unimplemented")
	return &types.QueryVerifierPrecompileResponse{}, nil
}
