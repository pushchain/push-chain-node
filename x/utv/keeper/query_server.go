package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollchains/pchain/x/utv/types"
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

// VerifiedTxHash returns the verified metadata (if any) for a given txHash on a chain.
func (q Querier) VerifiedTxHash(
	goCtx context.Context,
	req *types.QueryVerifiedTxHashRequest,
) (*types.QueryVerifiedTxHashResponse, error) {
	if req.Chain == "" || req.TxHash == "" {
		return nil, fmt.Errorf("chain and tx_hash are required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	meta, found, err := q.Keeper.GetVerifiedInboundTxMetadata(ctx, req.Chain, req.TxHash)
	if err != nil {
		return nil, err
	}

	resp := &types.QueryVerifiedTxHashResponse{
		Found: found,
	}
	if found {
		resp.Metadata = meta
	}

	return resp, nil
}
