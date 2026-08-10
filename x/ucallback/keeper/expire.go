package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

// MaxExpiriesPerBlock bounds how many requests one EndBlocker may retire.
//
// Each expiry is a real EVM call, so an unbounded sweep would let a backlog turn a
// single block into an arbitrarily expensive one. The bound is a rate limit, not a
// cap: whatever is left over is picked up next block, and the set is ordered by
// deadline so the longest-overdue always go first.
const MaxExpiriesPerBlock = 50

// MaxExpiryAttempts bounds how many times one request's expiry call may fail
// before the chain stops trying.
//
// Retrying matters because expiry now moves money: UniversalCallback._settle
// credits the funder's refund, and expireExternalRead is module-gated, so if we
// stop calling nobody else can. A transient failure that we treated as final would
// strand the refund permanently.
//
// Bounded because two of the contract's three reverts are permanent —
// RequestAlreadyFulfilled and InvalidCallbackTarget both mean the request was
// already settled by the fulfil path, so the money is safe and retrying is pure
// waste. Five attempts distinguishes the transient case without looping forever.
const MaxExpiryAttempts = 5

// SweepExpired retires every read whose deadline has passed.
//
// This is the only path by which a request expires. Nothing else can trigger it,
// because a deadline passing is not an event — it is just time going by, so
// somebody has to look. Fulfilment has an event to hang off (a ballot reaching
// quorum); expiry does not.
func (k Keeper) SweepExpired(ctx sdk.Context) error {
	height := uint64(ctx.BlockHeight())

	// Phase 1 — collect. IterateExpiredBy walks PendingByExpiry, and ExpireRead
	// removes from it. Mutating a collection while iterating it skips entries at
	// best and panics at worst, so the walk finishes before anything is written.
	// Same two-phase shape as x/uvalidator's ExpireBallotsBeforeHeight.
	due := make([]types.UniversalRead, 0, MaxExpiriesPerBlock)
	if err := k.IterateExpiredBy(ctx, height, func(ur types.UniversalRead) bool {
		due = append(due, ur)
		return len(due) < MaxExpiriesPerBlock
	}); err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}

	k.Logger().Debug("sweeping expired read requests", "count", len(due), "height", height)

	// Phase 2 — act.
	for _, ur := range due {
		if err := k.ExpireRead(ctx, ur); err != nil {
			// One unwritable record must not abort the block or stop the rest of
			// the sweep. ExpireRead already absorbs contract-level failures; an
			// error here means the state write itself failed.
			k.Logger().Error("failed to expire read request",
				"request_id", ur.Id, "err", err.Error())
		}
	}

	return nil
}

// ExpireRead retires one request: tell the contract, record the attempt, and mark
// the record terminal once the contract has acknowledged it.
//
// A failed call leaves the request in flight so the next block retries it, up to
// MaxExpiryAttempts. Every attempt is recorded as a PCTx, so the count is the
// record's own history rather than a separate counter.
func (k Keeper) ExpireRead(ctx sdk.Context, ur types.UniversalRead) error {
	_, moduleHex := k.GetModuleAddress(ctx)

	// The EVM call runs against a scratch state so a revert leaves nothing behind —
	// in particular the module nonce increment inside callAsModule must not land
	// when no transaction actually happened.
	tmpCtx, commit := ctx.CacheContext()
	res, callErr := k.CallExpireExternalRead(tmpCtx, ur.Id)
	succeeded := callErr == nil && (res == nil || res.VmError == "")
	if succeeded {
		commit()
	}

	ur.PcTx = append(ur.PcTx, pcTxFrom(ctx, moduleHex, res, callErr))

	switch {
	case succeeded:
		// Terminal status removes it from PendingByExpiry, so it is never swept twice.
		ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED
		k.Logger().Info("read request expired",
			"request_id", ur.Id, "tx_hash", pcTxHash(res), "attempts", len(ur.PcTx))

	case len(ur.PcTx) < MaxExpiryAttempts:
		// Status untouched, so the request stays in PendingByExpiry and the next
		// block tries again.
		k.Logger().Warn("read expiry call failed, will retry",
			"request_id", ur.Id, "attempt", len(ur.PcTx),
			"of", MaxExpiryAttempts, "error", pcTxError(res, callErr))

	default:
		// Out of attempts. ABORTED, not EXPIRED: EXPIRED asserts the contract
		// accepted the expiry and credited the refund, and here it never did. The
		// contract may still hold the request as pending, and since
		// expireExternalRead is module-gated nobody else can settle it — so this
		// state means "needs manual intervention", not "finished".
		//
		// Still terminal, so the sweeper stops spending a slot on it every block.
		ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_ABORTED
		ur.ErrorMsg = pcTxError(res, callErr)
		k.Logger().Error("read expiry abandoned; contract may still hold the request and the refund is unsettled",
			"request_id", ur.Id, "attempts", len(ur.PcTx), "error", ur.ErrorMsg)
	}

	return k.SetUniversalRead(ctx, ur)
}
