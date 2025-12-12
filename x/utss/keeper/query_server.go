package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/pushchain/push-chain-node/x/utss/types"
)

var _ types.QueryServer = Querier{}

type Querier struct {
	Keeper
}

func NewQuerier(keeper Keeper) Querier {
	return Querier{Keeper: keeper}
}

// ---------------- Params ------------------
func (k Querier) Params(c context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	p, err := k.Keeper.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryParamsResponse{Params: &p}, nil
}

// ---------------- Current TSS Process ------------------
func (k Querier) CurrentProcess(goCtx context.Context, req *types.QueryCurrentProcessRequest) (*types.QueryCurrentProcessResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	process, err := k.Keeper.CurrentTssProcess.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryCurrentProcessResponse{
		Process: &process,
	}, nil
}

// ---------------- Process By ID ------------------------
func (k Querier) ProcessById(goCtx context.Context, req *types.QueryProcessByIdRequest) (*types.QueryProcessByIdResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	process, err := k.Keeper.ProcessHistory.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	return &types.QueryProcessByIdResponse{
		Process: &process,
	}, nil
}

// ---------------- All Processes (Paginated) -------------
func (k Querier) AllProcesses(goCtx context.Context, req *types.QueryAllProcessesRequest) (*types.QueryAllProcessesResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	results, pageRes, err := query.CollectionPaginate(
		ctx,
		k.Keeper.ProcessHistory,
		req.Pagination,
		func(id uint64, process types.TssKeyProcess) (*types.TssKeyProcess, error) {
			p := process
			return &p, nil // return transformed object
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllProcessesResponse{
		Processes:  results,
		Pagination: pageRes,
	}, nil
}

// ---------------- Current TSS Key -----------------------
func (k Querier) CurrentKey(goCtx context.Context, req *types.QueryCurrentKeyRequest) (*types.QueryCurrentKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	key, err := k.Keeper.CurrentTssKey.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &types.QueryCurrentKeyResponse{
		Key: &key,
	}, nil
}

// ---------------- Key By ID -----------------------------
func (k Querier) KeyById(goCtx context.Context, req *types.QueryKeyByIdRequest) (*types.QueryKeyByIdResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	key, err := k.Keeper.TssKeyHistory.Get(ctx, req.KeyId)
	if err != nil {
		return nil, err
	}

	return &types.QueryKeyByIdResponse{
		Key: &key,
	}, nil
}

// ---------------- All Keys (Paginated) -------------------
func (k Querier) AllKeys(goCtx context.Context, req *types.QueryAllKeysRequest) (*types.QueryAllKeysResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	results, pageRes, err := query.CollectionPaginate(
		ctx,
		k.Keeper.TssKeyHistory,
		req.Pagination,
		func(id string, key types.TssKey) (*types.TssKey, error) {
			kcopy := key
			return &kcopy, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllKeysResponse{
		Keys:       results,
		Pagination: pageRes,
	}, nil
}
