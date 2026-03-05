package keeper

import (
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	typesv2 "github.com/pushchain/push-chain-node/x/uexecutor/typesv2"
)

var _ typesv2.QueryServer = QuerierV2{}

// QuerierV2 implements the uexecutor.v2 QueryServer, returning native types.
type QuerierV2 struct {
	Keeper
}

func NewQuerierV2(keeper Keeper) QuerierV2 {
	return QuerierV2{Keeper: keeper}
}

// GetUniversalTx implements typesv2.QueryServer.
// Returns the native UniversalTx type (not legacy) for the given ID.
func (k QuerierV2) GetUniversalTx(goCtx context.Context, req *typesv2.QueryGetUniversalTxRequest) (*typesv2.QueryGetUniversalTxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	currentTx, err := k.UniversalTx.Get(ctx, strings.TrimPrefix(req.Id, "0x"))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "UniversalTx not found")
		}
		return nil, err
	}

	return &typesv2.QueryGetUniversalTxResponse{
		UniversalTx: &currentTx,
	}, nil
}
