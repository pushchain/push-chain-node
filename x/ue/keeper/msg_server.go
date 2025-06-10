package keeper

import (
	"context"

	"cosmossdk.io/errors"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/rollchains/pchain/utils"
	"github.com/rollchains/pchain/x/ue/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns an implementation of the module MsgServer interface
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// UpdateParams handles MsgUpdateParams for updating module parameters.
// Only authorized governance account can execute this.
func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != msg.Authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", ms.k.authority, msg.Authority)
	}

	err := ms.k.updateParams(ctx, msg.Params)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// DeployNMSC handles the deployment of new Smart Account (NMSC).
func (ms msgServer) DeployNMSC(ctx context.Context, msg *types.MsgDeployNMSC) (*types.MsgDeployNMSCResponse, error) {
	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	sa, err := ms.k.deployNMSC(ctx, evmFromAddress, msg.AccountId, msg.TxHash)
	if err != nil {
		return nil, err
	}

	return &types.MsgDeployNMSCResponse{
		SmartAccount: sa,
	}, nil
}

// MintPush handles token minting to the user's NMSC for the tokens locked on source chain.
func (ms msgServer) MintPush(ctx context.Context, msg *types.MsgMintPush) (*types.MsgMintPushResponse, error) {
	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	err = ms.k.mintPush(ctx, evmFromAddress, msg.AccountId, msg.TxHash)
	if err != nil {
		return nil, err
	}

	return &types.MsgMintPushResponse{}, nil
}

// ExecutePayload handles cross-chain payload execution on the NMSC.
func (ms msgServer) ExecutePayload(ctx context.Context, msg *types.MsgExecutePayload) (*types.MsgExecutePayloadResponse, error) {
	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	err = ms.k.executePayload(ctx, evmFromAddress, msg.AccountId, msg.CrosschainPayload, msg.Signature)
	if err != nil {
		return nil, err
	}

	return &types.MsgExecutePayloadResponse{}, nil
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

	err = ms.k.addChainConfig(ctx, msg.ChainConfig)
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

	err = ms.k.updateChainConfig(ctx, msg.ChainConfig)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateChainConfigResponse{}, nil
}
