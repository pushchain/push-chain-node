package evm

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
)

// evmQueryType mirrors the EvmQueryEnvelope enum from the read spec.
type evmQueryType uint8

const (
	evmQueryAccountBalance evmQueryType = 0
	evmQueryContractCall   evmQueryType = 1
	evmQueryStorageSlot    evmQueryType = 2
)

// evmBlockRefType mirrors the EvmBlockRefType enum. Only AT_NUMBER exists in v1.
type evmBlockRefType uint8

const evmBlockRefAtNumber evmBlockRefType = 0

// evmQueryEnvelope is the decoded abi.encode(EvmQueryEnvelope) query.
type evmQueryEnvelope struct {
	QueryType   evmQueryType
	RefType     evmBlockRefType
	BlockNumber uint64
	Payload     []byte
}

var (
	evmEnvelopeArgs = mustReadArgs(abi.ArgumentMarshaling{Type: "tuple", Components: []abi.ArgumentMarshaling{
		{Name: "queryType", Type: "uint8"},
		{Name: "blockRef", Type: "tuple", Components: []abi.ArgumentMarshaling{
			{Name: "refType", Type: "uint8"},
			{Name: "blockNumber", Type: "uint64"},
		}},
		{Name: "payload", Type: "bytes"},
	}})

	addressArgs        = mustReadArgs(abi.ArgumentMarshaling{Type: "address"})
	addressBytesArgs   = mustReadArgs(abi.ArgumentMarshaling{Type: "address"}, abi.ArgumentMarshaling{Type: "bytes"})
	addressBytes32Args = mustReadArgs(abi.ArgumentMarshaling{Type: "address"}, abi.ArgumentMarshaling{Type: "bytes32"})
)

func mustReadArgs(marshalings ...abi.ArgumentMarshaling) abi.Arguments {
	args := make(abi.Arguments, 0, len(marshalings))
	for i, m := range marshalings {
		if m.Name == "" {
			m.Name = fmt.Sprintf("arg%d", i)
		}
		typ, err := abi.NewType(m.Type, "", m.Components)
		if err != nil {
			panic(fmt.Sprintf("evm: invalid abi type %q: %v", m.Type, err))
		}
		args = append(args, abi.Argument{Name: m.Name, Type: typ})
	}
	return args
}

type rawEvmEnvelope struct {
	QueryType uint8
	BlockRef  struct {
		RefType     uint8
		BlockNumber uint64
	}
	Payload []byte
}

// decodeEvmQueryEnvelope decodes ReadSpec.query for eip155 chains.
func decodeEvmQueryEnvelope(query []byte) (*evmQueryEnvelope, error) {
	vals, err := evmEnvelopeArgs.Unpack(query)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack EvmQueryEnvelope: %w", err)
	}
	raw := *abi.ConvertType(vals[0], new(rawEvmEnvelope)).(*rawEvmEnvelope)

	env := &evmQueryEnvelope{
		QueryType:   evmQueryType(raw.QueryType),
		RefType:     evmBlockRefType(raw.BlockRef.RefType),
		BlockNumber: raw.BlockRef.BlockNumber,
		Payload:     raw.Payload,
	}
	if env.QueryType > evmQueryStorageSlot {
		return nil, fmt.Errorf("unknown EvmQueryType %d", env.QueryType)
	}
	if env.RefType != evmBlockRefAtNumber {
		return nil, fmt.Errorf("unsupported EvmBlockRefType %d", env.RefType)
	}
	return env, nil
}

// decodeAccountBalancePayload decodes abi.encode(address target).
func decodeAccountBalancePayload(payload []byte) (ethcommon.Address, error) {
	vals, err := addressArgs.Unpack(payload)
	if err != nil {
		return ethcommon.Address{}, fmt.Errorf("failed to unpack AccountBalance payload: %w", err)
	}
	return vals[0].(ethcommon.Address), nil
}

// decodeContractCallPayload decodes abi.encode(address target, bytes callData).
func decodeContractCallPayload(payload []byte) (ethcommon.Address, []byte, error) {
	vals, err := addressBytesArgs.Unpack(payload)
	if err != nil {
		return ethcommon.Address{}, nil, fmt.Errorf("failed to unpack ContractCall payload: %w", err)
	}
	return vals[0].(ethcommon.Address), vals[1].([]byte), nil
}

// decodeStorageSlotPayload decodes abi.encode(address contractAddr, bytes32 slot).
func decodeStorageSlotPayload(payload []byte) (ethcommon.Address, ethcommon.Hash, error) {
	vals, err := addressBytes32Args.Unpack(payload)
	if err != nil {
		return ethcommon.Address{}, ethcommon.Hash{}, fmt.Errorf("failed to unpack StorageSlot payload: %w", err)
	}
	slot := vals[1].([32]byte)
	return vals[0].(ethcommon.Address), ethcommon.Hash(slot), nil
}
