package svm

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// solanaQueryType mirrors the SolanaQueryEnvelope enum from the read spec.
type solanaQueryType uint8

const (
	solanaQueryLamportBalance  solanaQueryType = 0
	solanaQuerySPLTokenAccount solanaQueryType = 1
	solanaQueryRawAccountData  solanaQueryType = 2
)

// solanaQueryEnvelope is the decoded abi.encode(SolanaQueryEnvelope) query —
// ABI-encoded because it is built by UniversalCallback.sol on Push EVM.
// The target account pubkey travels in ReadSpec.account.owner (32 bytes), not here.
type solanaQueryEnvelope struct {
	QueryType solanaQueryType
	MinSlot   uint64
	Payload   []byte // empty for all v1 query types
}

var svmEnvelopeArgs = func() abi.Arguments {
	tupleTy, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "queryType", Type: "uint8"},
		{Name: "slotRef", Type: "tuple", Components: []abi.ArgumentMarshaling{
			{Name: "minSlot", Type: "uint64"},
		}},
		{Name: "payload", Type: "bytes"},
	})
	if err != nil {
		panic(fmt.Sprintf("svm: invalid envelope abi type: %v", err))
	}
	return abi.Arguments{{Name: "envelope", Type: tupleTy}}
}()

type rawSvmEnvelope struct {
	QueryType uint8
	SlotRef   struct {
		MinSlot uint64
	}
	Payload []byte
}

// decodeSolanaQueryEnvelope decodes ReadSpec.query for solana chains.
func decodeSolanaQueryEnvelope(query []byte) (*solanaQueryEnvelope, error) {
	vals, err := svmEnvelopeArgs.Unpack(query)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack SolanaQueryEnvelope: %w", err)
	}
	raw := *abi.ConvertType(vals[0], new(rawSvmEnvelope)).(*rawSvmEnvelope)

	env := &solanaQueryEnvelope{
		QueryType: solanaQueryType(raw.QueryType),
		MinSlot:   raw.SlotRef.MinSlot,
		Payload:   raw.Payload,
	}
	if env.QueryType > solanaQueryRawAccountData {
		return nil, fmt.Errorf("unknown SolanaQueryType %d", env.QueryType)
	}
	return env, nil
}
