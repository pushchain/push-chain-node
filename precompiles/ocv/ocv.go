package ocv

import (
	"embed"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/cosmos/evm/x/vm/core/vm"

	cmn "github.com/cosmos/evm/precompiles/common"
)

const (
	OcvPrecompileAddress = "0x0000000000000000000000000000000000000901"
	// VerifyTxHashGas is the gas cost for verifying transaction hash.
	VerifyTxHashGas uint64 = 4000
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

// Precompile defines the precompile
type Precompile struct {
	cmn.Precompile
	utvKeeper UtvKeeper
}

// return address of the precompile
func GetAddress() common.Address {
	return common.HexToAddress(OcvPrecompileAddress)
}

func NewPrecompile() (*Precompile, error) {
	ocvABI, err := cmn.LoadABI(f, "abi.json")

	if err != nil {
		return nil, err
	}

	p := &Precompile{
		Precompile: cmn.Precompile{
			ABI:                  ocvABI,
			KvGasConfig:          storetypes.KVGasConfig(),
			TransientKVGasConfig: storetypes.TransientGasConfig(),
		},
	}

	p.SetAddress(GetAddress())

	return p, nil
}

// NewPrecompileWithUtv creates a new OCV precompile with UTV keeper dependency
func NewPrecompileWithUtv(utvKeeper UtvKeeper) (*Precompile, error) {
	p, err := NewPrecompile()
	if err != nil {
		return nil, err
	}

	p.utvKeeper = utvKeeper
	return p, nil
}

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

	switch method.Name {
	case VerifyTxHashMethod:
		return VerifyTxHashGas
	default:
		return p.Precompile.RequiredGas(input, p.IsTransaction(method))
	}
}

func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) (bz []byte, err error) {
	if len(contract.Input) < 4 {
		return nil, vm.ErrExecutionReverted
	}

	methodID := contract.Input[:4]
	// NOTE: this function iterates over the method map and returns
	// the method with the given ID
	method, err := p.MethodById(methodID)
	if err != nil {
		return nil, err
	}

	argsBz := contract.Input[4:]
	args, err := method.Inputs.Unpack(argsBz)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case VerifyTxHashMethod:
		fmt.Println("VerifyTxHashMethod called")
		bz, err = p.VerifyTxHash(method, args)
	default:
		return nil, fmt.Errorf(cmn.ErrUnknownMethod, method.Name)
	}

	if err != nil {
		return nil, err
	}

	return bz, nil
}

// IsTransaction checks if the given method name corresponds to a transaction or query.
func (Precompile) IsTransaction(method *abi.Method) bool {
	return false // default is false as there are no txs in this precompile
}
