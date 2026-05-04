package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// UValidatorHooks defines the interface that external modules can implement
// to react to uvalidator lifecycle events.
type UValidatorHooks interface {
	// Triggered when a validator enters PENDING_JOIN (newly added or rejoining)
	AfterValidatorAdded(ctx sdk.Context, valAddr sdk.ValAddress)

	// Triggered when a validator enters PENDING_LEAVE status (starting removal)
	AfterValidatorRemoved(ctx sdk.Context, valAddr sdk.ValAddress)

	// Triggered whenever a validator's status changes between any two valid states
	AfterValidatorStatusChanged(ctx sdk.Context, valAddr sdk.ValAddress, oldStatus, newStatus UVStatus)
}

// BallotHooks defines the interface that external modules can implement
// to react to ballot lifecycle terminal transitions (EXPIRED, PASSED, REJECTED).
//
// Implementations MUST be idempotent — terminal transitions are write-once
// per ballot in normal flow, but defensive idempotency protects against
// state replay or future code paths that might re-mark a ballot.
//
// Implementations SHOULD NOT block the terminal transition by returning
// errors. The terminal status is the desired outcome regardless of
// secondary-index side-effect failure; callers log+ignore hook errors.
type BallotHooks interface {
	AfterBallotTerminal(
		ctx sdk.Context,
		ballotID string,
		ballotType BallotObservationType,
		status BallotStatus,
	) error
}
