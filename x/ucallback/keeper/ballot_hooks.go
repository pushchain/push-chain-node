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

	if status != uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED {
		// EXPIRED or REJECTED — neither retires the request here.
		//
		// Expiry belongs to the sweeper, not to this hook. A request's deadline is a
		// property of the request, and PendingByExpiry already tracks it directly;
		// an expiring ballot is only a shadow of that. Worse, ballot expiry is lazy
		// — x/uvalidator runs ExpireBallotsBeforeHeight from inside CreateBallot,
		// with no EndBlocker — so this hook fires only when some unrelated ballot
		// happens to be created. The sweeper covers strictly more (requests nobody
		// ever voted on have no ballot at all) and, running every block, never
		// later. A second path here would add a race and buy nothing.
		//
		// REJECTED is not a deadline either: the request keeps its remaining time
		// and another observation may still win.
		h.k.Logger().Debug("read ballot did not pass, leaving request to the sweeper",
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
		ur.ErrorMsg = pcTxError(res, callErr)
		k.Logger().Error("read fulfilment failed",
			"request_id", ur.Id, "error", ur.ErrorMsg)
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
