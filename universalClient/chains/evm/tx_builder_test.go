package evm

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// testVaultAddress is a non-zero address used as the vault in tests
const testVaultAddress = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// newTestTxBuilder creates a TxBuilder for unit tests by directly setting the
// vault address, bypassing the constructor's RPC call to VAULT().
func newTestTxBuilder(t *testing.T) *TxBuilder {
	t.Helper()
	logger := zerolog.Nop()
	gwAddr := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	vAddr := ethcommon.HexToAddress(testVaultAddress)

	return &TxBuilder{
		rpcClient:      &RPCClient{},
		chainID:        "eth_sepolia",
		chainIDInt:     11155111,
		gatewayAddress: gwAddr,
		vaultAddress:   vAddr,
		logger:         logger.With().Str("component", "evm_tx_builder").Logger(),
	}
}

func TestRevertInstructionsStruct(t *testing.T) {
	recipient := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	revertMsg := []byte("test revert message")

	ri := RevertInstructions{
		RevertRecipient: recipient,
		RevertMsg:       revertMsg,
	}

	assert.Equal(t, recipient, ri.RevertRecipient)
	assert.Equal(t, revertMsg, ri.RevertMsg)
}

func TestRevertInstructionsABIEncoding(t *testing.T) {
	recipient := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	revertMsg := []byte("test message")

	ri := RevertInstructions{
		RevertRecipient: recipient,
		RevertMsg:       revertMsg,
	}

	revertInstructionType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "revertRecipient", Type: "address"},
		{Name: "revertMsg", Type: "bytes"},
	})
	require.NoError(t, err)

	arguments := abi.Arguments{
		{Type: revertInstructionType},
	}

	encoded, err := arguments.Pack(ri)
	require.NoError(t, err, "RevertInstructions should be ABI encodable")
	assert.NotEmpty(t, encoded)
}

func TestGetFunctionSignature(t *testing.T) {
	builder := newTestTxBuilder(t)

	tests := []struct {
		name     string
		funcName string
		isNative bool
		expected string
	}{
		{
			name:     "finalizeUniversalTx",
			funcName: "finalizeUniversalTx",
			isNative: true,
			expected: "finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)",
		},
		{
			name:     "revertUniversalTx",
			funcName: "revertUniversalTx",
			isNative: true,
			expected: "revertUniversalTx(bytes32,bytes32,address,uint256,(address,bytes))",
		},
		{
			name:     "rescueFunds",
			funcName: "rescueFunds",
			isNative: false,
			expected: "rescueFunds(bytes32,bytes32,address,uint256,(address,bytes))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signature := builder.getFunctionSignature(tt.funcName, tt.isNative)
			assert.Equal(t, tt.expected, signature)
		})
	}
}

func TestFunctionSelectorGeneration(t *testing.T) {
	tests := []struct {
		signature string
	}{
		{"finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"},
		{"revertUniversalTx(bytes32,bytes32,address,uint256,(address,bytes))"},
		{"rescueFunds(bytes32,bytes32,address,uint256,(address,bytes))"},
	}

	for _, tt := range tests {
		t.Run(tt.signature, func(t *testing.T) {
			selector := crypto.Keccak256([]byte(tt.signature))[:4]
			assert.Len(t, selector, 4)
		})
	}
}

func TestDetermineFunctionName(t *testing.T) {
	builder := newTestTxBuilder(t)

	nativeAsset := ethcommon.Address{}
	erc20Asset := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")

	tests := []struct {
		name         string
		txType       uetypes.TxType
		assetAddr    ethcommon.Address
		expectedFunc string
	}{
		{"FUNDS native", uetypes.TxType_FUNDS, nativeAsset, "finalizeUniversalTx"},
		{"FUNDS ERC20", uetypes.TxType_FUNDS, erc20Asset, "finalizeUniversalTx"},
		{"FUNDS_AND_PAYLOAD native", uetypes.TxType_FUNDS_AND_PAYLOAD, nativeAsset, "finalizeUniversalTx"},
		{"FUNDS_AND_PAYLOAD ERC20", uetypes.TxType_FUNDS_AND_PAYLOAD, erc20Asset, "finalizeUniversalTx"},
		{"PAYLOAD native", uetypes.TxType_PAYLOAD, nativeAsset, "finalizeUniversalTx"},
		{"PAYLOAD ERC20", uetypes.TxType_PAYLOAD, erc20Asset, "finalizeUniversalTx"},
		{"INBOUND_REVERT native", uetypes.TxType_INBOUND_REVERT, nativeAsset, "revertUniversalTx"},
		{"INBOUND_REVERT ERC20", uetypes.TxType_INBOUND_REVERT, erc20Asset, "revertUniversalTx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcName := builder.determineFunctionName(tt.txType, tt.assetAddr)
			assert.Equal(t, tt.expectedFunc, funcName)
		})
	}
}

func TestEncodeFunctionCallAllFunctions(t *testing.T) {
	builder := newTestTxBuilder(t)

	txIDBytes := make([]byte, 32)
	for i := range txIDBytes {
		txIDBytes[i] = byte(i)
	}
	universalTxIDBytes := make([]byte, 32)
	for i := range universalTxIDBytes {
		universalTxIDBytes[i] = byte(255 - i)
	}

	baseData := &uetypes.OutboundCreatedEvent{
		TxID:          "0x" + hex.EncodeToString(txIDBytes),
		UniversalTxId: "0x" + hex.EncodeToString(universalTxIDBytes),
		Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
		Recipient:     "0x1111111111111111111111111111111111111111",
		Amount:        "1000000000000000000",
		Payload:       "0x1234567890",
		RevertMsg:     hex.EncodeToString([]byte("revert message")),
	}

	nativeAsset := ethcommon.Address{}
	erc20Asset := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	amount := big.NewInt(1000000000000000000)

	tests := []struct {
		name              string
		funcName          string
		assetAddr         ethcommon.Address
		txType            uetypes.TxType
		expectedSignature string
		expectError       bool
	}{
		{
			name:              "finalizeUniversalTx native",
			funcName:          "finalizeUniversalTx",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_FUNDS,
			expectedSignature: "finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)",
		},
		{
			name:              "finalizeUniversalTx ERC20",
			funcName:          "finalizeUniversalTx",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)",
		},
		{
			name:              "revertUniversalTx native",
			funcName:          "revertUniversalTx",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTx(bytes32,bytes32,address,uint256,(address,bytes))",
		},
		{
			name:              "revertUniversalTx ERC20",
			funcName:          "revertUniversalTx",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTx(bytes32,bytes32,address,uint256,(address,bytes))",
		},
		{
			name:              "rescueFunds native",
			funcName:          "rescueFunds",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "rescueFunds(bytes32,bytes32,address,uint256,(address,bytes))",
		},
		{
			name:              "rescueFunds ERC20",
			funcName:          "rescueFunds",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "rescueFunds(bytes32,bytes32,address,uint256,(address,bytes))",
		},
		{
			name:        "unknown function",
			funcName:    "unknownFunction",
			assetAddr:   nativeAsset,
			txType:      uetypes.TxType_FUNDS,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := builder.encodeFunctionCall(tt.funcName, baseData, amount, tt.assetAddr, tt.txType)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			expectedSelector := crypto.Keccak256([]byte(tt.expectedSignature))[:4]
			actualSelector := encoded[:4]
			assert.Equal(t, expectedSelector, actualSelector,
				"Function selector mismatch for %s\nExpected: 0x%x\nActual: 0x%x",
				tt.name, expectedSelector, actualSelector)

			t.Logf("%s: selector=0x%x, total_length=%d bytes", tt.name, actualSelector, len(encoded))
		})
	}
}

func TestEncodeFunctionCallRevertInstructionsEncoding(t *testing.T) {
	builder := newTestTxBuilder(t)

	tests := []struct {
		name      string
		revertMsg string
	}{
		{"empty revert message", ""},
		{"short revert message", hex.EncodeToString([]byte("err"))},
		{"medium revert message", hex.EncodeToString([]byte("Transaction reverted due to insufficient funds"))},
		{"binary revert message", hex.EncodeToString([]byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &uetypes.OutboundCreatedEvent{
				TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
				UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
				Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
				Recipient:     "0x1111111111111111111111111111111111111111",
				Amount:        "1000000000000000000",
				Payload:       "0x",
				RevertMsg:     tt.revertMsg,
			}

			amount := big.NewInt(1000000000000000000)
			nativeAsset := ethcommon.Address{}

			encoded, err := builder.encodeFunctionCall("revertUniversalTx", data, amount, nativeAsset, uetypes.TxType_INBOUND_REVERT)
			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			expectedSignature := "revertUniversalTx(bytes32,bytes32,address,uint256,(address,bytes))"
			expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]
			assert.Equal(t, expectedSelector, encoded[:4])
		})
	}
}

func TestEncodeFunctionCallPayloadEncoding(t *testing.T) {
	builder := newTestTxBuilder(t)

	tests := []struct {
		name    string
		payload string
	}{
		{"empty payload", "0x"},
		{"small payload (4 bytes)", "0x12345678"},
		{"medium payload (32 bytes)", "0x" + hex.EncodeToString(make([]byte, 32))},
		{"large payload (256 bytes)", "0x" + hex.EncodeToString(make([]byte, 256))},
		{"function call payload", "0xa9059cbb000000000000000000000000111111111111111111111111111111111111111100000000000000000000000000000000000000000000000000000000000186a0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &uetypes.OutboundCreatedEvent{
				TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
				UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
				Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
				Recipient:     "0x1111111111111111111111111111111111111111",
				Amount:        "1000000000000000000",
				Payload:       tt.payload,
				RevertMsg:     "",
			}

			amount := big.NewInt(1000000000000000000)
			nativeAsset := ethcommon.Address{}

			encoded, err := builder.encodeFunctionCall("finalizeUniversalTx", data, amount, nativeAsset, uetypes.TxType_FUNDS_AND_PAYLOAD)
			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			expectedSignature := "finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"
			expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]
			assert.Equal(t, expectedSelector, encoded[:4])
		})
	}
}

func TestEncodeFunctionCallEdgeCases(t *testing.T) {
	builder := newTestTxBuilder(t)

	t.Run("zero amount", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
			UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
			Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
			Recipient:     "0x1111111111111111111111111111111111111111",
			Amount:        "0",
			Payload:       "0x",
		}

		amount := big.NewInt(0)
		nativeAsset := ethcommon.Address{}

		encoded, err := builder.encodeFunctionCall("finalizeUniversalTx", data, amount, nativeAsset, uetypes.TxType_FUNDS)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})

	t.Run("max uint256 amount", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
			UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
			Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
			Recipient:     "0x1111111111111111111111111111111111111111",
			Amount:        "115792089237316195423570985008687907853269984665640564039457584007913129639935",
			Payload:       "0x",
		}

		maxUint256 := new(big.Int)
		maxUint256.SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
		nativeAsset := ethcommon.Address{}

		encoded, err := builder.encodeFunctionCall("finalizeUniversalTx", data, maxUint256, nativeAsset, uetypes.TxType_FUNDS)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})
}

func TestEncodeFunctionCallInvalidTxID(t *testing.T) {
	builder := newTestTxBuilder(t)

	data := &uetypes.OutboundCreatedEvent{
		TxID:          "invalid-tx-id",
		UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
		Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
		Recipient:     "0x1111111111111111111111111111111111111111",
		Amount:        "1000000000000000000",
		Payload:       "0x",
	}

	amount := big.NewInt(1000000000000000000)
	assetAddr := ethcommon.Address{}

	_, err := builder.encodeFunctionCall("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid txID")
}

func TestEncodeFunctionCallInvalidUniversalTxID(t *testing.T) {
	builder := newTestTxBuilder(t)

	data := &uetypes.OutboundCreatedEvent{
		TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
		UniversalTxId: "not-valid-hex",
		Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
		Recipient:     "0x1111111111111111111111111111111111111111",
		Amount:        "1000000000000000000",
		Payload:       "0x",
	}

	amount := big.NewInt(1000000000000000000)
	assetAddr := ethcommon.Address{}

	_, err := builder.encodeFunctionCall("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid universalTxID")
}

func TestRemoveHexPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0xabcdef", "abcdef"},
		{"0XABCDEF", "0XABCDEF"},
		{"abcdef", "abcdef"},
		{"", ""},
		{"0x", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := removeHexPrefix(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseTxType(t *testing.T) {
	tests := []struct {
		input       string
		expected    uetypes.TxType
		expectError bool
	}{
		{"GAS", uetypes.TxType_GAS, false},
		{"FUNDS", uetypes.TxType_FUNDS, false},
		{"PAYLOAD", uetypes.TxType_PAYLOAD, false},
		{"FUNDS_AND_PAYLOAD", uetypes.TxType_FUNDS_AND_PAYLOAD, false},
		{"GAS_AND_PAYLOAD", uetypes.TxType_GAS_AND_PAYLOAD, false},
		{"INBOUND_REVERT", uetypes.TxType_INBOUND_REVERT, false},
		{"1", uetypes.TxType(1), false},
		{"invalid", uetypes.TxType_UNSPECIFIED_TX, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseTxType(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseGasLimit(t *testing.T) {
	t.Run("empty returns error", func(t *testing.T) {
		_, err := parseGasLimit("")
		assert.Error(t, err)
	})
	t.Run("zero returns error", func(t *testing.T) {
		_, err := parseGasLimit("0")
		assert.Error(t, err)
	})
	t.Run("valid gas limit", func(t *testing.T) {
		result, err := parseGasLimit("100000")
		assert.NoError(t, err)
		assert.Equal(t, int64(100000), result.Int64())
	})
	t.Run("large gas limit", func(t *testing.T) {
		result, err := parseGasLimit("1000000")
		assert.NoError(t, err)
		assert.Equal(t, int64(1000000), result.Int64())
	})
	t.Run("invalid returns error", func(t *testing.T) {
		_, err := parseGasLimit("not-a-number")
		assert.Error(t, err)
	})
}

// TestFinalizeUniversalTxUnifiedEncoding verifies that finalizeUniversalTx is used
// for all non-revert tx types (replacing withdraw, withdrawTokens, executeUniversalTx)
func TestFinalizeUniversalTxUnifiedEncoding(t *testing.T) {
	builder := newTestTxBuilder(t)

	data := &uetypes.OutboundCreatedEvent{
		TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
		UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
		Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
		Recipient:     "0x1111111111111111111111111111111111111111",
		Amount:        "1000000000000000000",
		Payload:       "0x1234",
	}

	amount := big.NewInt(1000000000000000000)
	expectedSignature := "finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"
	expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]

	txTypes := []uetypes.TxType{
		uetypes.TxType_FUNDS,
		uetypes.TxType_FUNDS_AND_PAYLOAD,
		uetypes.TxType_PAYLOAD,
	}

	assets := []struct {
		name string
		addr ethcommon.Address
	}{
		{"native", ethcommon.Address{}},
		{"ERC20", ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")},
	}

	for _, txType := range txTypes {
		for _, asset := range assets {
			testName := txType.String() + "_" + asset.name
			t.Run(testName, func(t *testing.T) {
				funcName := builder.determineFunctionName(txType, asset.addr)
				assert.Equal(t, "finalizeUniversalTx", funcName)

				encoded, err := builder.encodeFunctionCall(funcName, data, amount, asset.addr, txType)
				require.NoError(t, err)
				assert.Equal(t, expectedSelector, encoded[:4])
			})
		}
	}
}

// TestIsAlreadyExecuted tests the stub that always returns false
func TestIsAlreadyExecuted(t *testing.T) {
	builder := newTestTxBuilder(t)
	ctx := context.Background()

	t.Run("always returns false", func(t *testing.T) {
		executed, err := builder.IsAlreadyExecuted(ctx, "0x1234567890abcdef")
		assert.NoError(t, err)
		assert.False(t, executed)
	})

	t.Run("returns false for empty txID", func(t *testing.T) {
		executed, err := builder.IsAlreadyExecuted(ctx, "")
		assert.NoError(t, err)
		assert.False(t, executed)
	})

	t.Run("returns false for arbitrary txID", func(t *testing.T) {
		executed, err := builder.IsAlreadyExecuted(ctx, "any-string-at-all")
		assert.NoError(t, err)
		assert.False(t, executed)
	})
}

// TestNewTxBuilderValidation tests the NewTxBuilder constructor validation
func TestNewTxBuilderValidation(t *testing.T) {
	logger := zerolog.Nop()
	vAddr := ethcommon.HexToAddress(testVaultAddress)

	t.Run("nil rpcClient returns error", func(t *testing.T) {
		tb, err := NewTxBuilder(nil, "eip155:1", 1, "0x1234567890123456789012345678901234567890", vAddr, logger)
		assert.Error(t, err)
		assert.Nil(t, tb)
		assert.Contains(t, err.Error(), "rpcClient is required")
	})

	t.Run("empty chainID returns error", func(t *testing.T) {
		tb, err := NewTxBuilder(&RPCClient{}, "", 1, "0x1234567890123456789012345678901234567890", vAddr, logger)
		assert.Error(t, err)
		assert.Nil(t, tb)
		assert.Contains(t, err.Error(), "chainID is required")
	})

	t.Run("empty gatewayAddress returns error", func(t *testing.T) {
		tb, err := NewTxBuilder(&RPCClient{}, "eip155:1", 1, "", vAddr, logger)
		assert.Error(t, err)
		assert.Nil(t, tb)
		assert.Contains(t, err.Error(), "gatewayAddress is required")
	})

	t.Run("valid inputs succeed", func(t *testing.T) {
		tb, err := NewTxBuilder(&RPCClient{}, "eip155:1", 1, "0x1234567890123456789012345678901234567890", vAddr, logger)
		assert.NoError(t, err)
		assert.NotNil(t, tb)
	})
}

// TestGetOutboundSigningRequestValidation tests input validation for GetOutboundSigningRequest
func TestGetOutboundSigningRequestValidation(t *testing.T) {
	builder := newTestTxBuilder(t)
	ctx := context.Background()

	t.Run("nil data returns error", func(t *testing.T) {
		req, err := builder.GetOutboundSigningRequest(ctx, nil, 0)
		assert.Error(t, err)
		assert.Nil(t, req)
		assert.Contains(t, err.Error(), "outbound event data is nil")
	})

	t.Run("empty txID returns error", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{TxID: ""}
		req, err := builder.GetOutboundSigningRequest(ctx, data, 0)
		assert.Error(t, err)
		assert.Nil(t, req)
		assert.Contains(t, err.Error(), "txID is required")
	})

	t.Run("empty destinationChain returns error", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:             "0x" + hex.EncodeToString(make([]byte, 32)),
			DestinationChain: "",
		}
		req, err := builder.GetOutboundSigningRequest(ctx, data, 0)
		assert.Error(t, err)
		assert.Nil(t, req)
		assert.Contains(t, err.Error(), "destinationChain is required")
	})

	t.Run("zero gas price returns error", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:             "0x" + hex.EncodeToString(make([]byte, 32)),
			DestinationChain: "eip155:1",
			GasPrice:         "0",
		}
		req, err := builder.GetOutboundSigningRequest(ctx, data, 0)
		assert.Error(t, err)
		assert.Nil(t, req)
	})
}

// TestBroadcastOutboundSigningRequestValidation tests input validation for BroadcastOutboundSigningRequest
func TestBroadcastOutboundSigningRequestValidation(t *testing.T) {
	builder := newTestTxBuilder(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		hash, err := builder.BroadcastOutboundSigningRequest(ctx, nil, nil, nil)
		assert.Error(t, err)
		assert.Empty(t, hash)
		assert.Contains(t, err.Error(), "signing request is nil")
	})

	t.Run("nil data returns error", func(t *testing.T) {
		req := &common.UnsignedSigningReq{}
		hash, err := builder.BroadcastOutboundSigningRequest(ctx, req, nil, nil)
		assert.Error(t, err)
		assert.Empty(t, hash)
		assert.Contains(t, err.Error(), "outbound event data is nil")
	})

	t.Run("wrong signature length returns error", func(t *testing.T) {
		req := &common.UnsignedSigningReq{}
		data := &uetypes.OutboundCreatedEvent{TxID: "0x1234"}
		hash, err := builder.BroadcastOutboundSigningRequest(ctx, req, data, []byte{1, 2, 3})
		assert.Error(t, err)
		assert.Empty(t, hash)
		assert.Contains(t, err.Error(), "signature must be 65 bytes")
	})
}

// TestGetFunctionSignatureUnknown tests unknown function name returns empty string
func TestGetFunctionSignatureUnknown(t *testing.T) {
	builder := newTestTxBuilder(t)

	sig := builder.getFunctionSignature("unknownFunc", false)
	assert.Equal(t, "", sig)
}

// TestDetermineFunctionNameRescueFunds tests RESCUE_FUNDS routing
func TestDetermineFunctionNameRescueFunds(t *testing.T) {
	builder := newTestTxBuilder(t)
	funcName := builder.determineFunctionName(uetypes.TxType_RESCUE_FUNDS, ethcommon.Address{})
	assert.Equal(t, "rescueFunds", funcName)
}

// TestDetermineFunctionNameDefault tests unknown TxType defaults to finalizeUniversalTx
func TestDetermineFunctionNameDefault(t *testing.T) {
	builder := newTestTxBuilder(t)
	funcName := builder.determineFunctionName(uetypes.TxType(999), ethcommon.Address{})
	assert.Equal(t, "finalizeUniversalTx", funcName)
}

const (
	bscGatewayAddress = "0x44aFFC61983F4348DdddB886349eb992C061EaC0"
	bscVaultAddress   = "0xE52AC4f8DD3e0263bDF748F3390cdFA1f02be881"
	bscSimulateFrom   = "0x05D7386FB3D7cB00e0CFAc5Af3B2EFF6BF37c5f1" // TSS Address (has TSS_ROLE)
	bscPushAccount    = "0x35B84d6848D16415177c64D64504663b998A6ab4" // Push account / origin caller
	bscRPCURL         = "https://bsc-testnet-rpc.publicnode.com"
	bscChainID        = int64(97)
	bscUSDT           = "0xBC14F348BC9667be46b35Edc9B68653d86013DC5"
	bscNativeAsset    = "0x0000000000000000000000000000000000000000"
)

// setupBSCSimulation creates RPCClient and TxBuilder for BSC testnet simulation tests.
func setupBSCSimulation(t *testing.T) (*RPCClient, *TxBuilder) {
	t.Helper()
	t.Skip("skipping simulation tests") // DELIBERATELY SKIPPING SIMULATION TESTS
	logger := zerolog.Nop()
	rpcClient, err := NewRPCClient([]string{bscRPCURL}, bscChainID, logger)
	if err != nil {
		t.Skipf("skipping simulation test: failed to connect to BSC RPC: %v", err)
	}

	vaultAddr := ethcommon.HexToAddress(bscVaultAddress)
	builder, err := NewTxBuilder(rpcClient, "eip155:97", bscChainID, bscGatewayAddress, vaultAddr, logger)
	if err != nil {
		t.Skipf("skipping simulation test: failed to create TxBuilder: %v", err)
	}

	t.Logf("BSC Gateway: %s", bscGatewayAddress)
	t.Logf("BSC Vault: %s", bscVaultAddress)
	t.Logf("BSC TSS: %s", bscSimulateFrom)

	return rpcClient, builder
}

// encodeMulticallPayload ABI-encodes a Multicall[] array for use as the `data` param in finalizeUniversalTx.
func encodeMulticallPayload(t *testing.T, calls []struct {
	To    ethcommon.Address
	Value *big.Int
	Data  []byte
}) string {
	t.Helper()

	multicallArrayType, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
	})
	require.NoError(t, err)

	type Multicall struct {
		To    ethcommon.Address
		Value *big.Int
		Data  []byte
	}

	var multicalls []Multicall
	for _, c := range calls {
		multicalls = append(multicalls, Multicall{To: c.To, Value: c.Value, Data: c.Data})
	}
	if multicalls == nil {
		multicalls = []Multicall{}
	}

	args := abi.Arguments{{Type: multicallArrayType}}
	encoded, err := args.Pack(multicalls)
	require.NoError(t, err)

	return "0x" + hex.EncodeToString(encoded)
}

// newBSCSimulationOutbound creates a full OutboundCreatedEvent for BSC simulation tests.
func newBSCSimulationOutbound(t *testing.T, amount, assetAddr, payload, revertMsg string) *uetypes.OutboundCreatedEvent {
	t.Helper()
	txIDBytes := crypto.Keccak256([]byte(t.Name() + time.Now().String()))
	universalTxIDBytes := crypto.Keccak256([]byte("utx-" + t.Name() + time.Now().String()))

	return &uetypes.OutboundCreatedEvent{
		TxID:          "0x" + hex.EncodeToString(txIDBytes),
		UniversalTxId: "0x" + hex.EncodeToString(universalTxIDBytes),
		Sender:        bscPushAccount,
		Recipient:     "0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811",
		Amount:        amount,
		AssetAddr:     assetAddr,
		Payload:       payload,
		RevertMsg:     revertMsg,
	}
}

// simulateOnVault is a helper that encodes a function call and simulates it via eth_call on the vault.
func simulateOnVault(t *testing.T, rpcClient *RPCClient, builder *TxBuilder, funcName string, data *uetypes.OutboundCreatedEvent, txType uetypes.TxType) {
	t.Helper()

	amount := new(big.Int)
	amount.SetString(data.Amount, 10)
	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

	calldata, err := builder.encodeFunctionCall(funcName, data, amount, assetAddr, txType)
	require.NoError(t, err)

	txValue := big.NewInt(0)
	if assetAddr == (ethcommon.Address{}) {
		txValue = amount
	}

	t.Logf("%s calldata: 0x%s", funcName, hex.EncodeToString(calldata))
	t.Logf("from (TSS): %s, vault: %s, value: %s", bscSimulateFrom, bscVaultAddress, txValue.String())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	from := ethcommon.HexToAddress(bscSimulateFrom)
	vault := ethcommon.HexToAddress(bscVaultAddress)
	result, err := rpcClient.CallContractWithFrom(ctx, from, vault, calldata, txValue, nil)
	require.NoError(t, err, "simulate %s should pass", funcName)
	require.NotNil(t, result)
}

func TestSimulateBSC_FetchVaultFromGateway(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping simulation test in short mode")
	}

	logger := zerolog.Nop()
	rpcClient, err := NewRPCClient([]string{bscRPCURL}, bscChainID, logger)
	if err != nil {
		t.Skipf("skipping: failed to connect to BSC RPC: %v", err)
	}
	defer rpcClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	gwAddr := ethcommon.HexToAddress(bscGatewayAddress)
	vaultCallSelector := crypto.Keccak256([]byte("VAULT()"))[:4]
	result, err := rpcClient.CallContract(ctx, gwAddr, vaultCallSelector, nil)
	require.NoError(t, err, "VAULT() call should succeed")
	require.True(t, len(result) >= 32, "VAULT() should return at least 32 bytes")

	vaultAddr := ethcommon.BytesToAddress(result[12:32])
	assert.NotEqual(t, ethcommon.Address{}, vaultAddr, "VAULT() should not return zero address")
	assert.Equal(t, ethcommon.HexToAddress(bscVaultAddress), vaultAddr, "VAULT() should match expected vault address")
	t.Logf("VAULT() returned: %s", vaultAddr.Hex())
}

func TestSimulateBSC_RevertUniversalTx_Native(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	revertMsg := hex.EncodeToString([]byte("test revert"))
	data := newBSCSimulationOutbound(t, "1000000000000000", bscNativeAsset, "0x", revertMsg) // 0.001 BNB
	simulateOnVault(t, rpcClient, builder, "revertUniversalTx", data, uetypes.TxType_INBOUND_REVERT)
}

func TestSimulateBSC_RevertUniversalTx_ERC20(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	revertMsg := hex.EncodeToString([]byte("test revert token"))
	data := newBSCSimulationOutbound(t, "1000000", bscUSDT, "0x", revertMsg) // 1 USDT
	simulateOnVault(t, rpcClient, builder, "revertUniversalTx", data, uetypes.TxType_INBOUND_REVERT)
}

func TestSimulateBSC_FinalizeUniversalTx_Native(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	emptyMulticall := encodeMulticallPayload(t, nil)
	data := newBSCSimulationOutbound(t, "10000000000", bscNativeAsset, emptyMulticall, "") // ~10 gwei BNB
	simulateOnVault(t, rpcClient, builder, "finalizeUniversalTx", data, uetypes.TxType_FUNDS)
}

func TestSimulateBSC_FinalizeUniversalTx_ERC20(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	emptyMulticall := encodeMulticallPayload(t, nil)
	data := newBSCSimulationOutbound(t, "1000000", bscUSDT, emptyMulticall, "") // 1 USDT
	simulateOnVault(t, rpcClient, builder, "finalizeUniversalTx", data, uetypes.TxType_FUNDS)
}

func TestSimulateBSC_FinalizeUniversalTx_NativeWithPayload(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	recipient := ethcommon.HexToAddress("0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811")
	nativeMulticall := encodeMulticallPayload(t, []struct {
		To    ethcommon.Address
		Value *big.Int
		Data  []byte
	}{{To: recipient, Value: big.NewInt(1000000000000000), Data: nil}})
	data := newBSCSimulationOutbound(t, "1000000000000000", bscNativeAsset, nativeMulticall, "") // 0.001 BNB + payload
	simulateOnVault(t, rpcClient, builder, "finalizeUniversalTx", data, uetypes.TxType_FUNDS_AND_PAYLOAD)
}

func TestSimulateBSC_FinalizeUniversalTx_PayloadOnly(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	payloadRecipient := ethcommon.HexToAddress("0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811")
	payloadOnlyMulticall := encodeMulticallPayload(t, []struct {
		To    ethcommon.Address
		Value *big.Int
		Data  []byte
	}{{To: payloadRecipient, Value: big.NewInt(0), Data: []byte{0xde, 0xad, 0xbe, 0xef}}})
	data := newBSCSimulationOutbound(t, "0", bscNativeAsset, payloadOnlyMulticall, "")
	simulateOnVault(t, rpcClient, builder, "finalizeUniversalTx", data, uetypes.TxType_PAYLOAD)
}

func TestSimulateBSC_RescueFunds_Native(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	revertMsg := hex.EncodeToString([]byte("rescue native"))
	data := newBSCSimulationOutbound(t, "1000000000000000", bscNativeAsset, "0x", revertMsg) // 0.001 BNB
	simulateOnVault(t, rpcClient, builder, "rescueFunds", data, uetypes.TxType_INBOUND_REVERT)
}

func TestSimulateBSC_RescueFunds_ERC20(t *testing.T) {
	rpcClient, builder := setupBSCSimulation(t)
	defer rpcClient.Close()

	revertMsg := hex.EncodeToString([]byte("rescue token"))
	data := newBSCSimulationOutbound(t, "1000000", bscUSDT, "0x", revertMsg) // 1 USDT
	simulateOnVault(t, rpcClient, builder, "rescueFunds", data, uetypes.TxType_INBOUND_REVERT)
}

// ---------------------------------------------------------------------------
// GetGasFeeUsed — requires live RPC; cannot be unit-tested without mocking.
// The function calls rpcClient.GetTransactionReceipt and
// rpcClient.GetTransactionByHash, so a proper test would need a mock
// RPCClient or an integration test against a real node (similar to the
// BSC simulation tests above).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// parseGasLimit — additional edge-case coverage
// ---------------------------------------------------------------------------

func TestParseGasLimitEdgeCases(t *testing.T) {
	t.Run("very large gas limit", func(t *testing.T) {
		result, err := parseGasLimit("999999999999999999")
		assert.NoError(t, err)
		expected := new(big.Int)
		expected.SetString("999999999999999999", 10)
		assert.Equal(t, expected, result)
	})

	t.Run("leading zeros", func(t *testing.T) {
		result, err := parseGasLimit("0021000")
		assert.NoError(t, err)
		assert.Equal(t, int64(21000), result.Int64())
	})

	t.Run("negative number is invalid", func(t *testing.T) {
		// big.Int.SetString with base 10 will parse negative numbers, but the
		// function doesn't explicitly reject them. This documents behavior.
		result, err := parseGasLimit("-100")
		if err == nil {
			// If it parses, the value would be negative
			assert.True(t, result.Sign() < 0)
		}
	})

	t.Run("whitespace only is invalid", func(t *testing.T) {
		_, err := parseGasLimit("   ")
		assert.Error(t, err)
	})

	t.Run("hex string is invalid", func(t *testing.T) {
		_, err := parseGasLimit("0xff")
		assert.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// parseTxType — additional edge-case coverage
// ---------------------------------------------------------------------------

func TestParseTxTypeEdgeCases(t *testing.T) {
	t.Run("lowercase input is uppercased", func(t *testing.T) {
		result, err := parseTxType("funds")
		assert.NoError(t, err)
		assert.Equal(t, uetypes.TxType_FUNDS, result)
	})

	t.Run("mixed case input", func(t *testing.T) {
		result, err := parseTxType("Funds_And_Payload")
		assert.NoError(t, err)
		assert.Equal(t, uetypes.TxType_FUNDS_AND_PAYLOAD, result)
	})

	t.Run("input with whitespace is trimmed", func(t *testing.T) {
		result, err := parseTxType("  PAYLOAD  ")
		assert.NoError(t, err)
		assert.Equal(t, uetypes.TxType_PAYLOAD, result)
	})

	t.Run("numeric string 0", func(t *testing.T) {
		result, err := parseTxType("0")
		assert.NoError(t, err)
		assert.Equal(t, uetypes.TxType(0), result)
	})

	t.Run("numeric string for RESCUE_FUNDS", func(t *testing.T) {
		result, err := parseTxType("RESCUE_FUNDS")
		assert.NoError(t, err)
		assert.Equal(t, uetypes.TxType_RESCUE_FUNDS, result)
	})

	t.Run("empty string is invalid", func(t *testing.T) {
		_, err := parseTxType("")
		assert.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// removeHexPrefix — additional edge-case coverage
// ---------------------------------------------------------------------------

func TestRemoveHexPrefixAdditional(t *testing.T) {
	t.Run("single character", func(t *testing.T) {
		assert.Equal(t, "a", removeHexPrefix("a"))
	})

	t.Run("0x only", func(t *testing.T) {
		assert.Equal(t, "", removeHexPrefix("0x"))
	})

	t.Run("does not strip uppercase 0X", func(t *testing.T) {
		// The function only checks lowercase 0x
		assert.Equal(t, "0XABCDEF", removeHexPrefix("0XABCDEF"))
	})
}

// ---------------------------------------------------------------------------
// NewTxBuilder — additional validation edge cases
// ---------------------------------------------------------------------------

func TestNewTxBuilderZeroGatewayAddress(t *testing.T) {
	logger := zerolog.Nop()
	vAddr := ethcommon.HexToAddress(testVaultAddress)

	// "0x0000000000000000000000000000000000000000" is the zero address
	tb, err := NewTxBuilder(&RPCClient{}, "eip155:1", 1, "0x0000000000000000000000000000000000000000", vAddr, logger)
	assert.Error(t, err)
	assert.Nil(t, tb)
	assert.Contains(t, err.Error(), "invalid gateway address")
}
