package types

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
)

// Method names on UniversalCallback. All are module-gated: the contract admits
// only the x/ucallback module account as caller.
const (
	MethodFulfillExternalCallback = "fulfillExternalCallback"

	// MethodReportCallbackGas settles an EXECUTED request against the gas its
	// callback consumed, and returns that figure clamped to the request's budget.
	MethodReportCallbackGas = "reportCallbackGas"

	MethodExpireExternalRead = "expireExternalRead"
)

// universalCallbackABI covers only the module-gated entry points x/ucallback calls,
// plus the custom errors it must tell apart. Transcribed from
// push-chain-core-contracts src/UniversalCallback.sol and src/libraries/Errors.sol.
//
// Deliberately not the full contract ABI: everything else on UniversalCallback is
// either user-facing or read-only, and a narrower fragment is one less thing to
// keep in sync with the contract.
const universalCallbackABI = `[
  {
    "type": "function",
    "name": "fulfillExternalCallback",
    "stateMutability": "nonpayable",
    "inputs": [
      {"name": "requestId",  "type": "uint256"},
      {"name": "resultData", "type": "bytes"}
    ],
    "outputs": []
  },
  {
    "type": "function",
    "name": "reportCallbackGas",
    "stateMutability": "nonpayable",
    "inputs": [
      {"name": "requestId", "type": "uint256"},
      {"name": "gasBurned", "type": "uint256"}
    ],
    "outputs": [{"name": "burned", "type": "uint256"}]
  },
  {
    "type": "function",
    "name": "expireExternalRead",
    "stateMutability": "nonpayable",
    "inputs": [{"name": "requestId", "type": "uint256"}],
    "outputs": []
  },

  {"type": "error", "name": "InvalidRequestStatus", "inputs": [
    {"name": "requestId", "type": "uint256"},
    {"name": "actual",    "type": "uint8"},
    {"name": "expected",  "type": "uint8"}
  ]},
  {"type": "error", "name": "InvalidCallbackTarget",       "inputs": []},
  {"type": "error", "name": "CallerIsNotUCallbackModule",  "inputs": []},
  {"type": "error", "name": "RequestNotYetExpired",        "inputs": []},
  {"type": "error", "name": "TransferFailed",              "inputs": []}
]`

var (
	parsedCallbackABI abi.ABI

	// Custom-error selectors, derived from the ABI rather than hardcoded so they
	// cannot drift from the contract. A Solidity custom error reverts with
	// keccak256(signature)[:4] followed by its ABI-encoded args, and that prefix is
	// how we tell one revert from another.
	errInvalidRequestStatus       [4]byte
	errInvalidCallbackTarget      [4]byte
	errCallerIsNotUCallbackModule [4]byte
	errRequestNotYetExpired       [4]byte
	errTransferFailed             [4]byte
)

func init() {
	parsed, err := abi.JSON(strings.NewReader(universalCallbackABI))
	if err != nil {
		panic(fmt.Sprintf("ucallback: bad UniversalCallback ABI: %v", err))
	}
	parsedCallbackABI = parsed

	sel := func(name string) [4]byte {
		e, ok := parsed.Errors[name]
		if !ok {
			panic(fmt.Sprintf("ucallback: error %s missing from ABI", name))
		}
		var out [4]byte
		copy(out[:], e.ID[:4])
		return out
	}
	errInvalidRequestStatus = sel("InvalidRequestStatus")
	errInvalidCallbackTarget = sel("InvalidCallbackTarget")
	errCallerIsNotUCallbackModule = sel("CallerIsNotUCallbackModule")
	errRequestNotYetExpired = sel("RequestNotYetExpired")
	errTransferFailed = sel("TransferFailed")
}

// ParseUniversalCallbackABI returns the parsed fragment for UniversalCallback.
func ParseUniversalCallbackABI() (abi.ABI, error) {
	return parsedCallbackABI, nil
}

// CallOutcome classifies what happened to a call into UniversalCallback, because
// the right response differs sharply between them — and the difference is invisible
// from "the call failed" alone.
type CallOutcome int

const (
	// CallOK — the transaction succeeded. Note this includes the app's callback
	// reverting: the contract catches that with .call, so the outer tx still
	// succeeds and the request is settled.
	CallOK CallOutcome = iota

	// CallAlreadySettled — RequestAlreadyFulfilled or InvalidCallbackTarget. The
	// contract closed this request by another route and already ran _settle, so the
	// funder has their refund. Nothing left to do; safe to mark terminal.
	CallAlreadySettled

	// CallOutOfGas — the outer transaction ran out. Nothing persisted, but real gas
	// was burned. The only outcome where the user's allowance was consumed without
	// the contract recording it.
	CallOutOfGas

	// CallUnsettled — everything else: wrong module address, the vault refusing the
	// protocol fee, an intrinsic-gas rejection, a dispatch error. Nothing executed
	// or nothing persisted, the deposit is still escrowed, and expiry must stay
	// reachable so the funder can be refunded.
	CallUnsettled
)

func (o CallOutcome) String() string {
	switch o {
	case CallOK:
		return "ok"
	case CallAlreadySettled:
		return "already_settled"
	case CallOutOfGas:
		return "out_of_gas"
	default:
		return "unsettled"
	}
}

// ClassifyCall maps a DerivedEVMCallWithData result onto a CallOutcome.
//
// vmError distinguishes out-of-gas ("out of gas") from a revert ("execution
// reverted"); revertData carries the custom error's 4-byte selector on a revert.
// Both come straight off MsgEthereumTxResponse.
func ClassifyCall(vmError string, revertData []byte, callErr error) CallOutcome {
	// vmError is checked before callErr on purpose. The EVM call returns BOTH a
	// response and an error when the EVM reverts (call_evm.go:323 wraps res.Failed()
	// in ErrVMExecution), so treating a non-nil error as "no response" would discard
	// the revert data and make CallAlreadySettled unreachable.
	if vmError == "" {
		if callErr != nil {
			// No execution at all — rejected before the EVM ran (intrinsic gas,
			// nonce lookup, dispatch).
			return CallUnsettled
		}
		return CallOK
	}
	if vmError == vm.ErrOutOfGas.Error() || vmError == vm.ErrCodeStoreOutOfGas.Error() {
		return CallOutOfGas
	}

	if len(revertData) >= 4 {
		var sel [4]byte
		copy(sel[:], revertData[:4])
		switch sel {
		case errInvalidCallbackTarget:
			return CallAlreadySettled

		case errInvalidRequestStatus:
			// The request moved on, but not necessarily to a settled state. Decode
			// the actual status rather than assuming: EXECUTED means the callback
			// ran and the budget is still escrowed awaiting reportCallbackGas, so
			// treating it as settled would abandon that money.
			return classifyStatusRevert(revertData)

		case errCallerIsNotUCallbackModule, errRequestNotYetExpired, errTransferFailed:
			return CallUnsettled
		}
	}

	// An unrecognised revert. Treated as unsettled on purpose: assuming the
	// contract settled when it did not would strand the funder's deposit, while the
	// reverse only costs a retry.
	return CallUnsettled
}

// RequestStatus mirrors the contract's lifecycle enum (ReadTypes.sol). NONE must
// stay zero: every never-created requestId reads as zero on-chain.
type RequestStatus uint8

const (
	RequestStatusNone RequestStatus = iota
	RequestStatusPending
	RequestStatusExecuted
	RequestStatusSettled
	RequestStatusExpired
)

// classifyStatusRevert reads the `actual` status out of an InvalidRequestStatus
// revert and decides whether the request is genuinely finished.
//
// Only SETTLED and EXPIRED are terminal on the contract. EXECUTED is not: the
// callback ran but reportCallbackGas has not, so callbackBudget is still escrowed
// and the funder is still owed a refund — retiring our record there would leave
// nothing driving the report.
func classifyStatusRevert(revertData []byte) CallOutcome {
	args, err := parsedCallbackABI.Errors["InvalidRequestStatus"].Inputs.Unpack(revertData[4:])
	if err != nil || len(args) < 2 {
		// Undecodable: assume unsettled, the safe direction.
		return CallUnsettled
	}
	actual, ok := args[1].(uint8)
	if !ok {
		return CallUnsettled
	}
	switch RequestStatus(actual) {
	case RequestStatusSettled, RequestStatusExpired:
		return CallAlreadySettled
	default:
		return CallUnsettled
	}
}

// CallerIsNotUCallbackModuleSelector exposes the access-control revert prefix so
// tests can assert which revert a call produced. The contract pairs itself with a
// single module address at construction; getting this wrong makes every callback
// fail, so it is worth asserting against the real deployed bytecode.
func CallerIsNotUCallbackModuleSelector() [4]byte { return errCallerIsNotUCallbackModule }
