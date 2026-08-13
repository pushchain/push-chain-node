// Package bank extends the upstream cosmos/evm bank precompile with a burn
// method, without forking cosmos/evm.
//
// The upstream precompile is read-only — balances, totalSupply, supplyOf — and its
// Execute switch is closed, so an unknown selector falls through to a default
// error. This type registers at the same address, intercepts burn in Run, and
// delegates everything else upstream. Contracts see one IBank interface.
//
// Three overrides are load-bearing and must move together:
//
//   - Run          routes burn here; everything else to the embedded precompile.
//   - RequiredGas  the embedded one prices against the upstream ABI, which has no
//     burn, so it would charge nothing for it.
//   - IsTransaction the embedded one returns false unconditionally, because bank
//     is read-only upstream. Left inherited, burn would be permitted inside a
//     STATICCALL — state mutation in a context the EVM guarantees is pure.
package bank

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankprecompile "github.com/cosmos/evm/precompiles/bank"
	cmn "github.com/cosmos/evm/precompiles/common"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	pchaintypes "github.com/pushchain/push-chain-node/types"
)

const (
	// BurnMethod is the only method this type handles itself.
	BurnMethod = "burn"

	// BurnGas is the flat cost of a burn, on top of the intrinsic call cost. Two
	// bank writes (send to module, burn from module) plus a supply update.
	BurnGas = 40_000
)

//go:embed abi.json
var abiJSON []byte

// BankKeeper is the slice of x/bank a burn needs. Deliberately not cmn.BankKeeper:
// that interface is read-only and has neither of these.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// Precompile is the upstream bank precompile plus burn.
type Precompile struct {
	*bankprecompile.Precompile

	// extABI is the upstream ABI plus burn. Held as a named field rather than
	// embedded: the upstream type already embeds an abi.ABI, and two embedded ABIs
	// would resolve by depth in ways that are easy to get wrong at a call site.
	extABI     abi.ABI
	bankKeeper BankKeeper
}

// NewPrecompile builds the extended bank precompile. It registers at the upstream
// bank address, inherited from the embedded type.
func NewPrecompile(bankKeeper BankKeeper, upstream *bankprecompile.Precompile) (*Precompile, error) {
	var parsed struct {
		ABI abi.ABI `json:"abi"`
	}
	if err := json.Unmarshal(abiJSON, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse extended bank ABI: %w", err)
	}
	if _, ok := parsed.ABI.Methods[BurnMethod]; !ok {
		return nil, fmt.Errorf("extended bank ABI is missing %s", BurnMethod)
	}
	return &Precompile{
		Precompile: upstream,
		extABI:     parsed.ABI,
		bankKeeper: bankKeeper,
	}, nil
}

// isBurnCall reports whether the call data selects burn.
func (p Precompile) isBurnCall(input []byte) bool {
	if len(input) < 4 {
		return false
	}
	return bytes.Equal(input[:4], p.extABI.Methods[BurnMethod].ID)
}

// RequiredGas prices burn here and defers everything else upstream.
func (p Precompile) RequiredGas(input []byte) uint64 {
	if p.isBurnCall(input) {
		return BurnGas
	}
	return p.Precompile.RequiredGas(input)
}

// IsTransaction marks burn as state-mutating. SetupABI consults this to reject a
// write attempted under STATICCALL; without it burn would run in a context the EVM
// promises is side-effect free.
func (p Precompile) IsTransaction(method *abi.Method) bool {
	return method != nil && method.Name == BurnMethod
}

// Run intercepts burn and delegates the rest to the upstream precompile.
func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readonly bool) ([]byte, error) {
	if !p.isBurnCall(contract.Input) {
		return p.Precompile.Run(evm, contract, readonly)
	}

	// RunNativeAction supplies the cache context, snapshots the multistore and
	// files a journal entry, so an EVM revert later in the transaction unwinds the
	// burn along with everything else.
	return p.Precompile.RunNativeAction(evm, contract, func(ctx sdk.Context) ([]byte, error) {
		method, args, err := cmn.SetupABI(p.extABI, contract, readonly, p.IsTransaction)
		if err != nil {
			return nil, err
		}
		return p.burn(ctx, contract, method, args)
	})
}

// burn destroys native tokens belonging to the calling account.
//
// The amount comes from the caller's own balance and nowhere else: there is no
// address argument, so the authorisation question does not arise. A contract can
// only ever burn what it holds.
func (p Precompile) burn(
	ctx sdk.Context,
	contract *vm.Contract,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("burn expects 1 argument, got %d", len(args))
	}
	amount, ok := args[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("burn amount must be uint256")
	}
	if amount.Sign() <= 0 {
		return nil, fmt.Errorf("burn amount must be positive")
	}

	// Caller() is the immediate caller, never tx.origin — and it stays correct under
	// DELEGATECALL. opDelegateCall passes scope.Contract.Address() as the caller,
	// and evm.DelegateCall short-circuits to RunPrecompiledContract with that value
	// (evm.go:376) before reaching the NewContract(originCaller, ...) line that
	// would otherwise shift it to the caller's caller. So a contract delegatecalling
	// this precompile burns its own balance, not its caller's.
	return p.burnFrom(ctx, contract.Caller(), method, args)
}

// burnFrom performs the burn for an already-resolved caller.
func (p Precompile) burnFrom(
	ctx sdk.Context,
	caller ethcommon.Address,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	amount, ok := args[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("burn amount must be uint256")
	}
	if amount.Sign() <= 0 {
		return nil, fmt.Errorf("burn amount must be positive")
	}

	burner := sdk.AccAddress(caller.Bytes())

	coins := sdk.NewCoins(sdk.NewCoin(
		pchaintypes.BaseDenom, sdkmath.NewIntFromBigInt(amount),
	))

	// Two steps, mirroring x/uexecutor's DeductAndBurnFees: only a module account
	// with the Burner permission may burn, so the coins move there first. Both run
	// inside the snapshot RunNativeAction took, so a failure in the second leaves
	// no trace of the first.
	if err := p.bankKeeper.SendCoinsFromAccountToModule(
		ctx, burner, evmtypes.ModuleName, coins,
	); err != nil {
		return nil, fmt.Errorf("failed to transfer coins for burn: %w", err)
	}
	if err := p.bankKeeper.BurnCoins(ctx, evmtypes.ModuleName, coins); err != nil {
		return nil, fmt.Errorf("failed to burn coins: %w", err)
	}

	return method.Outputs.Pack(true)
}
