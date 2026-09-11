package usigverifier

import (
	"embed"
	"encoding/binary"
	"fmt"
	"math"

	storetypes "cosmossdk.io/store/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	cmn "github.com/cosmos/evm/precompiles/common"
)

const (
	USigVerifierPrecompileAddress = "0xEC00000000000000000000000000000000000001"
	// VerifyEd25519Gas is the gas cost for verifying an Ed25519 signature over a
	// bytes32 digest. The verified message is always the 66-byte ASCII hex form
	// of that digest, so the work does not vary with the calldata — flat is the
	// honest price here.
	VerifyEd25519Gas uint64 = 4000
	// VerifyEd25519RawMessageBaseGas is the fixed part of a raw-message
	// verification: the Ed25519 curve arithmetic, which dominates below ~8 KiB.
	VerifyEd25519RawMessageBaseGas uint64 = 4000
	// VerifyEd25519RawMessagePerWordGas is charged for every 32-byte word of the
	// message on top of the base, so that the SHA-512 pass Ed25519 makes over the
	// whole message is paid for. Priced off the EVM SHA-256 precompile, which
	// charges 12 gas per 32-byte word for comparable hashing work.
	VerifyEd25519RawMessagePerWordGas uint64 = 12
	// MaxEd25519MessageBytes hard-caps the message a raw-message verification
	// accepts (128 KiB, the same limit used for gateway payloads). This is a view
	// method, so a contract can hold one large message in memory and loop
	// STATICCALLs over it, paying the calldata only once; on that path a price
	// curve alone is not a defence, the size has to be bounded outright.
	MaxEd25519MessageBytes = 128 * 1024
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

var ABI abi.ABI

func init() {
	var err error
	ABI, err = cmn.LoadABI(f, "abi.json")
	if err != nil {
		panic(err)
	}
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

// RequiredGas is charged before Run executes, so it runs on unvalidated,
// attacker-controlled calldata and must never panic.
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
		return verifyEd25519RawMessageGas(rawMessageLen(input))
	default:
		return p.Precompile.RequiredGas(input, p.IsTransaction(method))
	}
}

// verifyEd25519RawMessageGas prices a raw-message verification: a flat base for
// the curve arithmetic plus a per-word term for the hash pass over the message.
// A msgLen past MaxEd25519MessageBytes is clamped — Run rejects those calls, and
// clamping keeps the charge bounded (and overflow-free) for a calldata that
// declares an absurd length.
func verifyEd25519RawMessageGas(msgLen uint64) uint64 {
	if msgLen > MaxEd25519MessageBytes {
		msgLen = MaxEd25519MessageBytes
	}

	words := (msgLen + 31) / 32

	return VerifyEd25519RawMessageBaseGas + words*VerifyEd25519RawMessagePerWordGas
}

// rawMessageLen recovers the declared length of the `message` argument of
// verifyEd25519RawMessage(bytes,bytes,bytes) straight out of the ABI-encoded
// calldata, without decoding the payload. `input` includes the 4-byte method ID.
//
// Layout: one 32-byte head slot per argument holding the offset of its tail,
// then each dynamic tail starting with a 32-byte length. `message` is argument
// index 1. Calldata that does not parse is priced at 0 extra — Run reverts on it
// before any verification happens — while a length too large for a uint64 is
// reported as the maximum so it prices at the cap instead of the base.
func rawMessageLen(input []byte) uint64 {
	const (
		wordSize      = 32
		messageArgIdx = 1
	)

	if len(input) < 4 {
		return 0
	}
	args := input[4:]

	head := messageArgIdx * wordSize
	if len(args) < head+wordSize {
		return 0
	}

	offset, ok := abiWordToUint64(args[head : head+wordSize])
	if !ok || offset > uint64(len(args)) || uint64(len(args))-offset < wordSize {
		return 0
	}

	length, ok := abiWordToUint64(args[offset : offset+wordSize])
	if !ok {
		return math.MaxUint64
	}

	return length
}

// abiWordToUint64 reads a big-endian 32-byte ABI word as a uint64. ok is false
// when the word does not fit one, i.e. its top 24 bytes are not all zero.
func abiWordToUint64(word []byte) (value uint64, ok bool) {
	for _, b := range word[:len(word)-8] {
		if b != 0 {
			return 0, false
		}
	}

	return binary.BigEndian.Uint64(word[len(word)-8:]), true
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
