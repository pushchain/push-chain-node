package keeper

import (
	"context"

	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	"github.com/pushchain/push-chain-node/x/utss/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns an implementation of the module MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// UpdateParams handles MsgUpdateParams for updating module parameters.
// Only authorized governance account can execute this.
func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != msg.Authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", ms.k.authority, msg.Authority)
	}

	err := ms.k.UpdateParams(ctx, msg.Params)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// InitiateTssKeyProcess implements types.MsgServer.
func (ms msgServer) InitiateTssKeyProcess(ctx context.Context, msg *types.MsgInitiateTssKeyProcess) (*types.MsgInitiateTssKeyProcessResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("InitiateTssKeyProcess is unimplemented")
	return &types.MsgInitiateTssKeyProcessResponse{}, nil
}

// VoteTssKeyProcess implements types.MsgServer.
func (ms msgServer) VoteTssKeyProcess(ctx context.Context, msg *types.MsgVoteTssKeyProcess) (*types.MsgVoteTssKeyProcessResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("VoteTssKeyProcess is unimplemented")
	return &types.MsgVoteTssKeyProcessResponse{}, nil
}
