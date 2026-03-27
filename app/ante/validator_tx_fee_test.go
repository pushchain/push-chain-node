package ante_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app/ante"
)

// TestCheckTxFeeWithValidatorMinGasPrices_CheckTxSufficientFee verifies that in
// CheckTx mode with a non-zero min gas price, a fee that meets the threshold passes.
func TestCheckTxFeeWithValidatorMinGasPrices_CheckTxSufficientFee(t *testing.T) {
	gas := uint64(100_000)
	// min gas price = 0.01upc → required fee = ceil(0.01 * 100000) = 1000upc
	minGasPrice := sdk.DecCoin{Denom: "upc", Amount: sdkmath.LegacyNewDecWithPrec(1, 2)} // 0.01
	fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))                                    // exactly meets requirement

	ctx := newAnteTestCtx(t, true /*isCheckTx*/).
		WithMinGasPrices(sdk.DecCoins{minGasPrice})

	bk := &mockBankKeeperAnte{}
	payerAcc := sdk.AccAddress([]byte("validpayer_fee1"))
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	ak.SetAccount(ctx, newBaseAcc(payerAcc))

	// nil TxFeeChecker causes NewDeductFeeDecorator to use checkTxFeeWithValidatorMinGasPrices.
	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, nil)

	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      gas,
		fee:      fee,
		feePayer: payerAcc,
	}

	nextCalled := false
	_, err := dfd.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})
	require.NoError(t, err)
	require.True(t, nextCalled)
}

// TestCheckTxFeeWithValidatorMinGasPrices_CheckTxInsufficientFee verifies that in
// CheckTx mode, providing a fee below the validator minimum gas price is rejected.
func TestCheckTxFeeWithValidatorMinGasPrices_CheckTxInsufficientFee(t *testing.T) {
	gas := uint64(100_000)
	// min gas price = 0.01upc → required fee = ceil(0.01 * 100000) = 1000upc
	minGasPrice := sdk.DecCoin{Denom: "upc", Amount: sdkmath.LegacyNewDecWithPrec(1, 2)}
	insufficientFee := sdk.NewCoins(sdk.NewInt64Coin("upc", 500)) // below required 1000

	ctx := newAnteTestCtx(t, true /*isCheckTx*/).
		WithMinGasPrices(sdk.DecCoins{minGasPrice})

	bk := &mockBankKeeperAnte{}
	payerAcc := sdk.AccAddress([]byte("validpayer_fee2"))
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	ak.SetAccount(ctx, newBaseAcc(payerAcc))

	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, nil)

	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      gas,
		fee:      insufficientFee,
		feePayer: payerAcc,
	}

	_, err := dfd.AnteHandle(ctx, tx, false, emptyNext)
	require.Error(t, err)
	require.True(t, sdkerrors.ErrInsufficientFee.Is(err),
		"expected ErrInsufficientFee when fee is below min gas price, got: %v", err)
}

// TestCheckTxFeeWithValidatorMinGasPrices_DeliverTxNoMinGasPriceCheck verifies
// that in DeliverTx mode (non-check), the min gas price requirement is skipped
// entirely — any non-zero fee passes.
func TestCheckTxFeeWithValidatorMinGasPrices_DeliverTxNoMinGasPriceCheck(t *testing.T) {
	gas := uint64(100_000)
	minGasPrice := sdk.DecCoin{Denom: "upc", Amount: sdkmath.LegacyNewDecWithPrec(1, 2)}
	// tiny fee that would fail in CheckTx mode
	smallFee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1))

	ctx := newAnteTestCtx(t, false /*isCheckTx=false = DeliverTx*/).
		WithMinGasPrices(sdk.DecCoins{minGasPrice})

	bk := &mockBankKeeperAnte{}
	payerAcc := sdk.AccAddress([]byte("validpayer_fee3"))
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	ak.SetAccount(ctx, newBaseAcc(payerAcc))

	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, nil)

	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      gas,
		fee:      smallFee,
		feePayer: payerAcc,
	}

	nextCalled := false
	_, err := dfd.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})
	require.NoError(t, err)
	require.True(t, nextCalled, "DeliverTx must not apply min gas price check")
}

// TestCheckTxFeeWithValidatorMinGasPrices_ZeroMinGasPrice verifies that when no
// min gas price is configured (zero), any fee (even zero) is accepted in CheckTx.
func TestCheckTxFeeWithValidatorMinGasPrices_ZeroMinGasPrice(t *testing.T) {
	ctx := newAnteTestCtx(t, true /*isCheckTx*/)
	// No WithMinGasPrices call → MinGasPrices is zero → no check applied

	bk := &mockBankKeeperAnte{}
	payerAcc := sdk.AccAddress([]byte("validpayer_fee4"))
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	ak.SetAccount(ctx, newBaseAcc(payerAcc))

	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, nil)

	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      100_000,
		fee:      sdk.Coins{}, // zero fee
		feePayer: payerAcc,
	}

	nextCalled := false
	_, err := dfd.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})
	require.NoError(t, err)
	require.True(t, nextCalled)
}

// ---------------------------------------------------------------------------
// Helper shared by validator_tx_fee_test and fee_test
// ---------------------------------------------------------------------------

func newBaseAcc(addr sdk.AccAddress) *authtypes.BaseAccount {
	return authtypes.NewBaseAccountWithAddress(addr)
}
