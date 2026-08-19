package keeper

import (
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"

	pchaintypes "github.com/pushchain/push-chain-node/types"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// universalCallbackAddress is the system contract x/ucallback drives.
func universalCallbackAddress() common.Address {
	return common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"].Address)
}

// requestIDToUint256 converts a stored request id back to the uint256 the contract
// expects. Ingest keeps the raw 32-byte topic hex precisely so this is a parse and
// not a base conversion.
func requestIDToUint256(requestID string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(trim0x(requestID), 16)
	if !ok {
		return nil, fmt.Errorf("request id %q is not hex", requestID)
	}
	return v, nil
}

func trim0x(s string) string {
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		return s[2:]
	}
	return s
}

// callAsModule issues a DerivedEVMCall to UniversalCallback from the x/ucallback
// module account.
//
// That account is the only sender UniversalCallback's access control admits, and
// this module is its only user — so the nonce counter lives here, alongside the
// account it belongs to. Every call must draw and advance it, or the second call in
// a block reuses a consumed nonce and is rejected.
func (k Keeper) callAsModule(
	ctx sdk.Context,
	method string,
	args ...interface{},
) (*evmtypes.MsgEthereumTxResponse, error) {
	callbackABI, err := types.ParseUniversalCallbackABI()
	if err != nil {
		return nil, err
	}

	from, _ := k.GetModuleAddress(ctx)

	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read module account nonce: %w", err)
	}
	if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
		return nil, fmt.Errorf("failed to advance module account nonce: %w", err)
	}

	data, err := callbackABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack %s: %w", method, err)
	}

	// DerivedEVMCallWithData, not the ABI-typed DerivedEVMCall wrapper: on a revert
	// the wrapper returns (nil, err), throwing away the response — and with it
	// res.Ret, the revert data. ClassifyCall reads that data to tell "already
	// settled" from "try again", so routing through the wrapper would collapse every
	// revert into CallUnsettled and make the distinction unreachable. This layer
	// returns (res, err) together, which is what its own doc comment describes.
	contract := universalCallbackAddress()
	return k.evmKeeper.DerivedEVMCallWithData(
		ctx,
		from,
		&contract,
		data,
		true,  // commit
		false, // not gasless — we want gas accounted in the receipt
		true,  // isModuleSender
		big.NewInt(0),
		// nil gas limit — the callback's own budget is enforced by the contract
		// (callbackGasLimit, capped at MAX_CALLBACK_GAS_LIMIT), so a limit here
		// would only add a second ceiling that could cut the callback short.
		nil,
		&nonce,
	)
}

// CallFulfillExternalCallback delivers a finalized observation to the contract,
// which forwards it to the requesting app's callback.
func (k Keeper) CallFulfillExternalCallback(
	ctx sdk.Context,
	requestID string,
	result *types.ReadResult,
) (*evmtypes.MsgEthereumTxResponse, error) {
	if result == nil {
		return nil, fmt.Errorf("cannot fulfil %s: nil result", requestID)
	}

	id, err := requestIDToUint256(requestID)
	if err != nil {
		return nil, err
	}

	k.Logger().Debug("EVM call: fulfillExternalCallback",
		"request_id", requestID,
		"result_len", len(result.ResultData),
	)

	return k.callAsModule(ctx,
		types.MethodFulfillExternalCallback,
		id,
		result.ResultData,
	)
}

// CallExpireExternalRead retires a request the contract will no longer accept.
func (k Keeper) CallExpireExternalRead(
	ctx sdk.Context,
	requestID string,
) (*evmtypes.MsgEthereumTxResponse, error) {
	id, err := requestIDToUint256(requestID)
	if err != nil {
		return nil, err
	}

	k.Logger().Debug("EVM call: expireExternalRead", "request_id", requestID)

	return k.callAsModule(ctx, types.MethodExpireExternalRead, id)
}

// pcTxFrom renders an EVM call attempt as a PCTx audit entry. Both the success and
// failure paths produce one, so a request's history shows every attempt made on it
// rather than only the one that stuck.
func pcTxFrom(ctx sdk.Context, sender string, res *evmtypes.MsgEthereumTxResponse, callErr error) *uexecutortypes.PCTx {
	pcTx := &uexecutortypes.PCTx{
		Sender:      sender,
		BlockHeight: uint64(ctx.BlockHeight()),
		Status:      "SUCCESS",
	}
	if res != nil {
		pcTx.TxHash = res.Hash
		pcTx.GasUsed = res.GasUsed
		if res.VmError != "" {
			pcTx.Status = "FAILED"
			pcTx.ErrorMsg = res.VmError
		}
	}
	if callErr != nil {
		pcTx.Status = "FAILED"
		pcTx.ErrorMsg = callErr.Error()
	}
	return pcTx
}

// ErrBudgetTooSmall is recorded on a read whose callback budget cannot cover the
// gas its app declared it needs. Fixed text, set by us from a deterministic
// comparison — it never comes from a validator and never touches a ballot.
const ErrBudgetTooSmall = "callback budget does not cover the declared callback gas limit"

// CallbackCost prices a gas figure at the current base fee.
//
// Same valuation x/uexecutor applies to UEA execution, so a read and a payload
// execution are charged alike.
func (k Keeper) CallbackCost(ctx sdk.Context, gas uint64) (*big.Int, error) {
	baseFee := k.feemarketKeeper.GetBaseFee(ctx)
	if baseFee.IsNil() {
		return nil, fmt.Errorf("base fee unavailable")
	}
	return new(big.Int).Mul(
		new(big.Int).SetUint64(gas),
		baseFee.TruncateInt().BigInt(),
	), nil
}

// CanAffordCallback reports whether the request funded the gas it declared.
//
// All-or-nothing on purpose. Executing a partially funded callback would hand the
// app less gas than it asked for, which almost certainly runs out anyway — the user
// then pays for a doomed attempt instead of getting a full refund.
func (k Keeper) CanAffordCallback(ctx sdk.Context, req *types.ReadRequest) (bool, error) {
	if req == nil {
		return false, fmt.Errorf("nil read request")
	}
	cost, err := k.CallbackCost(ctx, req.CallbackGasLimit)
	if err != nil {
		return false, err
	}
	budget, err := parseBudget(req.CallbackBudget)
	if err != nil {
		return false, err
	}
	return budget.Cmp(cost) >= 0, nil
}

// parseBudget reads a uint256 decimal string, treating empty as zero. Records
// ingested before the fee split existed carry no budget, and an unfunded read is
// the honest reading of that — not a malformed one.
func parseBudget(s string) (*big.Int, error) {
	if s == "" {
		return big.NewInt(0), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("callback budget %q is not a decimal integer", s)
	}
	return v, nil
}

// CallReportCallbackGas settles an executed request, returning the amount the
// contract clamped the report to.
func (k Keeper) CallReportCallbackGas(
	ctx sdk.Context, requestID string, cost *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	id, err := requestIDToUint256(requestID)
	if err != nil {
		return nil, err
	}
	k.Logger().Debug("EVM call: reportCallbackGas", "request_id", requestID, "cost", cost.String())
	return k.callAsModule(ctx, types.MethodReportCallbackGas, id, cost)
}

// TakeAndBurn moves the consumed callback budget out of UniversalCallback and
// destroys it.
//
// No contract API is involved: a contract's balance is an ordinary bank balance, so
// the module debits it directly — the same shape as x/uexecutor's DeductAndBurnFees.
// reportCallbackGas has already released the refund and decremented totalEscrowed,
// so `amount` is exactly the unattributed slack the contract left behind for us.
func (k Keeper) TakeAndBurn(ctx sdk.Context, amount *big.Int) error {
	if amount == nil || amount.Sign() <= 0 {
		return nil
	}
	coins := sdk.NewCoins(sdk.NewCoin(
		pchaintypes.BaseDenom, sdkmath.NewIntFromBigInt(amount),
	))

	contractAcc := sdk.AccAddress(universalCallbackAddress().Bytes())
	if err := k.bankKeeper.SendCoinsFromAccountToModule(
		ctx, contractAcc, types.ModuleName, coins,
	); err != nil {
		return fmt.Errorf("failed to take burned callback gas from the contract: %w", err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return fmt.Errorf("failed to burn callback gas: %w", err)
	}

	k.Logger().Info("callback gas burned", "amount", amount.String())
	return nil
}
