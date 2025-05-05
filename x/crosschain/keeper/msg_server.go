package keeper

import (
	"context"

	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	"github.com/rollchains/pchain/x/crosschain/types"
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

// // SetFactoryAddress implements types.MsgServer.
// func (ms msgServer) SetFactoryAddress(ctx context.Context, msg *types.MsgSetFactoryAddress) (*types.MsgSetFactoryAddressResponse, error) {
// 	// ctx := sdk.UnwrapSDKContext(goCtx)
// 	panic("SetFactoryAddress is unimplemented")
// 	return &types.MsgSetFactoryAddressResponse{}, nil
// }

// // SetVerifierPrecompile implements types.MsgServer.
// func (ms msgServer) SetVerifierPrecompile(ctx context.Context, msg *types.MsgSetVerifierPrecompile) (*types.MsgSetVerifierPrecompileResponse, error) {
// 	// ctx := sdk.UnwrapSDKContext(goCtx)
// 	panic("SetVerifierPrecompile is unimplemented")
// 	return &types.MsgSetVerifierPrecompileResponse{}, nil
// }

// UpdateAdminParams implements types.MsgServer.
func (ms msgServer) UpdateAdminParams(ctx context.Context, msg *types.MsgUpdateAdminParams) (*types.MsgUpdateAdminParamsResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	// Check if the sender is the admin (from params)
	if params.Admin != msg.Admin {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid admin; expected admin address %s, got %s", params.Admin, msg.Admin)
	}

	return nil, ms.k.AdminParams.Set(ctx, msg.AdminParams)
}
