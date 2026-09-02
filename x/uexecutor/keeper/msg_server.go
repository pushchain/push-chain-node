package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
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

// VoteInbound implements types.MsgServer.
func (ms msgServer) VoteInbound(ctx context.Context, msg *types.MsgVoteInbound) (*types.MsgVoteInboundResponse, error) {
	signerAccAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	// Convert account to validator operator address
	signerValAddr := sdk.ValAddress(signerAccAddr)

	// Lookup the linked universal validator for this signer
	isBonded, err := ms.k.uvalidatorKeeper.IsBondedUniversalValidator(ctx, msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check bonded status for signer %s", msg.Signer)
	}
	if !isBonded {
		return nil, fmt.Errorf("universal validator for signer %s is not bonded", msg.Signer)
	}

	isTombstoned, err := ms.k.uvalidatorKeeper.IsTombstonedUniversalValidator(ctx, msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check tombstoned status for signer %s", msg.Signer)
	}
	if isTombstoned {
		return nil, fmt.Errorf("universal validator for signer %s is tombstoned", msg.Signer)
	}

	// continue with inbound synthetic creation / voting logic here
	err = ms.k.VoteInbound(ctx, signerValAddr, *msg.Inbound)
	if err != nil {
		return nil, err
	}

	return &types.MsgVoteInboundResponse{}, nil
}

// VoteOutbound implements types.MsgServer.
func (ms msgServer) VoteOutbound(ctx context.Context, msg *types.MsgVoteOutbound) (*types.MsgVoteOutboundResponse, error) {
	signerAccAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	// Normalize IDs: strip 0x prefix
	msg.TxId = strings.TrimPrefix(msg.TxId, "0x")
	msg.UtxId = strings.TrimPrefix(msg.UtxId, "0x")

	// Convert account to validator operator address
	signerValAddr := sdk.ValAddress(signerAccAddr)

	// Lookup the linked universal validator for this signer
	isBonded, err := ms.k.uvalidatorKeeper.IsBondedUniversalValidator(ctx, msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check bonded status for signer %s", msg.Signer)
	}
	if !isBonded {
		return nil, fmt.Errorf("universal validator for signer %s is not bonded", msg.Signer)
	}

	isTombstoned, err := ms.k.uvalidatorKeeper.IsTombstonedUniversalValidator(ctx, msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check tombstoned status for signer %s", msg.Signer)
	}
	if isTombstoned {
		return nil, fmt.Errorf("universal validator for signer %s is tombstoned", msg.Signer)
	}

	err = ms.k.VoteOutbound(ctx, signerValAddr, msg.UtxId, msg.TxId, *msg.ObservedTx)
	if err != nil {
		return nil, err
	}

	return &types.MsgVoteOutboundResponse{}, nil
}

// VoteChainMeta implements types.MsgServer.
func (ms msgServer) VoteChainMeta(ctx context.Context, msg *types.MsgVoteChainMeta) (*types.MsgVoteChainMetaResponse, error) {
	signerAccAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	signerValAddr := sdk.ValAddress(signerAccAddr)

	isTombstoned, err := ms.k.uvalidatorKeeper.IsTombstonedUniversalValidator(ctx, msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check tombstoned status for signer %s", msg.Signer)
	}
	if isTombstoned {
		return nil, fmt.Errorf("universal validator for signer %s is tombstoned", msg.Signer)
	}

	// Admission is gated on the same eligibility predicate ballot creation uses
	// (lifecycle ACTIVE/PENDING_JOIN AND bonded AND not tombstoned) rather than
	// the lifecycle-blind IsBondedUniversalValidator. Admin removal moves a
	// universal validator to PENDING_LEAVE while its stake can remain bonded,
	// and AfterValidatorRemoved prunes its ChainMeta rows but revokes neither
	// its AuthZ grant nor its membership in the universal validator set -- so
	// under the bonded-only gate the removed hotkey could reinsert votes right
	// after the prune.
	//
	// Tightening admission is safe here, and only here, because ChainMeta is
	// median-based rather than ballot-based: there is no CreateBallot, no
	// snapshotted EligibleVoters and no frozen VotingThreshold. Every vote
	// recomputes the median over whichever votes are currently fresh, so a
	// narrower voter set cannot strand anything in flight. The ballot-based
	// vote paths (VoteInbound/VoteOutbound above) deliberately keep the looser
	// gate: tightening them would make already-frozen thresholds unreachable.
	if err := ms.requireEligibleChainMetaVoter(ctx, signerValAddr); err != nil {
		return nil, err
	}

	err = ms.k.VoteChainMeta(ctx, signerValAddr, msg.ObservedChainId, msg.Price, msg.ChainHeight)
	if err != nil {
		return nil, err
	}
	return &types.MsgVoteChainMetaResponse{}, nil
}

// requireEligibleChainMetaVoter returns nil only when signerValAddr is present
// in the current eligible-voter set, i.e. it satisfies exactly the same
// predicate uvalidator applies when it snapshots a ballot's voters. Reusing
// GetEligibleVoters rather than re-deriving the checks keeps ChainMeta vote
// admission from drifting away from that definition.
func (ms msgServer) requireEligibleChainMetaVoter(ctx context.Context, signerValAddr sdk.ValAddress) error {
	eligible, err := ms.k.uvalidatorKeeper.GetEligibleVoters(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to fetch eligible voters for signer %s", signerValAddr.String())
	}

	want := signerValAddr.String()
	for _, uv := range eligible {
		if uv.IdentifyInfo != nil && uv.IdentifyInfo.CoreValidatorAddress == want {
			return nil
		}
	}

	return fmt.Errorf(
		"universal validator %s is not an eligible voter; only ACTIVE or PENDING_JOIN universal validators with bonded, non-tombstoned staking state may vote on chain meta",
		want,
	)
}

// RevertStuckInbound is the admin escape hatch — see Keeper.RevertStuckInbound.
func (ms msgServer) RevertStuckInbound(ctx context.Context, msg *types.MsgRevertStuckInbound) (*types.MsgRevertStuckInboundResponse, error) {
	ms.k.Logger().Info("msg: RevertStuckInbound", "signer", msg.Signer)

	admin, err := ms.k.uvalidatorKeeper.GetAdmin(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read uvalidator admin")
	}
	if admin != msg.Signer {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid admin; expected %s, got %s", admin, msg.Signer)
	}

	if msg.Inbound == nil {
		return nil, errors.Wrap(sdkErrors.ErrInvalidRequest, "inbound is required")
	}

	utxId, outboundId, err := ms.k.RevertStuckInbound(ctx, *msg.Inbound)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"inbound_reverted_by_admin",
		sdk.NewAttribute("admin", msg.Signer),
		sdk.NewAttribute("utx_id", utxId),
		sdk.NewAttribute("outbound_id", outboundId),
		sdk.NewAttribute("source_chain", msg.Inbound.SourceChain),
		sdk.NewAttribute("amount", msg.Inbound.Amount),
	))

	return &types.MsgRevertStuckInboundResponse{
		UtxId:      utxId,
		OutboundId: outboundId,
	}, nil
}

// ExecuteStuckInbound is the admin escape hatch — see Keeper.ExecuteStuckInbound.
func (ms msgServer) ExecuteStuckInbound(ctx context.Context, msg *types.MsgExecuteStuckInbound) (*types.MsgExecuteStuckInboundResponse, error) {
	ms.k.Logger().Info("msg: ExecuteStuckInbound", "signer", msg.Signer)

	admin, err := ms.k.uvalidatorKeeper.GetAdmin(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read uvalidator admin")
	}
	if admin != msg.Signer {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid admin; expected %s, got %s", admin, msg.Signer)
	}

	if msg.Inbound == nil {
		return nil, errors.Wrap(sdkErrors.ErrInvalidRequest, "inbound is required")
	}

	utxId, err := ms.k.ExecuteStuckInbound(ctx, *msg.Inbound)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"inbound_executed_by_admin",
		sdk.NewAttribute("admin", msg.Signer),
		sdk.NewAttribute("utx_id", utxId),
		sdk.NewAttribute("source_chain", msg.Inbound.SourceChain),
		sdk.NewAttribute("amount", msg.Inbound.Amount),
	))

	return &types.MsgExecuteStuckInboundResponse{
		UtxId: utxId,
	}, nil
}

// ExecuteStuckOutbound is the admin escape hatch — see Keeper.ExecuteStuckOutbound.
func (ms msgServer) ExecuteStuckOutbound(ctx context.Context, msg *types.MsgExecuteStuckOutbound) (*types.MsgExecuteStuckOutboundResponse, error) {
	ms.k.Logger().Info("msg: ExecuteStuckOutbound", "signer", msg.Signer)

	admin, err := ms.k.uvalidatorKeeper.GetAdmin(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read uvalidator admin")
	}
	if admin != msg.Signer {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid admin; expected %s, got %s", admin, msg.Signer)
	}

	if msg.ObservedTx == nil {
		return nil, errors.Wrap(sdkErrors.ErrInvalidRequest, "observed_tx is required")
	}

	// Normalize IDs: strip 0x prefix, as VoteOutbound does.
	utxId := strings.TrimPrefix(msg.UtxId, "0x")
	outboundId := strings.TrimPrefix(msg.TxId, "0x")

	settledId, err := ms.k.ExecuteStuckOutbound(ctx, utxId, outboundId, *msg.ObservedTx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"outbound_executed_by_admin",
		sdk.NewAttribute("admin", msg.Signer),
		sdk.NewAttribute("utx_id", utxId),
		sdk.NewAttribute("outbound_id", settledId),
		sdk.NewAttribute("success", fmt.Sprintf("%t", msg.ObservedTx.Success)),
	))

	return &types.MsgExecuteStuckOutboundResponse{
		OutboundId: settledId,
	}, nil
}
