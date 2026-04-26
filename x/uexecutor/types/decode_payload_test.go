package types_test

import (
	"encoding/binary"
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

	t.Run("dispatches to Solana for solana chain", func(t *testing.T) {
		encoded := borshEncodeUniversalPayload(t,
			[20]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xbe, 0xef},
			500, []byte{0xca, 0xfe}, 21000, 1000000000, 200000000, 1, 9999999999, 1,
		)
		decoded, err := types.DecodeRawPayload(encoded, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "0x000000000000000000000000000000000000beef", decoded.To)
		require.Equal(t, "500", decoded.Value)
		require.Equal(t, "0xcafe", decoded.Data)
	})

	t.Run("returns error for unsupported chain namespace", func(t *testing.T) {
		_, err := types.DecodeRawPayload("0xdeadbeef", "cosmos:cosmoshub-4")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported chain namespace")
	})
}

// borshEncodeUniversalPayload encodes a UniversalPayload into Borsh format (Rust/Anchor layout).
func borshEncodeUniversalPayload(
	t *testing.T,
	to [20]byte,
	value uint64,
	data []byte,
	gasLimit uint64,
	maxFeePerGas uint64,
	maxPriorityFeePerGas uint64,
	nonce uint64,
	deadline int64,
	vType uint8,
) string {
	t.Helper()
	buf := make([]byte, 0, 20+8+4+len(data)+8*5+1)

	// to: [u8; 20]
	buf = append(buf, to[:]...)

	// value: u64 LE
	tmp := make([]byte, 8)
	binary.LittleEndian.PutUint64(tmp, value)
	buf = append(buf, tmp...)

	// data: Vec<u8> = 4-byte LE length + bytes
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(data)))
	buf = append(buf, lenBuf...)
	buf = append(buf, data...)

	// gasLimit: u64 LE
	binary.LittleEndian.PutUint64(tmp, gasLimit)
	buf = append(buf, tmp...)

	// maxFeePerGas: u64 LE
	binary.LittleEndian.PutUint64(tmp, maxFeePerGas)
	buf = append(buf, tmp...)

	// maxPriorityFeePerGas: u64 LE
	binary.LittleEndian.PutUint64(tmp, maxPriorityFeePerGas)
	buf = append(buf, tmp...)

	// nonce: u64 LE
	binary.LittleEndian.PutUint64(tmp, nonce)
	buf = append(buf, tmp...)

	// deadline: i64 LE
	binary.LittleEndian.PutUint64(tmp, uint64(deadline))
	buf = append(buf, tmp...)

	// vType: u8
	buf = append(buf, vType)

	return "0x" + hex.EncodeToString(buf)
}

func TestDecodeUniversalPayloadSolana(t *testing.T) {
	t.Run("decodes valid Borsh-encoded payload", func(t *testing.T) {
		to := [20]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xbe, 0xef}
		encoded := borshEncodeUniversalPayload(t,
			to, 1000, []byte{0xde, 0xad, 0xbe, 0xef},
			21000, 1000000000, 200000000, 1, 9999999999, 1,
		)

		decoded, err := types.DecodeUniversalPayloadSolana(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "0x000000000000000000000000000000000000beef", decoded.To)
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
		decoded, err := types.DecodeUniversalPayloadSolana("")
		require.NoError(t, err)
		require.Nil(t, decoded)
	})

	t.Run("0x only returns nil", func(t *testing.T) {
		decoded, err := types.DecodeUniversalPayloadSolana("0x")
		require.NoError(t, err)
		require.Nil(t, decoded)
	})

	t.Run("invalid hex fails", func(t *testing.T) {
		_, err := types.DecodeUniversalPayloadSolana("0xZZZZ")
		require.Error(t, err)
		require.Contains(t, err.Error(), "hex decode")
	})

	t.Run("truncated data fails", func(t *testing.T) {
		_, err := types.DecodeUniversalPayloadSolana("0xdeadbeef")
		require.Error(t, err)
		require.Contains(t, err.Error(), "insufficient data length")
	})

	t.Run("decodes zero-value payload", func(t *testing.T) {
		encoded := borshEncodeUniversalPayload(t,
			[20]byte{}, 0, []byte{}, 0, 0, 0, 0, 0, 0,
		)
		decoded, err := types.DecodeUniversalPayloadSolana(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "0", decoded.Value)
		require.Equal(t, "0x", decoded.Data)
		require.Equal(t, "0", decoded.GasLimit)
	})

	t.Run("decodes payload with large data field", func(t *testing.T) {
		bigData := make([]byte, 1024)
		for i := range bigData {
			bigData[i] = byte(i % 256)
		}
		encoded := borshEncodeUniversalPayload(t,
			[20]byte{0xff}, 42, bigData, 100, 200, 300, 4, 5, 2,
		)
		decoded, err := types.DecodeUniversalPayloadSolana(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "42", decoded.Value)
		require.Equal(t, "0x"+hex.EncodeToString(bigData), decoded.Data)
	})

	t.Run("negative deadline (i64)", func(t *testing.T) {
		encoded := borshEncodeUniversalPayload(t,
			[20]byte{}, 0, []byte{}, 0, 0, 0, 0, -100, 0,
		)
		decoded, err := types.DecodeUniversalPayloadSolana(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "-100", decoded.Deadline)
	})

	t.Run("truncated after data vec fails", func(t *testing.T) {
		// Build payload that passes minSize (73) but has a large data vec that causes
		// the trailing fixed fields to be truncated.
		// Layout: 20 (to) + 8 (value) + 4 (data len) + 50 (data) + need 41 more bytes for trailing.
		// Total needed = 20 + 8 + 4 + 50 + 41 = 123.
		// We provide only 20 + 8 + 4 + 50 + 8 = 90 bytes (only gasLimit, missing rest).
		buf := make([]byte, 0, 90)
		buf = append(buf, make([]byte, 20)...)                    // to
		tmp := make([]byte, 8)
		binary.LittleEndian.PutUint64(tmp, 0)
		buf = append(buf, tmp...)                                 // value
		lenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBuf, 50)
		buf = append(buf, lenBuf...)                              // data len = 50
		buf = append(buf, make([]byte, 50)...)                    // data (50 bytes)
		buf = append(buf, make([]byte, 8)...)                     // gasLimit only — missing 4 more u64s + u8

		_, err := types.DecodeUniversalPayloadSolana("0x" + hex.EncodeToString(buf))
		require.Error(t, err)
		require.Contains(t, err.Error(), "truncated after data")
	})

	t.Run("realistic solana inbound payload with EVM call data", func(t *testing.T) {
		// Simulates what the SVM UV sends: a UniversalPayload targeting a Push Chain
		// contract with ERC20 approve calldata.
		to := [20]byte{}
		// Target contract: 0x387b9C8Db60E74999aAAC5A2b7825b400F12d68E
		toBytes, _ := hex.DecodeString("387b9C8Db60E74999aAAC5A2b7825b400F12d68E")
		copy(to[:], toBytes)

		// approve(address,uint256) calldata
		calldata, _ := hex.DecodeString(
			"095ea7b3" +
				"000000000000000000000000000000000000000000000000000000000000beef" +
				"0000000000000000000000000000000000000000000000000000000000001000",
		)

		encoded := borshEncodeUniversalPayload(t,
			to,
			0,        // value: 0 for approve
			calldata, // EVM calldata
			100000,   // gasLimit
			25000000000, // maxFeePerGas (25 gwei)
			1000000000,  // maxPriorityFeePerGas (1 gwei)
			42,          // nonce
			1717027200,  // deadline (unix timestamp)
			1,           // vType: signed verification
		)

		decoded, err := types.DecodeUniversalPayloadSolana(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "0x387b9c8db60e74999aaac5a2b7825b400f12d68e", decoded.To)
		require.Equal(t, "0", decoded.Value)
		require.Equal(t, "0x"+hex.EncodeToString(calldata), decoded.Data)
		require.Equal(t, "100000", decoded.GasLimit)
		require.Equal(t, "25000000000", decoded.MaxFeePerGas)
		require.Equal(t, "1000000000", decoded.MaxPriorityFeePerGas)
		require.Equal(t, "42", decoded.Nonce)
		require.Equal(t, "1717027200", decoded.Deadline)
		require.Equal(t, types.VerificationType(1), decoded.VType)
	})

	t.Run("realistic solana inbound: funds only (empty data)", func(t *testing.T) {
		// Pure fund transfer with no calldata — data vec is empty
		to := [20]byte{}
		toBytes, _ := hex.DecodeString("90F4A15601E08570D6fFbaE883C44BDB85bDb7d1")
		copy(to[:], toBytes)

		encoded := borshEncodeUniversalPayload(t,
			to,
			1000000000000000000, // 1 ETH in wei (u64 max ~18.4 ETH)
			[]byte{},            // no calldata
			21000,               // standard gas limit
			20000000000,         // 20 gwei
			2000000000,          // 2 gwei
			0,                   // nonce
			0,                   // no deadline
			0,                   // vType: none
		)

		decoded, err := types.DecodeUniversalPayloadSolana(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "0x90f4a15601e08570d6ffbae883c44bdb85bdb7d1", decoded.To)
		require.Equal(t, "1000000000000000000", decoded.Value)
		require.Equal(t, "0x", decoded.Data)
		require.Equal(t, "21000", decoded.GasLimit)
		require.Equal(t, types.VerificationType(0), decoded.VType)
	})

	t.Run("max u64 values", func(t *testing.T) {
		encoded := borshEncodeUniversalPayload(t,
			[20]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			^uint64(0),   // max u64
			[]byte{0x01}, // 1-byte data
			^uint64(0),
			^uint64(0),
			^uint64(0),
			^uint64(0),
			9223372036854775807, // max i64
			255,                 // max u8
		)

		decoded, err := types.DecodeUniversalPayloadSolana(encoded)
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "0xffffffffffffffffffffffffffffffffffffffff", decoded.To)
		require.Equal(t, "18446744073709551615", decoded.Value)
		require.Equal(t, "0x01", decoded.Data)
		require.Equal(t, "18446744073709551615", decoded.GasLimit)
		require.Equal(t, "9223372036854775807", decoded.Deadline)
		require.Equal(t, types.VerificationType(255), decoded.VType)
	})

	t.Run("DecodeRawPayload dispatches solana devnet chain ID", func(t *testing.T) {
		// Use real Solana devnet CAIP-2 ID from config
		to := [20]byte{0xab}
		encoded := borshEncodeUniversalPayload(t,
			to, 100, []byte{}, 21000, 0, 0, 0, 0, 0,
		)

		decoded, err := types.DecodeRawPayload(encoded, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1")
		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, "100", decoded.Value)
	})

	t.Run("data vec with declared length exceeding actual bytes", func(t *testing.T) {
		// data length says 1000 bytes but only 5 are present
		buf := make([]byte, 0, 80)
		buf = append(buf, make([]byte, 20)...) // to
		tmp := make([]byte, 8)
		binary.LittleEndian.PutUint64(tmp, 0)
		buf = append(buf, tmp...) // value
		lenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBuf, 1000) // claims 1000 bytes
		buf = append(buf, lenBuf...)
		buf = append(buf, make([]byte, 5)...) // only 5 bytes of data
		// pad to pass minSize
		buf = append(buf, make([]byte, 50)...)

		_, err := types.DecodeUniversalPayloadSolana("0x" + hex.EncodeToString(buf))
		require.Error(t, err)
		require.Contains(t, err.Error(), "data length 1000 exceeds remaining bytes")
	})
}
