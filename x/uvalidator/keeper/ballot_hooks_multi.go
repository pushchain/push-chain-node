package keeper

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// MultiBallotHooks fans a ballot terminal transition out to several modules.
//
// Added for x/ucallback, which reacts to READ_RESULT terminals alongside
// x/uexecutor's INBOUND_TX handling — the extension the Hooks doc comment
// anticipated. Follows MultiUValidatorHooks.
type MultiBallotHooks []types.BallotHooks

// NewMultiBallotHooks creates a new combined ballot hook instance.
func NewMultiBallotHooks(hooks ...types.BallotHooks) MultiBallotHooks {
	return hooks
}

// AfterBallotTerminal calls every hook, and does not stop at the first error.
//
// Terminal status is already decided by the time this runs; one module failing to
// clean up must not deny the others their notification. Errors are joined so the
// caller still sees everything that went wrong — though per the BallotHooks
// contract the caller logs and ignores them rather than blocking the transition.
func (mh MultiBallotHooks) AfterBallotTerminal(
	ctx sdk.Context,
	ballotID string,
	ballotType types.BallotObservationType,
	status types.BallotStatus,
) error {
	ctx.Logger().Debug("hook: AfterBallotTerminal",
		"ballot_id", ballotID,
		"ballot_type", ballotType.String(),
		"status", status.String(),
		"hook_count", len(mh),
	)

	var errs []error
	for _, h := range mh {
		if err := h.AfterBallotTerminal(ctx, ballotID, ballotType, status); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
