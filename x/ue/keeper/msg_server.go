package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
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

	err := ms.k.UpdateParams(ctx, msg.Params)
	if err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// DeployUEA handles the deployment of new Smart Account (UEA).
func (ms msgServer) DeployUEA(ctx context.Context, msg *types.MsgDeployUEA) (*types.MsgDeployUEAResponse, error) {
	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	sa, err := ms.k.DeployUEA(ctx, evmFromAddress, msg.UniversalAccountId, msg.TxHash)
	if err != nil {
		return nil, err
	}

	return &types.MsgDeployUEAResponse{
		UEA: sa,
	}, nil
}

// MintPC handles token minting to the user's UEA for the tokens locked on source chain.
func (ms msgServer) MintPC(ctx context.Context, msg *types.MsgMintPC) (*types.MsgMintPCResponse, error) {
	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	err = ms.k.MintPC(ctx, evmFromAddress, msg.UniversalAccountId, msg.TxHash)
	if err != nil {
		return nil, err
	}

	return &types.MsgMintPCResponse{}, nil
}

// ExecutePayload handles universal payload execution on the UEA.
func (ms msgServer) ExecutePayload(ctx context.Context, msg *types.MsgExecutePayload) (*types.MsgExecutePayloadResponse, error) {
	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	err = ms.k.ExecutePayload(ctx, evmFromAddress, msg.UniversalAccountId, msg.UniversalPayload, msg.VerificationData)
	if err != nil {
		return nil, err
	}

	return &types.MsgExecutePayloadResponse{}, nil
}

// VoteInboundSynthetic implements types.MsgServer.
func (ms msgServer) VoteInboundSynthetic(ctx context.Context, msg *types.MsgVoteInboundSynthetic) (*types.MsgVoteInboundSyntheticResponse, error) {
	// Get the signer address
	signerAddr := msg.Signer

	// Lookup the linked universal validator for this signer
	isBonded, err := ms.k.uvalidatorKeeper.IsBondedUniversalValidator(ctx, signerAddr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check bonded status for signer %s", signerAddr)
	}
	if !isBonded {
		return nil, fmt.Errorf("universal validator for signer %s is not bonded", signerAddr)
	}

	isTombstoned, err := ms.k.uvalidatorKeeper.IsTombstonedUniversalValidator(ctx, signerAddr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check tombstoned status for signer %s", signerAddr)
	}
	if isTombstoned {
		return nil, fmt.Errorf("universal validator for signer %s is tombstoned", signerAddr)
	}

	// continue with inbound synthetic creation / voting logic here

	return &types.MsgVoteInboundSyntheticResponse{}, nil
}
