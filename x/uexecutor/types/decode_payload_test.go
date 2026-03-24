package types_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

// abiEncodeUniversalPayload encodes a UniversalPayload into ABI-encoded hex
// (same format the EVM gateway contract emits).
func abiEncodeUniversalPayload(
	to common.Address,
	value *big.Int,
	data []byte,
	gasLimit *big.Int,
	maxFeePerGas *big.Int,
	maxPriorityFeePerGas *big.Int,
	nonce *big.Int,
	deadline *big.Int,
	vType uint8,
) (string, error) {
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
		return "", err
	}
	args := abi.Arguments{{Type: tupleType}}

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
		To:                   to,
		Value:                value,
		Data:                 data,
		GasLimit:             gasLimit,
		MaxFeePerGas:         maxFeePerGas,
		MaxPriorityFeePerGas: maxPriorityFeePerGas,
		Nonce:                nonce,
		Deadline:             deadline,
		VType:                vType,
	})
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func TestDecodeUniversalPayloadEVM(t *testing.T) {
	t.Run("decodes valid ABI-encoded payload", func(t *testing.T) {
		to := common.HexToAddress("0x000000000000000000000000000000000000beef")
		encoded, err := abiEncodeUniversalPayload(
			to,
			big.NewInt(1000),
			[]byte{0xde, 0xad, 0xbe, 0xef},
			big.NewInt(21000),
			big.NewInt(1000000000),
			big.NewInt(200000000),
			big.NewInt(1),
			big.NewInt(9999999999),
			1, // signedVerification
		)
		require.NoError(t, err)

		decoded, err := types.DecodeUniversalPayloadEVM(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, to.Hex(), decoded.To)
		require.Equal(t, "1000", decoded.Value)
		require.Equal(t, "0xdeadbeef", decoded.Data)
		require.Equal(t, "21000", decoded.GasLimit)
		require.Equal(t, "1000000000", decoded.MaxFeePerGas)
		require.Equal(t, "200000000", decoded.MaxPriorityFeePerGas)
		require.Equal(t, "1", decoded.Nonce)
		require.Equal(t, "9999999999", decoded.Deadline)
		require.Equal(t, types.VerificationType(1), decoded.VType)
	})

	t.Run("empty string returns nil", func(t *testing.T) {
		decoded, err := types.DecodeUniversalPayloadEVM("")
		require.NoError(t, err)
		require.Nil(t, decoded)
	})

	t.Run("0x only returns nil", func(t *testing.T) {
		decoded, err := types.DecodeUniversalPayloadEVM("0x")
		require.NoError(t, err)
		require.Nil(t, decoded)
	})

	t.Run("invalid hex fails", func(t *testing.T) {
		_, err := types.DecodeUniversalPayloadEVM("0xZZZZ")
		require.Error(t, err)
		require.Contains(t, err.Error(), "hex decode")
	})

	t.Run("truncated data fails", func(t *testing.T) {
		_, err := types.DecodeUniversalPayloadEVM("0xdeadbeef")
		require.Error(t, err)
	})

	t.Run("decodes zero-value payload", func(t *testing.T) {
		to := common.HexToAddress("0x0000000000000000000000000000000000000000")
		encoded, err := abiEncodeUniversalPayload(
			to,
			big.NewInt(0),
			[]byte{},
			big.NewInt(0),
			big.NewInt(0),
			big.NewInt(0),
			big.NewInt(0),
			big.NewInt(0),
			0,
		)
		require.NoError(t, err)

		decoded, err := types.DecodeUniversalPayloadEVM(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "0", decoded.Value)
		require.Equal(t, "0x", decoded.Data)
	})
}

func TestDecodeRawPayload(t *testing.T) {
	t.Run("dispatches to EVM for eip155 chain", func(t *testing.T) {
		to := common.HexToAddress("0x000000000000000000000000000000000000beef")
		encoded, err := abiEncodeUniversalPayload(
			to, big.NewInt(100), []byte{}, big.NewInt(21000),
			big.NewInt(1e9), big.NewInt(2e8), big.NewInt(0), big.NewInt(0), 0,
		)
		require.NoError(t, err)

		decoded, err := types.DecodeRawPayload(encoded, "eip155:11155111")
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, to.Hex(), decoded.To)
	})

	t.Run("returns error for unsupported chain namespace", func(t *testing.T) {
		_, err := types.DecodeRawPayload("0xdeadbeef", "solana:devnet")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported chain namespace")
	})
}
