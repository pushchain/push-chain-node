package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/push-protocol/push-chain/x/usvl/types"
)

// Error codes for the USVL module
const (
	BaseErrCode uint32 = 1
)

var (
	ErrUnauthorized = sdkerrors.Register("usvl", BaseErrCode, "unauthorized")
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// AddChainConfig adds a new chain configuration
func (ms msgServer) AddChainConfig(goCtx context.Context, msg *types.MsgAddChainConfig) (*types.MsgAddChainConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !ms.hasAuthority(msg.Authority) {
		return nil, sdkerrors.Wrapf(ErrUnauthorized, "unauthorized access: %s", msg.Authority)
	}

	// Convert proto ChainConfig to internal ChainConfigData
	chainConfig := types.ChainConfigDataFromProto(msg.ChainConfig)

	// Check if chain ID already exists
	if has, err := ms.ChainConfigs.Has(ctx, chainConfig.ChainId); err != nil {
		return nil, err
	} else if has {
		return nil, fmt.Errorf("chain config for %s already exists", chainConfig.ChainId)
	}

	// Add the chain configuration
	if err := ms.Keeper.AddChainConfig(ctx, chainConfig); err != nil {
		return nil, err
	}

	// Emit standard event
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeAddChainConfig,
			sdk.NewAttribute("chain_id", chainConfig.ChainId),
			sdk.NewAttribute("caip_prefix", chainConfig.CaipPrefix),
		),
	})

	return &types.MsgAddChainConfigResponse{}, nil
}

// UpdateChainConfig updates an existing chain configuration
func (ms msgServer) UpdateChainConfig(goCtx context.Context, msg *types.MsgUpdateChainConfig) (*types.MsgUpdateChainConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !ms.hasAuthority(msg.Authority) {
		return nil, sdkerrors.Wrapf(ErrUnauthorized, "unauthorized access: %s", msg.Authority)
	}

	// Convert proto ChainConfig to internal ChainConfigData
	chainConfig := types.ChainConfigDataFromProto(msg.ChainConfig)

	// Check if chain ID exists
	if has, err := ms.ChainConfigs.Has(ctx, chainConfig.ChainId); err != nil {
		return nil, err
	} else if !has {
		return nil, fmt.Errorf("chain config for %s does not exist", chainConfig.ChainId)
	}

	// Update the chain configuration
	if err := ms.Keeper.AddChainConfig(ctx, chainConfig); err != nil {
		return nil, err
	}

	// Emit standard event
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeUpdateChainConfig,
			sdk.NewAttribute("chain_id", chainConfig.ChainId),
			sdk.NewAttribute("caip_prefix", chainConfig.CaipPrefix),
		),
	})

	return &types.MsgUpdateChainConfigResponse{}, nil
}

// DeleteChainConfig deletes an existing chain configuration
func (ms msgServer) DeleteChainConfig(goCtx context.Context, msg *types.MsgDeleteChainConfig) (*types.MsgDeleteChainConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !ms.hasAuthority(msg.Authority) {
		return nil, sdkerrors.Wrapf(ErrUnauthorized, "unauthorized access: %s", msg.Authority)
	}

	// Delete the chain configuration
	if err := ms.Keeper.DeleteChainConfig(ctx, msg.ChainId); err != nil {
		return nil, err
	}

	// Emit standard event
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeDeleteChainConfig,
			sdk.NewAttribute("chain_id", msg.ChainId),
		),
	})

	return &types.MsgDeleteChainConfigResponse{}, nil
}

// UpdateParams implements the MsgServer.UpdateParams method.
func (ms msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != ms.GetAuthority() {
		return nil, sdkerrors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", ms.GetAuthority(), msg.Authority)
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	if err := ms.Params.Set(goCtx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// hasAuthority returns true if the provided address is the authority for the module or a gov proposal
func (ms msgServer) hasAuthority(authority string) bool {
	return authority == ms.Keeper.GetAuthority() || authority == authtypes.NewModuleAddress(govtypes.ModuleName).String()
}
