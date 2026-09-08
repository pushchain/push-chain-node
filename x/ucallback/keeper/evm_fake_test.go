package keeper_test

import (
	"context"
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"

	ethcommon "github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	pchaintypes "github.com/pushchain/push-chain-node/types"
	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// recordedCall captures one DerivedEVMCall so tests can assert on what was sent to
// the contract, not merely that something was.
type recordedCall struct {
	from     ethcommon.Address
	contract ethcommon.Address
	method   string
	args     []interface{}
	nonce    *uint64
	gasLimit *big.Int
	isModule bool
}

type fakeEVM struct {
	calls []recordedCall

	// per-call outcomes, consumed in order; the zero value means success
	vmErrors []string
	// revertData is returned alongside every vmError — set it to a custom-error
	// selector to exercise the classification path.
	revertData []byte
	callErr    error
	// gasUsed overrides the receipt's reported gas; zero means the default.
	gasUsed uint64
}

var _ types.EVMKeeper = (*fakeEVM)(nil)

// DerivedEVMCallWithData is the entry point production uses. It unpacks the call
// data back into method + args, so these tests also prove our ABI packing
// round-trips through the real UniversalCallback ABI rather than just recording
// whatever the keeper claims it sent.
func (f *fakeEVM) DerivedEVMCallWithData(
	_ sdk.Context,
	from ethcommon.Address,
	contract *ethcommon.Address,
	data []byte,
	_, _, isModuleSender bool,
	_, gasLimit *big.Int,
	manualNonce *uint64,
) (*evmtypes.MsgEthereumTxResponse, error) {
	method, args, err := unpackCallbackCall(data)
	if err != nil {
		return nil, err
	}

	var n *uint64
	if manualNonce != nil {
		v := *manualNonce
		n = &v
	}
	var to ethcommon.Address
	if contract != nil {
		to = *contract
	}
	f.calls = append(f.calls, recordedCall{
		from: from, contract: to, method: method, args: args,
		nonce: n, gasLimit: gasLimit, isModule: isModuleSender,
	})

	if f.callErr != nil {
		return nil, f.callErr
	}

	gas := uint64(21_000)
	if f.gasUsed != 0 {
		gas = f.gasUsed
	}
	res := &evmtypes.MsgEthereumTxResponse{
		Hash:    "0xEVMTX",
		GasUsed: gas,
	}
	if len(f.vmErrors) > 0 {
		vmErr := f.vmErrors[0]
		f.vmErrors = f.vmErrors[1:]
		// An empty entry means "this call succeeds" — queues use it to let an
		// earlier call through and fail a later one.
		if vmErr != "" {
			res.VmError = vmErr
			res.Ret = f.revertData

			// The real layer returns the response AND an error on a revert
			// (call_evm.go:323). Returning only the response here would let a
			// classification bug that trips on callErr pass unnoticed.
			return res, fmt.Errorf("%s: ret 0x%x", res.VmError, res.Ret)
		}
	}
	return res, nil
}

// unpackCallbackCall turns raw call data back into a method name and its arguments
// using the real ABI.
func unpackCallbackCall(data []byte) (string, []interface{}, error) {
	if len(data) < 4 {
		return "", nil, fmt.Errorf("call data too short: %d bytes", len(data))
	}
	parsed, err := types.ParseUniversalCallbackABI()
	if err != nil {
		return "", nil, err
	}
	m, err := parsed.MethodById(data[:4])
	if err != nil {
		return "", nil, err
	}
	args, err := m.Inputs.Unpack(data[4:])
	if err != nil {
		return "", nil, fmt.Errorf("unpack %s: %w", m.Name, err)
	}
	return m.Name, args, nil
}

func (f *fakeEVM) lastCall() recordedCall { return f.calls[len(f.calls)-1] }

// callsTo counts contract calls by method. Fulfilment now issues two — the
// callback and the gas report — so assertions name the method rather than a total.
func (f *fakeEVM) callsTo(method string) int {
	n := 0
	for _, c := range f.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

// firstCallTo returns the first call to a method, for asserting its arguments.
func (f *fakeEVM) firstCallTo(method string) (recordedCall, bool) {
	for _, c := range f.calls {
		if c.method == method {
			return c, true
		}
	}
	return recordedCall{}, false
}

// fakeAccount resolves the module account. The nonce is no longer faked — it lives
// in x/ucallback's own state now, so the tests exercise the real counter.
type fakeAccount struct {
	addr sdk.AccAddress
}

var _ types.AccountKeeper = (*fakeAccount)(nil)

func (f *fakeAccount) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return authtypes.NewEmptyModuleAccount(types.ModuleName)
}

// fakeBank records what the module moved and destroyed. Tracks a notional contract
// balance so a take-out larger than the contract holds fails the way bank would.
type fakeBank struct {
	contractBalance sdkmath.Int
	sentFrom        sdk.AccAddress
	sentTo          string
	sentAmount      sdkmath.Int
	burnedFrom      string
	burned          sdkmath.Int

	sendErr error
	burnErr error
}

var _ types.BankKeeper = (*fakeBank)(nil)

func newFakeBank() *fakeBank {
	return &fakeBank{
		contractBalance: sdkmath.NewInt(0),
		sentAmount:      sdkmath.NewInt(0),
		burned:          sdkmath.NewInt(0),
	}
}

func (b *fakeBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, module string, amt sdk.Coins) error {
	if b.sendErr != nil {
		return b.sendErr
	}
	a := amt.AmountOf(pchaintypes.BaseDenom)
	if a.GT(b.contractBalance) {
		return fmt.Errorf("insufficient funds: %s < %s", b.contractBalance, a)
	}
	b.contractBalance = b.contractBalance.Sub(a)
	b.sentFrom, b.sentTo = from, module
	b.sentAmount = b.sentAmount.Add(a)
	return nil
}

func (b *fakeBank) BurnCoins(_ context.Context, module string, amt sdk.Coins) error {
	if b.burnErr != nil {
		return b.burnErr
	}
	b.burnedFrom = module
	b.burned = b.burned.Add(amt.AmountOf(pchaintypes.BaseDenom))
	return nil
}

func (b *fakeBank) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.contractBalance)
}

// fakeFeeMarket serves a fixed base fee so gas pricing in tests is exact.
type fakeFeeMarket struct{ baseFee sdkmath.LegacyDec }

var _ types.FeeMarketKeeper = (*fakeFeeMarket)(nil)

func (f *fakeFeeMarket) GetBaseFee(sdk.Context) sdkmath.LegacyDec { return f.baseFee }

// contractAccAddr is UniversalCallback's account, the source of the escrow.
func contractAccAddr() sdk.AccAddress {
	return sdk.AccAddress(ethcommon.HexToAddress(
		uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"].Address).Bytes())
}
