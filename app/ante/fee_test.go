package ante_test

import (
	"context"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	addresscodec "cosmossdk.io/core/address"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/testutil/integration"

	"github.com/pushchain/push-chain-node/app/ante"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"

	protov2 "google.golang.org/protobuf/proto"
)

// ---------------------------------------------------------------------------
// Minimal mock types used across ante unit tests
// ---------------------------------------------------------------------------

// mockFeeTx implements sdk.FeeTx and sdk.Tx for unit tests.
type mockFeeTx struct {
	msgs       []sdk.Msg
	gas        uint64
	fee        sdk.Coins
	feePayer   []byte
	feeGranter []byte
}

func (m mockFeeTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m mockFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m mockFeeTx) ValidateBasic() error                  { return nil }
func (m mockFeeTx) GetGas() uint64                        { return m.gas }
func (m mockFeeTx) GetFee() sdk.Coins                     { return m.fee }
func (m mockFeeTx) FeePayer() []byte                      { return m.feePayer }
func (m mockFeeTx) FeeGranter() []byte                    { return m.feeGranter }

// mockAccountKeeperAnte satisfies ante.AccountKeeper.
type mockAccountKeeperAnte struct {
	accounts   map[string]sdk.AccountI
	moduleAddr sdk.AccAddress
}

func newMockAccountKeeperAnte(moduleAddr sdk.AccAddress) *mockAccountKeeperAnte {
	return &mockAccountKeeperAnte{
		accounts:   make(map[string]sdk.AccountI),
		moduleAddr: moduleAddr,
	}
}

func (m *mockAccountKeeperAnte) GetModuleAddress(name string) sdk.AccAddress {
	if name == authtypes.FeeCollectorName {
		return m.moduleAddr
	}
	return nil
}

func (m *mockAccountKeeperAnte) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return m.accounts[addr.String()]
}

func (m *mockAccountKeeperAnte) HasAccount(_ context.Context, addr sdk.AccAddress) bool {
	_, ok := m.accounts[addr.String()]
	return ok
}

func (m *mockAccountKeeperAnte) SetAccount(_ context.Context, acc sdk.AccountI) {
	m.accounts[acc.GetAddress().String()] = acc
}

func (m *mockAccountKeeperAnte) RemoveAccount(_ context.Context, acc sdk.AccountI) {
	delete(m.accounts, acc.GetAddress().String())
}

func (m *mockAccountKeeperAnte) NewAccountWithAddress(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(addr)
}

func (m *mockAccountKeeperAnte) GetParams(_ context.Context) authtypes.Params {
	return authtypes.DefaultParams()
}

func (m *mockAccountKeeperAnte) GetSequence(_ context.Context, _ sdk.AccAddress) (uint64, error) {
	return 0, nil
}

func (m *mockAccountKeeperAnte) AddressCodec() addresscodec.Codec {
	return sdkaddress.NewBech32Codec("push")
}

// mockBankKeeperAnte records whether the deduct call was made.
type mockBankKeeperAnte struct {
	deductCalled bool
	deductErr    error
}

func (m *mockBankKeeperAnte) IsSendEnabledCoins(_ context.Context, _ ...sdk.Coin) error { return nil }
func (m *mockBankKeeperAnte) SendCoins(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
	return nil
}
func (m *mockBankKeeperAnte) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
	m.deductCalled = true
	return m.deductErr
}

// newAnteTestCtx returns a minimal sdk.Context backed by an in-memory store.
func newAnteTestCtx(t *testing.T, isCheckTx bool) sdk.Context {
	t.Helper()
	logger := log.NewTestLogger(t)
	keys := storetypes.NewKVStoreKeys("test")
	ms := integration.CreateMultiStore(keys, logger)
	return sdk.NewContext(ms, cmtproto.Header{Height: 1}, isCheckTx, logger)
}

// emptyNext is a no-op next handler used in tests that only check early returns.
var emptyNext sdk.AnteHandler = func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	return ctx, nil
}

// ---------------------------------------------------------------------------
// DeductFeeDecorator.AnteHandle tests
// ---------------------------------------------------------------------------

// TestDeductFee_GaslessTxSkipsFeeDeduction verifies that a gasless tx (one whose
// messages are all in the IsGaslessTx allowlist) bypasses fee deduction and passes
// directly to the next handler.
func TestDeductFee_GaslessTxSkipsFeeDeduction(t *testing.T) {
	bk := &mockBankKeeperAnte{}
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))

	// Provide a txFeeChecker that would fail if called — it should not be called.
	checkerCalled := false
	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		checkerCalled = true
		return nil, 0, sdkerrors.ErrInsufficientFee
	})

	// MsgVoteInbound is in the IsGaslessTx allowlist.
	tx := mockFeeTx{
		msgs:     []sdk.Msg{&uexecutortypes.MsgVoteInbound{}},
		gas:      200_000,
		fee:      sdk.NewCoins(sdk.NewInt64Coin("upc", 0)),
		feePayer: sdk.AccAddress([]byte("payer")),
	}

	ctx := newAnteTestCtx(t, false)
	nextCalled := false
	_, err := dfd.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	require.NoError(t, err)
	require.True(t, nextCalled, "next handler must be reached for gasless tx")
	require.False(t, bk.deductCalled, "bank must not be called for gasless tx")
	require.False(t, checkerCalled, "txFeeChecker must not be called for gasless tx")
}

// TestDeductFee_ZeroGasRejected verifies that a non-gasless tx with zero gas
// is rejected at block height > 0 when not in simulation mode.
func TestDeductFee_ZeroGasRejected(t *testing.T) {
	bk := &mockBankKeeperAnte{}
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))

	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		return sdk.NewCoins(sdk.NewInt64Coin("upc", 1000)), 0, nil
	})

	// banktypes.MsgSend is NOT gasless.
	tx := mockFeeTx{
		msgs: []sdk.Msg{&banktypes.MsgSend{}},
		gas:  0, // zero gas triggers the check
		fee:  sdk.NewCoins(sdk.NewInt64Coin("upc", 1000)),
	}

	ctx := newAnteTestCtx(t, false) // block height 1 is set in newAnteTestCtx
	_, err := dfd.AnteHandle(ctx, tx, false /*simulate*/, emptyNext)

	require.Error(t, err)
	require.True(t, sdkerrors.ErrInvalidGasLimit.Is(err), "expected ErrInvalidGasLimit, got: %v", err)
}

// TestDeductFee_SimulationSkipsZeroGasCheck verifies that simulation bypasses
// the zero-gas rejection, even for non-gasless txs.
func TestDeductFee_SimulationSkipsZeroGasCheck(t *testing.T) {
	bk := &mockBankKeeperAnte{}
	payer := sdk.AccAddress([]byte("payeraddr123456"))
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	// Register payer so GetAccount returns non-nil (needed by checkDeductFee).
	ak.SetAccount(context.Background(), authtypes.NewBaseAccountWithAddress(payer))

	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, nil /* default checker */)

	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      0, // zero gas — would be rejected if not simulating
		fee:      sdk.Coins{},
		feePayer: payer,
	}

	ctx := newAnteTestCtx(t, false)
	nextCalled := false
	_, err := dfd.AnteHandle(ctx, tx, true /*simulate=true*/, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	require.NoError(t, err)
	require.True(t, nextCalled, "next handler must be called when simulate=true")
}

// TestDeductFee_FeePayerNotFound verifies that the decorator returns an error
// when the fee payer account does not exist in state.
func TestDeductFee_FeePayerNotFound(t *testing.T) {
	bk := &mockBankKeeperAnte{}
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))

	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		return sdk.NewCoins(sdk.NewInt64Coin("upc", 1000)), 10, nil
	})

	unknownPayer := sdk.AccAddress([]byte("unknown_payer_xyz"))
	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      200_000,
		fee:      sdk.NewCoins(sdk.NewInt64Coin("upc", 1000)),
		feePayer: unknownPayer,
	}

	ctx := newAnteTestCtx(t, false)
	_, err := dfd.AnteHandle(ctx, tx, false, emptyNext)

	require.Error(t, err)
	require.True(t, sdkerrors.ErrUnknownAddress.Is(err), "expected ErrUnknownAddress when fee payer not found, got: %v", err)
}

// TestDeductFee_SuccessfulDeduction verifies the happy path: payer exists, fee
// is non-zero, bank deduct is called, and next handler is invoked.
func TestDeductFee_SuccessfulDeduction(t *testing.T) {
	bk := &mockBankKeeperAnte{}
	payer := sdk.AccAddress([]byte("validpayer12345"))
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	ak.SetAccount(context.Background(), authtypes.NewBaseAccountWithAddress(payer))

	fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 5000))
	dfd := ante.NewDeductFeeDecorator(ak, bk, nil, func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		return fee, 100, nil
	})

	tx := mockFeeTx{
		msgs:     []sdk.Msg{&banktypes.MsgSend{}},
		gas:      200_000,
		fee:      fee,
		feePayer: payer,
	}

	ctx := newAnteTestCtx(t, false)
	nextCalled := false
	_, err := dfd.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	require.NoError(t, err)
	require.True(t, nextCalled)
	require.True(t, bk.deductCalled, "bank transfer must be called on non-zero fee")
}
