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

// VerifiedTx returns if the txHash has already been verified on the chain.
func (q Querier) VerifiedTxHash(
	goCtx context.Context,
	req *types.QueryVerifiedTxHashRequest,
) (*types.QueryVerifiedTxHashResponse, error) {
	if req.Chain == "" || req.TxHash == "" {
		return nil, fmt.Errorf("chain_id and tx_hash are required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// check verification status
	isVerified, err := q.Keeper.IsTxHashVerified(ctx, req.Chain, req.TxHash)
	if err != nil {
		return nil, err
	}

	return &types.QueryVerifiedTxHashResponse{
		Status: isVerified,
	}, nil
}
