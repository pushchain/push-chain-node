package keeper

import (
	"context"

	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	"github.com/pushchain/push-chain-node/x/uregistry/types"
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

// AddChainConfig enables the addition of a new chain configuration - Admin restricted.
func (ms msgServer) AddChainConfig(ctx context.Context, msg *types.MsgAddChainConfig) (*types.MsgAddChainConfigResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	err = ms.k.AddChainConfig(ctx, msg.ChainConfig)
	if err != nil {
		return nil, err
	}

	return &types.MsgAddChainConfigResponse{}, nil
}

// UpdateChainConfig enables the update of an existing chain configuration - Admin restricted.
func (ms msgServer) UpdateChainConfig(ctx context.Context, msg *types.MsgUpdateChainConfig) (*types.MsgUpdateChainConfigResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	err = ms.k.UpdateChainConfig(ctx, msg.ChainConfig)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateChainConfigResponse{}, nil
}

// AddTokenConfig implements types.MsgServer.
func (ms msgServer) AddTokenConfig(ctx context.Context, msg *types.MsgAddTokenConfig) (*types.MsgAddTokenConfigResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	err = ms.k.AddTokenConfig(ctx, msg.TokenConfig)
	if err != nil {
		return nil, err
	}

	return &types.MsgAddTokenConfigResponse{}, nil
}

// UpdateTokenConfig implements types.MsgServer.
func (ms msgServer) UpdateTokenConfig(ctx context.Context, msg *types.MsgUpdateTokenConfig) (*types.MsgUpdateTokenConfigResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	err = ms.k.UpdateTokenConfig(ctx, msg.TokenConfig)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateTokenConfigResponse{}, nil
}

// UpdateSystemConfig implements types.MsgServer.
func (ms msgServer) UpdateSystemConfig(ctx context.Context, msg *types.MsgUpdateSystemConfig) (*types.MsgUpdateSystemConfigResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	// Update system config
	if err := ms.k.SetSystemConfig(ctx, *msg.SystemConfig); err != nil {
		return nil, err
	}

	return &types.MsgUpdateSystemConfigResponse{}, nil
}
