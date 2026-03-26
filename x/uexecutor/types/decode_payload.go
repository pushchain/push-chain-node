package types

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// EncodeUniversalPayloadToRaw ABI-encodes a UniversalPayload into a hex string suitable for raw_payload.
// This is the inverse of DecodeUniversalPayloadEVM — used in tests and migration.
func EncodeUniversalPayloadToRaw(up *UniversalPayload) (string, error) {
	if up == nil {
		return "", nil
	}

	components := []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
		{Name: "gasLimit", Type: "uint256"},
		{Name: "maxFeePerGas", Type: "uint256"},
		{Name: "maxPriorityFeePerGas", Type: "uint256"},
		{Name: "nonce", Type: "uint256"},
		{Name: "deadline", Type: "uint256"},
		{Name: "vType", Type: "uint8"},
	}
	tupleType, err := abi.NewType("tuple", "UniversalPayload", components)
	if err != nil {
		return "", fmt.Errorf("failed to create tuple type: %w", err)
	}
	args := abi.Arguments{{Type: tupleType}}

	to := common.HexToAddress(up.To)
	value, _ := new(big.Int).SetString(up.Value, 10)
	if value == nil {
		value = big.NewInt(0)
	}

	var dataBytes []byte
	cleanData := strings.TrimPrefix(up.Data, "0x")
	if cleanData != "" {
		dataBytes, err = hex.DecodeString(cleanData)
		if err != nil {
			return "", fmt.Errorf("invalid data hex: %w", err)
		}
	}

	gasLimit, _ := new(big.Int).SetString(up.GasLimit, 10)
	if gasLimit == nil {
		gasLimit = big.NewInt(0)
	}
	maxFeePerGas, _ := new(big.Int).SetString(up.MaxFeePerGas, 10)
	if maxFeePerGas == nil {
		maxFeePerGas = big.NewInt(0)
	}
	maxPriorityFeePerGas, _ := new(big.Int).SetString(up.MaxPriorityFeePerGas, 10)
	if maxPriorityFeePerGas == nil {
		maxPriorityFeePerGas = big.NewInt(0)
	}
	nonce, _ := new(big.Int).SetString(up.Nonce, 10)
	if nonce == nil {
		nonce = big.NewInt(0)
	}
	deadline, _ := new(big.Int).SetString(up.Deadline, 10)
	if deadline == nil {
		deadline = big.NewInt(0)
	}

	type payload struct {
		To                   common.Address
		Value                *big.Int
		Data                 []byte
		GasLimit             *big.Int
		MaxFeePerGas         *big.Int
		MaxPriorityFeePerGas *big.Int
		Nonce                *big.Int
		Deadline             *big.Int
		VType                uint8
	}

	packed, err := args.Pack(payload{
		To: to, Value: value, Data: dataBytes,
		GasLimit: gasLimit, MaxFeePerGas: maxFeePerGas, MaxPriorityFeePerGas: maxPriorityFeePerGas,
		Nonce: nonce, Deadline: deadline, VType: uint8(up.VType),
	})
	if err != nil {
		return "", fmt.Errorf("failed to pack payload: %w", err)
	}
	return "0x" + hex.EncodeToString(packed), nil
}

// DecodeRawPayload decodes raw hex-encoded payload bytes based on the source chain namespace.
// sourceChain is in CAIP-2 format (e.g., "eip155:11155111" or "solana:devnet").
func DecodeRawPayload(rawPayload string, sourceChain string) (*UniversalPayload, error) {
	namespace := strings.Split(sourceChain, ":")[0]
	switch namespace {
	case "eip155":
		return DecodeUniversalPayloadEVM(rawPayload)
	default:
		return nil, fmt.Errorf("unsupported chain namespace for payload decoding: %s", namespace)
	}
}

// DecodeUniversalPayloadEVM decodes an ABI-encoded UniversalPayload from a hex string.
// The hex string should contain ABI-encoded tuple data as emitted by the EVM gateway contract.
// Ported from universalClient/chains/evm/event_parser.go:decodeUniversalPayload.
func DecodeUniversalPayloadEVM(hexStr string) (*UniversalPayload, error) {
	if hexStr == "" || strings.TrimSpace(hexStr) == "" {
		return nil, nil
	}

	clean := strings.TrimPrefix(hexStr, "0x")
	if clean == "" {
		return nil, nil
	}

	bz, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("hex decode: %w", err)
	}

	if len(bz) == 0 {
		return nil, nil
	}

	if len(bz) < 32 {
		return nil, fmt.Errorf("insufficient data length: got %d, need at least 32", len(bz))
	}

	// Define the UniversalPayload struct components matching the Solidity struct
	components := []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
		{Name: "gasLimit", Type: "uint256"},
		{Name: "maxFeePerGas", Type: "uint256"},
		{Name: "maxPriorityFeePerGas", Type: "uint256"},
		{Name: "nonce", Type: "uint256"},
		{Name: "deadline", Type: "uint256"},
		{Name: "vType", Type: "uint8"},
	}

	tupleType, err := abi.NewType("tuple", "UniversalPayload", components)
	if err != nil {
		return nil, fmt.Errorf("failed to create tuple type: %w", err)
	}

	args := abi.Arguments{{Type: tupleType}}

	decoded, err := args.Unpack(bz)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack payload: %w", err)
	}

	if len(decoded) != 1 {
		return nil, fmt.Errorf("expected 1 decoded value, got %d", len(decoded))
	}

	// Extract the struct fields using reflection (go-ethereum returns anonymous structs)
	v := reflect.ValueOf(decoded[0])
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %T", decoded[0])
	}

	t := v.Type()
	getField := func(name string) reflect.Value {
		if f, ok := t.FieldByName(name); ok {
			return v.FieldByIndex(f.Index)
		}
		return reflect.Value{}
	}

	to, ok := getField("To").Interface().(common.Address)
	if !ok {
		return nil, fmt.Errorf("expected address for 'to', got %T", getField("To").Interface())
	}
	value, ok := getField("Value").Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'value', got %T", getField("Value").Interface())
	}
	dataBytes, ok := getField("Data").Interface().([]byte)
	if !ok {
		return nil, fmt.Errorf("expected []byte for 'data', got %T", getField("Data").Interface())
	}
	gasLimit, ok := getField("GasLimit").Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'gasLimit'")
	}
	maxFeePerGas, ok := getField("MaxFeePerGas").Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'maxFeePerGas'")
	}
	maxPriorityFeePerGas, ok := getField("MaxPriorityFeePerGas").Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'maxPriorityFeePerGas'")
	}
	nonce, ok := getField("Nonce").Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'nonce'")
	}
	deadline, ok := getField("Deadline").Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int for 'deadline'")
	}
	vType, ok := getField("VType").Interface().(uint8)
	if !ok {
		return nil, fmt.Errorf("expected uint8 for 'vType', got %T", getField("VType").Interface())
	}

	return &UniversalPayload{
		To:                   to.Hex(),
		Value:                value.String(),
		Data:                 "0x" + hex.EncodeToString(dataBytes),
		GasLimit:             gasLimit.String(),
		MaxFeePerGas:         maxFeePerGas.String(),
		MaxPriorityFeePerGas: maxPriorityFeePerGas.String(),
		Nonce:                nonce.String(),
		Deadline:             deadline.String(),
		VType:                VerificationType(vType),
	}, nil
}
