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

// RetryReadExpiry implements types.MsgServer — the admin escape hatch.
//
// Only reaches records already at ABORTED, which is a state nothing else can leave:
// the sweeper skips it (terminal, so out of PendingByExpiry) and the contract's
// expireExternalRead admits only this module. Without this path the funder's refund
// stays uncredited permanently.
func (ms msgServer) RetryReadExpiry(ctx context.Context, msg *types.MsgRetryReadExpiry) (*types.MsgRetryReadExpiryResponse, error) {
	ms.k.Logger().Info("msg: RetryReadExpiry", "signer", msg.Signer, "request_id", msg.RequestId)

	admin, err := ms.k.uvalidatorKeeper.GetAdmin(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read uvalidator admin")
	}
	if admin != msg.Signer {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner,
			"invalid admin; expected %s, got %s", admin, msg.Signer)
	}

	if msg.RequestId == "" {
		return nil, fmt.Errorf("request_id is required")
	}

	ur, found := ms.k.GetUniversalRead(ctx, msg.RequestId)
	if !found {
		return nil, fmt.Errorf("read request not found: %s", msg.RequestId)
	}

	// Deliberately narrow. Any other status either settled cleanly or is still
	// moving on its own, and re-running expiry on it would call the contract for a
	// request the chain has no business closing.
	if ur.Status != types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED {
		return nil, fmt.Errorf("read request %s is %s, only ABORTED requests can be retried",
			msg.RequestId, ur.Status)
	}

	if err := ms.k.ExpireRead(sdk.UnwrapSDKContext(ctx), ur); err != nil {
		return nil, err
	}

	// ExpireRead leaves it EXPIRED on success and back at ABORTED on failure, with
	// ErrorMsg refreshed either way.
	after, _ := ms.k.GetUniversalRead(ctx, msg.RequestId)
	settled := after.Status == types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED

	ms.k.Logger().Info("admin retry of read expiry",
		"request_id", msg.RequestId, "settled", settled, "status", after.Status.String())

	return &types.MsgRetryReadExpiryResponse{Settled: settled}, nil
}
