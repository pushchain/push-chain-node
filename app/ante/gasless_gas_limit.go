package ante

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txpolicy "github.com/pushchain/push-chain-node/app/txpolicy"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// GaslessParamsKeeper reads the module parameters that bound fee-exempt txs.
type GaslessParamsKeeper interface {
	GetParams(ctx context.Context) (uexecutortypes.Params, error)
}

// GaslessGasLimitDecorator caps the gas limit a fee-exempt (gasless) tx may
// declare.
//
// A fee-paying tx is bounded by its own fee: the ante handler requires
// ceil(minGasPrice * gasLimit), so an absurd gas limit costs an absurd amount
// of tokens. A gasless tx pays nothing, so nothing bounds the gas it declares
// while that declared gas is still added to the block's cumulative gas wanted.
// Enough of them, or few enough with a large enough declaration, push the
// cumulative total past what the fee market can represent.
//
// CONTRACT: must run before the EVM GasWantedDecorator, which is what
// accumulates the declared gas into the fee market transient store.
type GaslessGasLimitDecorator struct {
	paramsKeeper GaslessParamsKeeper
}

func NewGaslessGasLimitDecorator(pk GaslessParamsKeeper) GaslessGasLimitDecorator {
	return GaslessGasLimitDecorator{paramsKeeper: pk}
}

func (ggd GaslessGasLimitDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !txpolicy.IsGaslessTx(tx) {
		return next(ctx, tx, simulate)
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	maxGas := ggd.maxGaslessTxGas(ctx)
	if gas := feeTx.GetGas(); gas > maxGas {
		ctx.Logger().Debug("gasless gas limit decorator: declared gas over cap",
			"gas", gas,
			"max_gas", maxGas,
		)
		return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidGasLimit,
			"gasless tx gas limit %d exceeds the maximum allowed %d", gas, maxGas)
	}

	return next(ctx, tx, simulate)
}

// maxGaslessTxGas resolves the governance-controlled cap, falling back to the
// default when it cannot be read or was never set. The fallback is deliberate:
// a missing parameter must not mean "no cap".
func (ggd GaslessGasLimitDecorator) maxGaslessTxGas(ctx sdk.Context) uint64 {
	params, err := ggd.paramsKeeper.GetParams(ctx)
	if err != nil {
		ctx.Logger().Error("gasless gas limit decorator: failed to read uexecutor params, using default cap",
			"error", err,
		)
		return uexecutortypes.DefaultMaxGaslessTxGas
	}

	if params.MaxGaslessTxGas == 0 {
		return uexecutortypes.DefaultMaxGaslessTxGas
	}

	return params.MaxGaslessTxGas
}
