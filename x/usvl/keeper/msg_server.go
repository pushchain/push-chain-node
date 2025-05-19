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
	BaseErrCode uint32 = 100 // Using a higher base to avoid conflicts with existing error codes
)

var (
	ErrUnauthorized   = sdkerrors.Register("usvl", BaseErrCode, "unauthorized")
	ErrInvalidRequest = sdkerrors.Register("usvl", BaseErrCode+1, "invalid request")
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



// VerifyExternalTransaction handles verification of transactions on external chains
func (ms msgServer) VerifyExternalTransaction(goCtx context.Context, msg *types.MsgVerifyExternalTransaction) (*types.MsgVerifyExternalTransactionResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Verify the transaction using the keeper
	result, err := ms.Keeper.VerifyExternalTransaction(ctx, msg.TxHash, msg.CaipAddress)
	if err != nil {
		return nil, sdkerrors.Wrapf(ErrInvalidRequest, "transaction verification failed: %s", err.Error())
	}

	// Always emit an event with the verification result, even for failed verifications
	event := sdk.NewEvent(
		types.EventTypeExternalTransactionVerified,
		sdk.NewAttribute("tx_hash", msg.TxHash),
		sdk.NewAttribute("caip_address", msg.CaipAddress),
		sdk.NewAttribute("verified", fmt.Sprintf("%t", result.Verified)),
	)

	// Emit the event
	ctx.EventManager().EmitEvent(event)

	// Return the verification result
	return &types.MsgVerifyExternalTransactionResponse{
		Verified: result.Verified,
		TxInfo:   result.TxInfo,
	}, nil
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

// DeleteChainConfig implements types.MsgServer.
func (ms msgServer) DeleteChainConfig(ctx context.Context, msg *types.MsgDeleteChainConfig) (*types.MsgDeleteChainConfigResponse, error) {
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("DeleteChainConfig is unimplemented")
	return &types.MsgDeleteChainConfigResponse{}, nil
}
