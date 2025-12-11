package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
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

// AddUniversalValidator implements types.MsgServer.
func (ms msgServer) AddUniversalValidator(ctx context.Context, msg *types.MsgAddUniversalValidator) (*types.MsgAddUniversalValidatorResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	err = ms.k.AddUniversalValidator(ctx, msg.CoreValidatorAddress, *msg.Network)
	if err != nil {
		return nil, err
	}

	return &types.MsgAddUniversalValidatorResponse{}, nil
}

// RemoveUniversalValidator implements types.MsgServer.
func (ms msgServer) RemoveUniversalValidator(ctx context.Context, msg *types.MsgRemoveUniversalValidator) (*types.MsgRemoveUniversalValidatorResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	err = ms.k.RemoveUniversalValidator(ctx, msg.CoreValidatorAddress)
	if err != nil {
		return nil, err
	}

	return &types.MsgRemoveUniversalValidatorResponse{}, nil
}

// UpdateUniversalValidator implements types.MsgServer.
func (ms msgServer) UpdateUniversalValidator(ctx context.Context, msg *types.MsgUpdateUniversalValidator) (*types.MsgUpdateUniversalValidatorResponse, error) {
	// Parse signer account
	signerAcc, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidAddress, "invalid signer address: %s", msg.Signer)
	}

	// Find validator controlled by this account
	valAddr := sdk.ValAddress(signerAcc)

	validator, err := ms.k.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return nil, errors.Wrap(err, "signer is not a validator operator")
	}

	err = ms.k.UpdateUniversalValidator(ctx, validator.OperatorAddress, *msg.Network)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateUniversalValidatorResponse{}, nil
}

// UpdateUniversalValidatorStatus implements types.MsgServer.
func (ms msgServer) UpdateUniversalValidatorStatus(ctx context.Context, msg *types.MsgUpdateUniversalValidatorStatus) (*types.MsgUpdateUniversalValidatorStatusResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	if params.Admin != msg.Signer {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid authority; expected %s, got %s", params.Admin, msg.Signer)
	}

	err = ms.k.UpdateUniversalValidatorStatus(ctx, msg.CoreValidatorAddress, msg.NewStatus)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateUniversalValidatorStatusResponse{}, nil
}
