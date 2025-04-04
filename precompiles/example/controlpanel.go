package controlpanel

import (
	"embed"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	cmn "github.com/evmos/os/precompiles/common"
	"github.com/evmos/os/x/evm/core/vm"
	"github.com/rollchains/pchain/x/precompileexa/keeper"
)

const (
	ControlPanelPrecompileAddress = "0x0000000000000000000000000000000000000901"
)

var _ vm.PrecompiledContract = &Precompile{}
var PrecompileAddress = common.HexToAddress(ControlPanelPrecompileAddress)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

// Precompile defines the precompile
type Precompile struct {
	cmn.Precompile
	keeper keeper.Keeper
}

func GetAddress() common.Address {
	return common.HexToAddress(ControlPanelPrecompileAddress)
}

// NewPrecompile creates a new Precompile instance implementing the
// PrecompiledContract interface.
func NewPrecompile(
	keeper keeper.Keeper,
) (*Precompile, error) {
	newABI, err := LoadABI()
	if err != nil {
		return nil, err
	}

	// NOTE: we set an empty gas configuration to avoid extra gas costs
	// during the run execution
	p := &Precompile{
		Precompile: cmn.Precompile{
			ABI:                  newABI,
			KvGasConfig:          storetypes.KVGasConfig(),
			TransientKVGasConfig: storetypes.TransientGasConfig(),
		},
		keeper: keeper,
	}

	p.SetAddress(PrecompileAddress)

	return p, nil
}

func LoadABI() (abi.ABI, error) {
	return cmn.LoadABI(f, "abi.json")
}

// RequiredGas calculates the precompiled contract's base gas rate.
func (p Precompile) RequiredGas(input []byte) uint64 {
	// NOTE: This check avoid panicking when trying to decode the method ID
	if len(input) < 4 {
		return 0
	}

	methodID := input[:4]
	method, err := p.MethodById(methodID)
	if err != nil {
		return 0
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method))
}

// Run executes the precompiled contract methods defined in the ABI.
func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) (bz []byte, err error) {
	ctx, stateDB, snapshot, method, initialGas, args, err := p.RunSetup(evm, contract, readOnly, p.IsTransaction)
	if err != nil {
		return nil, err
	}

	// This handles any out of gas errors that may occur during the execution of a precompile query.
	// It avoids panics and returns the out of gas error so the EVM can continue gracefully.
	defer cmn.HandleGasError(ctx, contract, initialGas, &err)()

	switch method.Name {
	// txs
	case UpdateParamsMethod:
		bz, err = p.UpdateParams(ctx, evm.Origin, contract, stateDB, method, args)

	// queries
	case ParamsMethod:
		bz, err = p.GetParams(ctx, contract, method, args)
	default:
		return nil, fmt.Errorf(cmn.ErrUnknownMethod, method.Name)
	}

	if err != nil {
		return nil, err
	}

	cost := ctx.GasMeter().GasConsumed() - initialGas

	if !contract.UseGas(cost) {
		return nil, vm.ErrOutOfGas
	}

	if err := p.AddJournalEntries(stateDB, snapshot); err != nil {
		return nil, err
	}

	return bz, nil
}

// IsTransaction checks if the given method name corresponds to a transaction or query.
func (Precompile) IsTransaction(method *abi.Method) bool {
	switch method.Name {
	case UpdateParamsMethod:
		return true
	default:
		return false
	}
}
