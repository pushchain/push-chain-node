package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// BallotHooks reacts to READ_RESULT ballots reaching a terminal state.
//
// Fulfilment is driven from here rather than from the vote that reached quorum, so
// it happens exactly once regardless of which validator's vote was decisive. Doing
// it in VoteReadResult would make the deciding validator pay the callback's gas and
// would tie the EVM call's success to that one transaction.
type BallotHooks struct {
	k Keeper
}

// NewBallotHooks returns the ballot hook implementation for x/ucallback.
func NewBallotHooks(k Keeper) uvalidatortypes.BallotHooks {
	return BallotHooks{k: k}
}

var _ uvalidatortypes.BallotHooks = BallotHooks{}

// AfterBallotTerminal dispatches on ballot type, ignoring everything that is not a
// read result — x/uexecutor owns the other kinds.
func (h BallotHooks) AfterBallotTerminal(
	ctx sdk.Context,
	ballotID string,
	ballotType uvalidatortypes.BallotObservationType,
	status uvalidatortypes.BallotStatus,
) error {
	if ballotType != uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_READ_RESULT {
		return nil
	}
	return h.afterReadBallotTerminal(ctx, ballotID, status)
}

// afterReadBallotTerminal settles the read the ballot belongs to.
//
// Per the BallotHooks contract this must be idempotent and must not block the
// terminal transition. Every branch below that cannot make progress logs and
// returns nil; only a genuine state-write failure propagates.
func (h BallotHooks) afterReadBallotTerminal(
	ctx sdk.Context,
	ballotID string,
	status uvalidatortypes.BallotStatus,
) error {
	ur, found := h.k.GetUniversalReadByBallot(ctx, ballotID)
	if !found {
		// The lookup scans in-flight reads only, so a miss means the request has
		// already settled — this hook re-firing, or a ballot that is not ours.
		// Either way there is nothing left to do.
		h.k.Logger().Debug("read ballot terminal: no in-flight request owns it",
			"ballot_id", ballotID, "status", status.String())
		return nil
	}

	switch status {
	case uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED:
		// fall through to fulfilment below

	case uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED:
		// The ballot carries the request's own deadline, so an expired ballot means
		// the request itself is over — votes accumulated but never reached quorum
		// in time. Retire it now rather than leaving it for the sweeper: the record
		// is unvotable from here (VoteReadResult rejects past-deadline requests), so
		// waiting would only delay closing it on the contract.
		h.k.Logger().Info("read ballot expired, retiring request",
			"ballot_id", ballotID, "request_id", ur.Id)
		return h.k.ExpireRead(ctx, ur)

	default:
		// REJECTED. Not a deadline — the request keeps its remaining time and other
		// observations may still win.
		h.k.Logger().Info("read ballot rejected, request stays in flight",
			"ballot_id", ballotID, "request_id", ur.Id, "status", status.String())
		return nil
	}

	if ur.Result == nil {
		// A PASSED ballot always has its observation attached by VoteReadResult.
		// Reaching here means the two disagree, which we cannot repair from the
		// ballot alone — the ballot ID is a digest, not the observation.
		h.k.Logger().Error("read ballot passed with no recorded result",
			"ballot_id", ballotID, "request_id", ur.Id)
		return nil
	}

	return h.k.FulfilRead(ctx, ur)
}

// FulfilRead delivers a settled observation to UniversalCallback and records the
// outcome on the request.
//
// The EVM call is made in a cache context. A revert inside the app's callback must
// not roll back our own bookkeeping — we still need the request marked terminal and
// the failed attempt recorded, or the sweeper would keep retrying a request the
// contract has already closed.
func (k Keeper) FulfilRead(ctx sdk.Context, ur types.UniversalRead) error {
	_, moduleHex := k.GetModuleAddress(ctx)

	tmpCtx, commit := ctx.CacheContext()
	res, callErr := k.CallFulfillExternalCallback(tmpCtx, ur.Id, ur.Result)
	succeeded := callErr == nil && (res == nil || res.VmError == "")
	if succeeded {
		commit()
	}

	ur.PcTx = append(ur.PcTx, pcTxFrom(ctx, moduleHex, res, callErr))

	if succeeded {
		ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED
		k.Logger().Info("read request fulfilled",
			"request_id", ur.Id, "tx_hash", pcTxHash(res))
	} else {
		// FAILED, not left pending: the contract sets fulfilledRequests before
		// invoking the callback, so the request is closed on its side whether or
		// not the callback reverted. Retrying would revert with
		// RequestAlreadyFulfilled forever.
		ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED
		k.Logger().Error("read fulfilment failed",
			"request_id", ur.Id, "error", pcTxError(res, callErr))
	}

	return k.SetUniversalRead(ctx, ur)
}

// Concrete pointer types, not interfaces: a nil *MsgEthereumTxResponse boxed into
// an interface is not itself nil, so the guards below would not fire.
func pcTxHash(res *evmtypes.MsgEthereumTxResponse) string {
	if res == nil {
		return ""
	}
	return res.Hash
}

func pcTxError(res *evmtypes.MsgEthereumTxResponse, callErr error) string {
	if callErr != nil {
		return callErr.Error()
	}
	if res != nil && res.VmError != "" {
		return res.VmError
	}
	return "unknown"
}

// ExpireRead closes a request the chain will no longer act on, telling the contract
// so its pending entry can be released.
//
// Reached from two directions: a ballot that carried the request's deadline and
// expired (above), and the sweeper for requests nobody ever voted on. Both mark the
// record terminal, which removes it from the in-flight set — so whichever runs
// first, the other will not find it.
//
// Note the contract refunds nothing here; the funder's fee stays with the protocol
// either way. Expiring promptly buys contract-storage cleanup, not a refund.
func (k Keeper) ExpireRead(ctx sdk.Context, ur types.UniversalRead) error {
	_, moduleHex := k.GetModuleAddress(ctx)

	tmpCtx, commit := ctx.CacheContext()
	res, callErr := k.CallExpireExternalRead(tmpCtx, ur.Id)
	succeeded := callErr == nil && (res == nil || res.VmError == "")
	if succeeded {
		commit()
	}

	ur.PcTx = append(ur.PcTx, pcTxFrom(ctx, moduleHex, res, callErr))

	// EXPIRED either way. A revert here is almost always RequestAlreadyFulfilled —
	// the contract closed the request by another route — and in every case the
	// chain is done with it. Leaving it pending would mean retrying forever.
	ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED
	if succeeded {
		k.Logger().Info("read request expired", "request_id", ur.Id, "tx_hash", pcTxHash(res))
	} else {
		k.Logger().Error("read expiry call failed, request retired anyway",
			"request_id", ur.Id, "error", pcTxError(res, callErr))
	}

	return k.SetUniversalRead(ctx, ur)
}
