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

// GetPendingTssEvent returns a single pending TSS event by process ID.
func (k Querier) GetPendingTssEvent(goCtx context.Context, req *types.QueryGetPendingTssEventRequest) (*types.QueryGetPendingTssEventResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	eventId, err := k.Keeper.PendingTssEvents.Get(ctx, req.ProcessId)
	if err != nil {
		return nil, err
	}

	event, err := k.Keeper.TssEvents.Get(ctx, eventId)
	if err != nil {
		return nil, err
	}

	return &types.QueryGetPendingTssEventResponse{
		Event: &event,
	}, nil
}

// AllPendingTssEvents returns all pending TSS events (paginated).
// Uses pagination.reverse for descending order (default: ascending by process_id).
func (k Querier) AllPendingTssEvents(goCtx context.Context, req *types.QueryAllPendingTssEventsRequest) (*types.QueryAllPendingTssEventsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	results, pageRes, err := query.CollectionPaginate(
		ctx,
		k.Keeper.PendingTssEvents,
		req.Pagination,
		func(processId uint64, eventId uint64) (*types.TssEvent, error) {
			event, err := k.Keeper.TssEvents.Get(ctx, eventId)
			if err != nil {
				return nil, err
			}
			return &event, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllPendingTssEventsResponse{
		Events:     results,
		Pagination: pageRes,
	}, nil
}

// GetFundMigration implements types.QueryServer.
func (k Querier) GetFundMigration(goCtx context.Context, req *types.QueryGetFundMigrationRequest) (*types.QueryGetFundMigrationResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	migration, err := k.Keeper.FundMigrations.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	return &types.QueryGetFundMigrationResponse{Migration: &migration}, nil
}

// PendingFundMigrations implements types.QueryServer.
func (k Querier) PendingFundMigrations(goCtx context.Context, req *types.QueryPendingFundMigrationsRequest) (*types.QueryPendingFundMigrationsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	var migrations []*types.FundMigration
	err := k.Keeper.PendingMigrations.Walk(ctx, nil, func(migrationId uint64, _ uint64) (bool, error) {
		m, err := k.Keeper.FundMigrations.Get(ctx, migrationId)
		if err != nil {
			return true, err
		}
		migrations = append(migrations, &m)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryPendingFundMigrationsResponse{Migrations: migrations}, nil
}

// AllFundMigrations implements types.QueryServer.
func (k Querier) AllFundMigrations(goCtx context.Context, req *types.QueryAllFundMigrationsRequest) (*types.QueryAllFundMigrationsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	results, pageRes, err := query.CollectionPaginate(
		ctx,
		k.Keeper.FundMigrations,
		req.Pagination,
		func(id uint64, migration types.FundMigration) (*types.FundMigration, error) {
			m := migration
			return &m, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryAllFundMigrationsResponse{
		Migrations: results,
		Pagination: pageRes,
	}, nil
}
