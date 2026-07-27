package usigverifier

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	cmn "github.com/cosmos/evm/precompiles/common"
)

const (
	USigVerifierPrecompileAddress = "0xEC00000000000000000000000000000000000001"
	// VerifyEd25519Gas is the gas cost for verifying an Ed25519 signature.
	VerifyEd25519Gas uint64 = 4000
	// VerifyEd25519RawMessageGas matches VerifyEd25519Gas — same Ed25519
	// verification cost, only the message-prep step differs (no hex encoding).
	VerifyEd25519RawMessageGas uint64 = 4000
)

var _ vm.PrecompiledContract = &Precompile{}

var (
	// Embed abi json file to the executable binary. Needed when importing as dependency.
	//
	//go:embed abi.json
	f   []byte
	ABI abi.ABI
)

func init() {
	// cosmos/evm v0.7.0 removed cmn.LoadABI, which unwrapped the "abi" field of a
	// Hardhat artifact. This abi.json is such an artifact (hh-sol-artifact-1), not
	// a bare ABI array like the upstream precompiles use, so unwrap it here before
	// handing the array to abi.JSON.
	var artifact struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(f, &artifact); err != nil {
		panic(err)
	}
	var err error
	ABI, err = abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		panic(err)
	}
}

// Name identifies the precompile. geth 1.17 added Name() to the
// vm.PrecompiledContract interface.
func (p Precompile) Name() string {
	return "usigverifier"
}

// Precompile defines the precompile
type Precompile struct {
	cmn.Precompile
	abi.ABI
}

// return address of the precompile
func GetAddress() common.Address {
	return common.HexToAddress(USigVerifierPrecompileAddress)
}

func NewPrecompile() (*Precompile, error) {
	p := &Precompile{
		Precompile: cmn.Precompile{
			KvGasConfig:          storetypes.KVGasConfig(),
			TransientKVGasConfig: storetypes.TransientGasConfig(),
		},
		ABI: ABI,
	}

	p.SetAddress(GetAddress())

	return p, nil
}

func (p Precompile) RequiredGas(input []byte) uint64 {
	// NOTE: This check avoid panicking when trying to decode the method ID
	if len(input) < 4 {
		return 0
	}

	methodID := input[:4]
	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		return 0
	}

	switch method.Name {
	case VerifyEd25519Method:
		return VerifyEd25519Gas
	case VerifyEd25519RawMessageMethod:
		return VerifyEd25519RawMessageGas
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
	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		return nil, err
	}

	argsBz := contract.Input[4:]
	args, err := method.Inputs.Unpack(argsBz)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case VerifyEd25519Method:
		bz, err = p.VerifyEd25519(method, args)
	case VerifyEd25519RawMessageMethod:
		bz, err = p.VerifyEd25519RawMessage(method, args)
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
