package keeper

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"

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

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		callbackABI,
		from,
		universalCallbackAddress(),
		big.NewInt(0),
		// nil gas limit — the callback's own budget is enforced by the contract
		// (callbackGasLimit, capped at MAX_CALLBACK_GAS_LIMIT), so a limit here
		// would only add a second ceiling that could cut the callback short.
		nil,
		true,  // commit
		false, // not gasless — we want gas accounted in the receipt
		true,  // isModuleSender
		&nonce,
		method,
		args...,
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

	// bytes32 is a fixed-size array in the ABI; a short or absent hash must be
	// left-padded rather than passed through as a variable-length slice.
	var blockHash [32]byte
	copy(blockHash[32-min(len(result.ObservedBlockHash), 32):], result.ObservedBlockHash)

	k.Logger().Debug("EVM call: fulfillExternalCallback",
		"request_id", requestID,
		"observed_height", result.ObservedBlockHeight,
		"result_len", len(result.ResultData),
	)

	return k.callAsModule(ctx,
		types.MethodFulfillExternalCallback,
		id,
		result.ResultData,
		result.ObservedBlockHeight,
		blockHash,
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
