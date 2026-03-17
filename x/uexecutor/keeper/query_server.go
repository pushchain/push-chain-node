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

// computeUniversalStatus derives a UniversalTxStatus from the actual state of the
// UTX's components instead of reading a stored field that can go stale.
//
// Priority: outbounds > PC txs > inbound presence.
func computeUniversalStatus(utx *types.UniversalTx) types.UniversalTxStatus {
	if len(utx.OutboundTx) > 0 {
		anyPending := false
		anyReverted := false
		for _, ob := range utx.OutboundTx {
			if ob == nil {
				continue
			}
			switch ob.OutboundStatus {
			case types.Status_PENDING:
				anyPending = true
			case types.Status_REVERTED:
				anyReverted = true
			}
		}
		if anyPending {
			return types.UniversalTxStatus_OUTBOUND_PENDING
		}
		if anyReverted {
			return types.UniversalTxStatus_OUTBOUND_FAILED
		}
		return types.UniversalTxStatus_OUTBOUND_SUCCESS
	}

	if len(utx.PcTx) > 0 {
		for _, pc := range utx.PcTx {
			if pc != nil && pc.Status == "FAILED" {
				return types.UniversalTxStatus_PC_EXECUTED_FAILED
			}
		}
		return types.UniversalTxStatus_PC_EXECUTED_SUCCESS
	}

	if utx.InboundTx != nil {
		return types.UniversalTxStatus_PENDING_INBOUND_EXECUTION
	}

	return types.UniversalTxStatus_UNIVERSAL_TX_STATUS_UNSPECIFIED
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
		UniversalStatus: computeUniversalStatus(current),
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
// Sources data from ChainMetas (the new unified store) to maintain backward compatibility.
func (k Querier) GasPrice(goCtx context.Context, req *types.QueryGasPriceRequest) (*types.QueryGasPriceResponse, error) {
	if req == nil || req.ChainId == "" {
		return nil, status.Error(codes.InvalidArgument, "chain_id is required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Source from ChainMetas first (preferred post-upgrade storage)
	cm, err := k.ChainMetas.Get(ctx, req.ChainId)
	if err == nil {
		return &types.QueryGasPriceResponse{
			GasPrice: chainMetaToGasPrice(&cm),
		}, nil
	}
	if !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Fallback to legacy GasPrices store (pre-upgrade nodes)
	gasPrice, err := k.GasPrices.Get(ctx, req.ChainId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "no gas price found for chain_id: %s", req.ChainId)
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryGasPriceResponse{GasPrice: &gasPrice}, nil
}

// AllGasPrices implements types.QueryServer.
// Sources data from ChainMetas (the new unified store) to maintain backward compatibility.
// Falls back to the legacy GasPrices store for entries not yet migrated.
func (k Querier) AllGasPrices(goCtx context.Context, req *types.QueryAllGasPricesRequest) (*types.QueryAllGasPricesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	var all []*types.GasPrice

	// Primary: read from ChainMetas
	items, pageRes, err := query.CollectionPaginate(ctx, k.ChainMetas, req.Pagination, func(_ string, value types.ChainMeta) (*types.GasPrice, error) {
		return chainMetaToGasPrice(&value), nil
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

// ChainMeta implements types.QueryServer.
// Returns the aggregated chain metadata (gas price + chain height) for a specific chain.
func (k Querier) ChainMeta(goCtx context.Context, req *types.QueryChainMetaRequest) (*types.QueryChainMetaResponse, error) {
	if req == nil || req.ChainId == "" {
		return nil, status.Error(codes.InvalidArgument, "chain_id is required")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	cm, err := k.ChainMetas.Get(ctx, req.ChainId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "no chain meta found for chain_id: %s", req.ChainId)
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryChainMetaResponse{ChainMeta: &cm}, nil
}

// AllChainMetas implements types.QueryServer.
// Returns paginated chain meta entries for all registered chains.
func (k Querier) AllChainMetas(goCtx context.Context, req *types.QueryAllChainMetasRequest) (*types.QueryAllChainMetasResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	items, pageRes, err := query.CollectionPaginate(ctx, k.ChainMetas, req.Pagination, func(_ string, value types.ChainMeta) (*types.ChainMeta, error) {
		return &value, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllChainMetasResponse{
		ChainMetas: items,
		Pagination: pageRes,
	}, nil
}

// chainMetaToGasPrice converts a ChainMeta into the legacy GasPrice shape
// so that existing API consumers see no breaking change.
func chainMetaToGasPrice(cm *types.ChainMeta) *types.GasPrice {
	return &types.GasPrice{
		ObservedChainId: cm.ObservedChainId,
		Signers:         cm.Signers,
		BlockNums:       cm.ChainHeights,
		Prices:          cm.Prices,
		MedianIndex:     cm.MedianIndex,
	}
}
