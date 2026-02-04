package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
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

// GetUniversalTx implements types.QueryServer.
func (k Querier) GetUniversalTx(goCtx context.Context, req *types.QueryGetUniversalTxRequest) (*types.QueryGetUniversalTxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Fetch from the collection
	currentTx, err := k.UniversalTx.Get(ctx, req.Id) // req.Id is the string key
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "UniversalTx not found")
		}
		return nil, err
	}

	legacyTx := convertToUniversalTxLegacy(&currentTx)

	return &types.QueryGetUniversalTxResponse{
		UniversalTx: legacyTx,
	}, nil
}

// convertToUniversalTxLegacy maps the current (post-upgrade) UniversalTx to the legacy shape
func convertToUniversalTxLegacy(current *types.UniversalTx) *types.UniversalTxLegacy {
	if current == nil {
		return nil
	}

	// Inbound conversion
	var legacyInbound *types.InboundLegacy
	if current.InboundTx != nil {
		legacyInbound = &types.InboundLegacy{
			SourceChain:      current.InboundTx.SourceChain,
			TxHash:           current.InboundTx.TxHash,
			Sender:           current.InboundTx.Sender,
			Recipient:        current.InboundTx.Recipient,
			Amount:           current.InboundTx.Amount,
			AssetAddr:        current.InboundTx.AssetAddr,
			LogIndex:         current.InboundTx.LogIndex,
			TxType:           mapTxTypeToLegacy(current.InboundTx.TxType),
			UniversalPayload: current.InboundTx.UniversalPayload,
			VerificationData: current.InboundTx.VerificationData,
		}
	}

	pcTxs := current.PcTx // repeated PCTx unchanged

	var legacyOutbound *types.OutboundTxLegacy
	if len(current.OutboundTx) > 0 {
		first := current.OutboundTx[0]
		if first != nil {
			var observedTxHash string
			if first.ObservedTx != nil {
				observedTxHash = first.ObservedTx.TxHash
			} else {
			}

			legacyOutbound = &types.OutboundTxLegacy{
				DestinationChain: first.DestinationChain,
				TxHash:           observedTxHash,
				Recipient:        first.Recipient,
				Amount:           first.Amount,
				AssetAddr:        first.Prc20AssetAddr,
			}
		}
	}

	// Build legacy UniversalTx
	return &types.UniversalTxLegacy{
		InboundTx:       legacyInbound,
		PcTx:            pcTxs,
		OutboundTx:      legacyOutbound,
		UniversalStatus: current.UniversalStatus,
	}
}

// mapTxTypeToLegacy maps current TxType → legacy InboundTxTypeLegacy
func mapTxTypeToLegacy(current types.TxType) types.InboundTxTypeLegacy {
	switch current {
	case types.TxType_UNSPECIFIED_TX:
		return types.InboundTxTypeLegacy_INBOUND_LEGACY_UNSPECIFIED_TX
	case types.TxType_GAS:
		return types.InboundTxTypeLegacy_INBOUND_LEGACY_GAS
	case types.TxType_GAS_AND_PAYLOAD:
		return types.InboundTxTypeLegacy_INBOUND_LEGACY_GAS_AND_PAYLOAD
	case types.TxType_FUNDS:
		return types.InboundTxTypeLegacy_INBOUND_LEGACY_FUNDS
	case types.TxType_FUNDS_AND_PAYLOAD:
		return types.InboundTxTypeLegacy_INBOUND_LEGACY_FUNDS_AND_PAYLOAD

	case types.TxType_PAYLOAD, types.TxType_INBOUND_REVERT:
		return types.InboundTxTypeLegacy_INBOUND_LEGACY_UNSPECIFIED_TX

	default:
		return types.InboundTxTypeLegacy_INBOUND_LEGACY_UNSPECIFIED_TX
	}
}

// AllUniversalTx implements types.QueryServer.
func (k Querier) AllUniversalTx(goCtx context.Context, req *types.QueryAllUniversalTxRequest) (*types.QueryAllUniversalTxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	var txs []*types.UniversalTx

	items, pageRes, err := query.CollectionPaginate(ctx, k.UniversalTx, req.Pagination, func(key string, value types.UniversalTx) (*types.UniversalTx, error) {
		return &value, nil
	})
	if err != nil {
		return nil, err
	}

	txs = append(txs, items...)

	return &types.QueryAllUniversalTxResponse{
		UniversalTxs: txs,
		Pagination:   pageRes,
	}, nil
}

// AllPendingInbounds implements types.QueryServer.
func (k Keeper) AllPendingInbounds(goCtx context.Context, req *types.QueryAllPendingInboundsRequest) (*types.QueryAllPendingInboundsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	inbounds, pageRes, err := query.CollectionPaginate(ctx, k.PendingInbounds, req.Pagination, func(key string, _ collections.NoValue) (string, error) {
		return key, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllPendingInboundsResponse{
		InboundIds: inbounds,
		Pagination: pageRes,
	}, nil
}

// GasPrice implements types.QueryServer.
func (k Querier) GasPrice(goCtx context.Context, req *types.QueryGasPriceRequest) (*types.QueryGasPriceResponse, error) {
	if req == nil || req.ChainId == "" {
		return nil, status.Error(codes.InvalidArgument, "chain_id is required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Try to fetch the gas price from keeper
	gasPrice, err := k.GasPrices.Get(ctx, req.ChainId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "no gas price found for chain_id: %s", req.ChainId)
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryGasPriceResponse{
		GasPrice: &gasPrice,
	}, nil
}

// AllGasPrices implements types.QueryServer.
func (k Querier) AllGasPrices(goCtx context.Context, req *types.QueryAllGasPricesRequest) (*types.QueryAllGasPricesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	var all []*types.GasPrice

	items, pageRes, err := query.CollectionPaginate(ctx, k.GasPrices, req.Pagination, func(key string, value types.GasPrice) (*types.GasPrice, error) {
		return &value, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	all = append(all, items...)

	return &types.QueryAllGasPricesResponse{
		GasPrices:  all,
		Pagination: pageRes,
	}, nil
}
