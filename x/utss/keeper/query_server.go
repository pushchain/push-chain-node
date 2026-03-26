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

// ---------------- Get TSS Event by ID --------------------
func (k Querier) GetTssEvent(goCtx context.Context, req *types.QueryGetTssEventRequest) (*types.QueryGetTssEventResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	event, err := k.Keeper.TssEvents.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	return &types.QueryGetTssEventResponse{
		Event: &event,
	}, nil
}

// ---------------- Active TSS Events (Paginated) ----------
func (k Querier) ActiveTssEvents(goCtx context.Context, req *types.QueryActiveTssEventsRequest) (*types.QueryActiveTssEventsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	limit := req.Pagination.GetLimit()
	if limit == 0 {
		limit = query.DefaultLimit
	}
	offset := req.Pagination.GetOffset()

	var events []*types.TssEvent
	var skipped uint64
	var total uint64

	collected := false
	err := k.Keeper.TssEvents.Walk(ctx, nil, func(id uint64, event types.TssEvent) (bool, error) {
		if event.Status != types.TssEventStatus_TSS_EVENT_ACTIVE {
			return false, nil // continue, skip non-active
		}
		total++
		if skipped < offset {
			skipped++
			return false, nil
		}
		if !collected && uint64(len(events)) < limit {
			e := event
			events = append(events, &e)
			if uint64(len(events)) >= limit {
				collected = true
			}
		}
		return false, nil // continue to count total
	})
	if err != nil {
		return nil, err
	}

	pageRes := &query.PageResponse{
		Total: total,
	}

	return &types.QueryActiveTssEventsResponse{
		Events:     events,
		Pagination: pageRes,
	}, nil
}

// ---------------- All TSS Events (Paginated) -------------
func (k Querier) AllTssEvents(goCtx context.Context, req *types.QueryAllTssEventsRequest) (*types.QueryAllTssEventsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	results, pageRes, err := query.CollectionPaginate(
		ctx,
		k.Keeper.TssEvents,
		req.Pagination,
		func(id uint64, event types.TssEvent) (*types.TssEvent, error) {
			e := event
			return &e, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllTssEventsResponse{
		Events:     results,
		Pagination: pageRes,
	}, nil
}
