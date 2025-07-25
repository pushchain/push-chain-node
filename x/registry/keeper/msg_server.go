package keeper

import (
	"context"

	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	"github.com/rollchains/pchain/x/registry/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns an implementation of the module MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != msg.Authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", ms.k.authority, msg.Authority)
	}

	return nil, ms.k.Params.Set(ctx, msg.Params)
}

// AddChainConfig implements types.MsgServer.
func (ms msgServer) AddChainConfig(ctx context.Context, msg *types.MsgAddChainConfig) (*types.MsgAddChainConfigResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("AddChainConfig is unimplemented")
	return &types.MsgAddChainConfigResponse{}, nil
}

// UpdateChainConfig implements types.MsgServer.
func (ms msgServer) UpdateChainConfig(ctx context.Context, msg *types.MsgUpdateChainConfig) (*types.MsgUpdateChainConfigResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("UpdateChainConfig is unimplemented")
	return &types.MsgUpdateChainConfigResponse{}, nil
}
