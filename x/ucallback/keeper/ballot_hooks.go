package keeper

import (
	"fmt"

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
// The read is only marked terminal when the contract actually settled it. That
// distinction matters: on any revert the whole transaction rolls back, so
// fulfilledRequests stays false, _pending survives and _settle never runs — the
// funder's deposit is still escrowed. Retiring the record in that state would drop
// it out of PendingByExpiry, leaving nothing able to release those funds, since
// expireExternalRead admits only this module.
//
// A reverting app callback is NOT such a case: the contract catches it with .call,
// so the outer transaction succeeds, the request settles, and we mark it FULFILLED.
func (k Keeper) FulfilRead(ctx sdk.Context, ur types.UniversalRead) error {
	_, moduleHex := k.GetModuleAddress(ctx)

	// A request that did not fund the gas it declared is never executed. Running it
	// on a short budget would hand the app less gas than it asked for, which fails
	// anyway and charges the funder for a doomed attempt. Left in flight instead, so
	// the sweeper expires it at its deadline and the whole budget goes back.
	affordable, err := k.CanAffordCallback(ctx, ur.Request)
	if err != nil {
		return err
	}
	if !affordable {
		ur.ErrorMsg = ErrBudgetTooSmall
		k.Logger().Warn("read not fulfilled: callback budget too small",
			"request_id", ur.Id,
			"callback_gas_limit", ur.Request.GetCallbackGasLimit(),
			"callback_budget", ur.Request.GetCallbackBudget())
		return k.SetUniversalRead(ctx, ur)
	}

	tmpCtx, commit := ctx.CacheContext()
	res, callErr := k.CallFulfillExternalCallback(tmpCtx, ur.Id, ur.Result)

	var vmErr string
	var revertData []byte
	if res != nil {
		vmErr, revertData = res.VmError, res.Ret
	}
	outcome := types.ClassifyCall(vmErr, revertData, callErr)

	if outcome == types.CallOK {
		commit()
	}

	ur.PcTx = append(ur.PcTx, pcTxFrom(ctx, moduleHex, res, callErr))

	switch outcome {
	case types.CallOK:
		ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED
		k.Logger().Info("read request fulfilled",
			"request_id", ur.Id, "tx_hash", pcTxHash(res))

		// Settle: tell the contract what the callback cost, then destroy exactly
		// that. Ordered report-then-take because reportCallbackGas releases the
		// refund and decrements totalEscrowed first — taking beforehand would leave
		// the contract briefly holding less than it owes.
		if err := k.settleCallbackGas(ctx, &ur, res); err != nil {
			// The callback ran and the contract is EXECUTED; only the accounting
			// failed. Record it and leave the status terminal — re-running fulfil
			// would revert, and the escrow is recoverable by admin.
			ur.ErrorMsg = err.Error()
			k.Logger().Error("callback gas not settled",
				"request_id", ur.Id, "error", err.Error())
		}

	case types.CallAlreadySettled:
		// The contract closed it another way and the funder already has their
		// refund. Terminal, but not a fulfilment we performed.
		ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED
		ur.ErrorMsg = pcTxError(res, callErr)
		k.Logger().Warn("read already settled on the contract",
			"request_id", ur.Id, "error", ur.ErrorMsg)

	default:
		// CallOutOfGas or CallUnsettled. Nothing persisted, so the request stays in
		// flight and PendingByExpiry keeps it — the sweeper will expire it at its
		// deadline and the funder gets refunded. Status is deliberately untouched.
		ur.ErrorMsg = pcTxError(res, callErr)
		k.Logger().Error("read fulfilment did not settle; leaving in flight for expiry",
			"request_id", ur.Id, "outcome", outcome.String(), "error", ur.ErrorMsg)
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

// pcTxError picks the reason worth storing on-chain. res.VmError comes first
// because a revert produces both a response and an error, and the error is the
// wrapper's decoration ("<vmError>: ret 0x...") around the same fact. The bare VM
// reason is the stable one; callErr is only informative when there is no response.
func pcTxError(res *evmtypes.MsgEthereumTxResponse, callErr error) string {
	if res != nil && res.VmError != "" {
		return res.VmError
	}
	if callErr != nil {
		return callErr.Error()
	}
	return "unknown"
}

// settleCallbackGas reports the callback's cost and burns what the contract clamps
// it to.
//
// The burn amount is recomputed here rather than read back from the EVM return
// data: with the affordability gate in place the clamp can never bind, so the two
// agree, and min() keeps that true even if the gate is ever relaxed.
func (k Keeper) settleCallbackGas(
	ctx sdk.Context, ur *types.UniversalRead, res *evmtypes.MsgEthereumTxResponse,
) error {
	if res == nil {
		return fmt.Errorf("no receipt to price the callback from")
	}

	cost, err := k.CallbackCost(ctx, res.GasUsed)
	if err != nil {
		return err
	}
	budget, err := parseBudget(ur.Request.GetCallbackBudget())
	if err != nil {
		return err
	}
	if cost.Cmp(budget) > 0 {
		cost = budget
	}

	_, moduleHex := k.GetModuleAddress(ctx)
	repRes, repErr := k.CallReportCallbackGas(ctx, ur.Id, cost)
	ur.PcTx = append(ur.PcTx, pcTxFrom(ctx, moduleHex, repRes, repErr))

	var vmErr string
	if repRes != nil {
		vmErr = repRes.VmError
	}
	if repErr != nil || vmErr != "" {
		return fmt.Errorf("reportCallbackGas failed: %s", pcTxError(repRes, repErr))
	}

	return k.TakeAndBurn(ctx, cost)
}
