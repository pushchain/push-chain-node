package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
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

// VoteReadResult implements types.MsgServer.
//
// Eligibility is checked here rather than in the keeper: bonded-and-not-tombstoned
// is a property of the signer, and the same two guards front x/uexecutor's vote
// handlers. A validator that has been slashed out must not keep steering ballots.
func (ms msgServer) VoteReadResult(ctx context.Context, msg *types.MsgVoteReadResult) (*types.MsgVoteReadResultResponse, error) {
	if msg.Result == nil {
		return nil, fmt.Errorf("result is required")
	}

	signerAccAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	isBonded, err := ms.k.uvalidatorKeeper.IsBondedUniversalValidator(ctx, msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("failed to check bonded status for signer %s: %w", msg.Signer, err)
	}
	if !isBonded {
		return nil, fmt.Errorf("universal validator for signer %s is not bonded", msg.Signer)
	}

	isTombstoned, err := ms.k.uvalidatorKeeper.IsTombstonedUniversalValidator(ctx, msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("failed to check tombstoned status for signer %s: %w", msg.Signer, err)
	}
	if isTombstoned {
		return nil, fmt.Errorf("universal validator for signer %s is tombstoned", msg.Signer)
	}

	finalized, err := ms.k.VoteReadResult(ctx, sdk.ValAddress(signerAccAddr), msg.RequestId, msg.Result)
	if err != nil {
		return nil, err
	}

	return &types.MsgVoteReadResultResponse{Finalized: finalized}, nil
}
