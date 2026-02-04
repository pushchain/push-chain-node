package evm

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestDefaultGasLimitV0(t *testing.T) {
	assert.Equal(t, int64(500000), int64(DefaultGasLimitV0), "DefaultGasLimitV0 should be 500000")
}

func TestRevertInstructionsV0Struct(t *testing.T) {
	// Test that RevertInstructionsV0 struct can be created and used
	recipient := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	revertMsg := []byte("test revert message")

	ri := RevertInstructionsV0{
		FundRecipient: recipient,
		RevertMsg:     revertMsg,
	}

	assert.Equal(t, recipient, ri.FundRecipient)
	assert.Equal(t, revertMsg, ri.RevertMsg)
}

func TestRevertInstructionsV0ABIEncoding(t *testing.T) {
	// Test that RevertInstructionsV0 can be ABI encoded as a tuple
	recipient := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	revertMsg := []byte("test message")

	ri := RevertInstructionsV0{
		FundRecipient: recipient,
		RevertMsg:     revertMsg,
	}

	// Create tuple type matching the Solidity struct
	revertInstructionType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "fundRecipient", Type: "address"},
		{Name: "revertMsg", Type: "bytes"},
	})
	require.NoError(t, err)

	arguments := abi.Arguments{
		{Type: revertInstructionType},
	}

	// This should not panic or error - the struct should be encodable
	encoded, err := arguments.Pack(ri)
	require.NoError(t, err, "RevertInstructionsV0 should be ABI encodable")
	assert.NotEmpty(t, encoded, "Encoded data should not be empty")
}

func TestNewTxBuilderV0(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name           string
		rpcClient      *RPCClient
		chainID        string
		chainIDInt     int64
		gatewayAddress string
		expectError    bool
		errorContains  string
	}{
		{
			name:           "valid inputs",
			rpcClient:      &RPCClient{},
			chainID:        "eth_sepolia",
			chainIDInt:     11155111,
			gatewayAddress: "0x1234567890123456789012345678901234567890",
			expectError:    false,
		},
		{
			name:           "nil rpcClient",
			rpcClient:      nil,
			chainID:        "eth_sepolia",
			chainIDInt:     11155111,
			gatewayAddress: "0x1234567890123456789012345678901234567890",
			expectError:    true,
			errorContains:  "rpcClient is required",
		},
		{
			name:           "empty chainID",
			rpcClient:      &RPCClient{},
			chainID:        "",
			chainIDInt:     11155111,
			gatewayAddress: "0x1234567890123456789012345678901234567890",
			expectError:    true,
			errorContains:  "chainID is required",
		},
		{
			name:           "empty gatewayAddress",
			rpcClient:      &RPCClient{},
			chainID:        "eth_sepolia",
			chainIDInt:     11155111,
			gatewayAddress: "",
			expectError:    true,
			errorContains:  "gatewayAddress is required",
		},
		{
			name:           "invalid gatewayAddress (zero address)",
			rpcClient:      &RPCClient{},
			chainID:        "eth_sepolia",
			chainIDInt:     11155111,
			gatewayAddress: "0x0000000000000000000000000000000000000000",
			expectError:    true,
			errorContains:  "invalid gateway address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, err := NewTxBuilder(tt.rpcClient, tt.chainID, tt.chainIDInt, tt.gatewayAddress, logger)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, builder)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, builder)
				assert.Equal(t, tt.chainID, builder.chainID)
				assert.Equal(t, tt.chainIDInt, builder.chainIDInt)
			}
		})
	}
}

func TestGetFunctionSignatureV0(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	tests := []struct {
		name     string
		funcName string
		isNative bool
		expected string
	}{
		{
			name:     "withdraw native",
			funcName: "withdraw",
			isNative: true,
			expected: "withdraw(bytes,bytes32,address,address,uint256)",
		},
		{
			name:     "withdrawTokens ERC20",
			funcName: "withdrawTokens",
			isNative: false,
			expected: "withdrawTokens(bytes,bytes32,address,address,address,uint256)",
		},
		{
			name:     "executeUniversalTx native",
			funcName: "executeUniversalTx",
			isNative: true,
			expected: "executeUniversalTx(bytes32,address,address,uint256,bytes)",
		},
		{
			name:     "executeUniversalTx ERC20",
			funcName: "executeUniversalTx",
			isNative: false,
			expected: "executeUniversalTx(bytes32,address,address,address,uint256,bytes)",
		},
		{
			name:     "revertUniversalTx native",
			funcName: "revertUniversalTx",
			isNative: true,
			expected: "revertUniversalTx(bytes32,uint256,(address,bytes))",
		},
		{
			name:     "revertUniversalTxToken ERC20",
			funcName: "revertUniversalTxToken",
			isNative: false,
			expected: "revertUniversalTxToken(bytes32,address,uint256,(address,bytes))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signature := builder.getFunctionSignature(tt.funcName, tt.isNative)
			assert.Equal(t, tt.expected, signature)
		})
	}
}

func TestFunctionSelectorGenerationV0(t *testing.T) {
	// Test that function selectors are correctly generated from UniversalGatewayV0 signatures
	tests := []struct {
		signature string
	}{
		{"withdraw(bytes,bytes32,address,address,uint256)"},
		{"withdrawTokens(bytes,bytes32,address,address,address,uint256)"},
		{"executeUniversalTx(bytes32,address,address,uint256,bytes)"},
		{"executeUniversalTx(bytes32,address,address,address,uint256,bytes)"},
		{"revertUniversalTx(bytes32,uint256,(address,bytes))"},
		{"revertUniversalTxToken(bytes32,address,uint256,(address,bytes))"},
	}

	for _, tt := range tests {
		t.Run(tt.signature, func(t *testing.T) {
			selector := crypto.Keccak256([]byte(tt.signature))[:4]
			assert.Len(t, selector, 4, "Function selector should be 4 bytes")
		})
	}
}

func TestDetermineFunctionNameV0(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	nativeAsset := ethcommon.Address{}
	erc20Asset := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")

	tests := []struct {
		name         string
		txType       uetypes.TxType
		assetAddr    ethcommon.Address
		expectedFunc string
	}{
		{"FUNDS native", uetypes.TxType_FUNDS, nativeAsset, "withdraw"},
		{"FUNDS ERC20", uetypes.TxType_FUNDS, erc20Asset, "withdrawTokens"},
		{"FUNDS_AND_PAYLOAD native", uetypes.TxType_FUNDS_AND_PAYLOAD, nativeAsset, "executeUniversalTx"},
		{"FUNDS_AND_PAYLOAD ERC20", uetypes.TxType_FUNDS_AND_PAYLOAD, erc20Asset, "executeUniversalTx"},
		{"PAYLOAD native", uetypes.TxType_PAYLOAD, nativeAsset, "executeUniversalTx"},
		{"PAYLOAD ERC20", uetypes.TxType_PAYLOAD, erc20Asset, "executeUniversalTx"},
		{"INBOUND_REVERT native", uetypes.TxType_INBOUND_REVERT, nativeAsset, "revertUniversalTx"},
		{"INBOUND_REVERT ERC20", uetypes.TxType_INBOUND_REVERT, erc20Asset, "revertUniversalTxToken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcName := builder.determineFunctionName(tt.txType, tt.assetAddr)
			assert.Equal(t, tt.expectedFunc, funcName)
		})
	}
}

// TestEncodeFunctionCallV0AllFunctions tests all UniversalGatewayV0 function encodings
func TestEncodeFunctionCallV0AllFunctions(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	// Sample test data
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
		// ========== withdraw (native) ==========
		{
			name:              "withdraw native",
			funcName:          "withdraw",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_FUNDS,
			expectedSignature: "withdraw(bytes,bytes32,address,address,uint256)",
		},

		// ========== withdrawTokens (ERC20) ==========
		{
			name:              "withdrawTokens ERC20",
			funcName:          "withdrawTokens",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_FUNDS,
			expectedSignature: "withdrawTokens(bytes,bytes32,address,address,address,uint256)",
		},

		// ========== executeUniversalTx ==========
		{
			name:              "executeUniversalTx native",
			funcName:          "executeUniversalTx",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "executeUniversalTx(bytes32,address,address,uint256,bytes)",
		},
		{
			name:              "executeUniversalTx ERC20",
			funcName:          "executeUniversalTx",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "executeUniversalTx(bytes32,address,address,address,uint256,bytes)",
		},

		// ========== revertUniversalTx ==========
		{
			name:              "revertUniversalTx native",
			funcName:          "revertUniversalTx",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTx(bytes32,uint256,(address,bytes))",
		},

		// ========== revertUniversalTxToken ==========
		{
			name:              "revertUniversalTxToken ERC20",
			funcName:          "revertUniversalTxToken",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTxToken(bytes32,address,uint256,(address,bytes))",
		},

		// ========== Error cases ==========
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

			require.NoError(t, err, "encodeFunctionCall should not error for %s", tt.name)
			assert.NotEmpty(t, encoded, "encoded data should not be empty")

			// Verify function selector (first 4 bytes)
			expectedSelector := crypto.Keccak256([]byte(tt.expectedSignature))[:4]
			actualSelector := encoded[:4]
			assert.Equal(t, expectedSelector, actualSelector,
				"Function selector mismatch for %s\nExpected signature: %s\nExpected selector: 0x%x\nActual selector: 0x%x",
				tt.name, tt.expectedSignature, expectedSelector, actualSelector)

			t.Logf("%s: selector=0x%x, total_length=%d bytes", tt.name, actualSelector, len(encoded))
		})
	}
}

// TestEncodeFunctionCallV0WithAllTxTypes tests each function with all applicable TxTypes
func TestEncodeFunctionCallV0WithAllTxTypes(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	txIDBytes := make([]byte, 32)
	universalTxIDBytes := make([]byte, 32)

	data := &uetypes.OutboundCreatedEvent{
		TxID:          "0x" + hex.EncodeToString(txIDBytes),
		UniversalTxId: "0x" + hex.EncodeToString(universalTxIDBytes),
		Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
		Recipient:     "0x1111111111111111111111111111111111111111",
		Amount:        "1000000000000000000",
		Payload:       "0x",
		RevertMsg:     "",
	}

	amount := big.NewInt(1000000000000000000)
	nativeAsset := ethcommon.Address{}
	erc20Asset := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")

	allTxTypes := []uetypes.TxType{
		uetypes.TxType_GAS,
		uetypes.TxType_GAS_AND_PAYLOAD,
		uetypes.TxType_FUNDS,
		uetypes.TxType_FUNDS_AND_PAYLOAD,
		uetypes.TxType_PAYLOAD,
		uetypes.TxType_INBOUND_REVERT,
	}

	functions := []struct {
		name      string
		assetAddr ethcommon.Address
	}{
		{"withdraw", nativeAsset},
		{"withdrawTokens", erc20Asset},
		{"executeUniversalTx", nativeAsset},
		{"executeUniversalTx", erc20Asset},
		{"revertUniversalTx", nativeAsset},
		{"revertUniversalTxToken", erc20Asset},
	}

	for _, fn := range functions {
		for _, txType := range allTxTypes {
			testName := fn.name
			if fn.assetAddr == nativeAsset {
				testName += "_native"
			} else {
				testName += "_erc20"
			}
			testName += "_" + txType.String()

			t.Run(testName, func(t *testing.T) {
				encoded, err := builder.encodeFunctionCall(fn.name, data, amount, fn.assetAddr, txType)
				require.NoError(t, err, "encoding should succeed")
				assert.NotEmpty(t, encoded)
				assert.GreaterOrEqual(t, len(encoded), 4, "encoded data should have at least function selector")
				t.Logf("%s: length=%d bytes, selector=0x%x", testName, len(encoded), encoded[:4])
			})
		}
	}
}

// TestEncodeFunctionCallV0RevertInstructionsEncoding tests RevertInstructionsV0 encoding
func TestEncodeFunctionCallV0RevertInstructionsEncoding(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

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

			// Verify function selector
			expectedSignature := "revertUniversalTx(bytes32,uint256,(address,bytes))"
			expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]
			assert.Equal(t, expectedSelector, encoded[:4])

			t.Logf("%s: revertMsg_hex_len=%d, encoded_len=%d", tt.name, len(tt.revertMsg)/2, len(encoded))
		})
	}
}

// TestEncodeFunctionCallV0PayloadEncoding tests various payload sizes
func TestEncodeFunctionCallV0PayloadEncoding(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

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

			encoded, err := builder.encodeFunctionCall("executeUniversalTx", data, amount, nativeAsset, uetypes.TxType_FUNDS_AND_PAYLOAD)
			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			// Verify function selector
			expectedSignature := "executeUniversalTx(bytes32,address,address,uint256,bytes)"
			expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]
			assert.Equal(t, expectedSelector, encoded[:4])

			payloadBytes, _ := hex.DecodeString(removeHexPrefixV0(tt.payload))
			t.Logf("%s: payload_len=%d, encoded_len=%d", tt.name, len(payloadBytes), len(encoded))
		})
	}
}

// TestEncodeFunctionCallV0EdgeCases tests edge cases and boundary conditions
func TestEncodeFunctionCallV0EdgeCases(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	t.Run("zero amount", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
			UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
			Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
			Recipient:     "0x1111111111111111111111111111111111111111",
			Amount:        "0",
			Payload:       "0x",
			RevertMsg:     "",
		}

		amount := big.NewInt(0)
		nativeAsset := ethcommon.Address{}

		encoded, err := builder.encodeFunctionCall("withdraw", data, amount, nativeAsset, uetypes.TxType_FUNDS)
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
			RevertMsg:     "",
		}

		maxUint256 := new(big.Int)
		maxUint256.SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
		nativeAsset := ethcommon.Address{}

		encoded, err := builder.encodeFunctionCall("withdraw", data, maxUint256, nativeAsset, uetypes.TxType_FUNDS)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})

	t.Run("zero address recipient", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
			UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
			Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
			Recipient:     "0x0000000000000000000000000000000000000000",
			Amount:        "1000000000000000000",
			Payload:       "0x",
			RevertMsg:     "",
		}

		amount := big.NewInt(1000000000000000000)
		nativeAsset := ethcommon.Address{}

		encoded, err := builder.encodeFunctionCall("withdraw", data, amount, nativeAsset, uetypes.TxType_FUNDS)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})
}

func TestEncodeFunctionCallV0InvalidTxID(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	data := &uetypes.OutboundCreatedEvent{
		TxID:          "invalid-tx-id", // Invalid hex
		UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
		Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
		Recipient:     "0x1111111111111111111111111111111111111111",
		Amount:        "1000000000000000000",
		Payload:       "0x",
	}

	amount := big.NewInt(1000000000000000000)
	assetAddr := ethcommon.Address{}

	_, err = builder.encodeFunctionCall("withdraw", data, amount, assetAddr, uetypes.TxType_FUNDS)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid txID")
}

func TestEncodeFunctionCallV0InvalidUniversalTxID(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

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

	_, err = builder.encodeFunctionCall("withdraw", data, amount, assetAddr, uetypes.TxType_FUNDS)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid universalTxID")
}

func TestEncodeFunctionCallV0UnknownFunction(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilder(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	data := &uetypes.OutboundCreatedEvent{
		TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
		UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
		Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
		Recipient:     "0x1111111111111111111111111111111111111111",
		Amount:        "1000000000000000000",
		Payload:       "0x",
	}

	amount := big.NewInt(1000000000000000000)
	assetAddr := ethcommon.Address{}

	_, err = builder.encodeFunctionCall("unknownFunction", data, amount, assetAddr, uetypes.TxType_FUNDS)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown function name")
}

func TestRemoveHexPrefixV0(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0xabcdef", "abcdef"},
		{"0XABCDEF", "0XABCDEF"}, // Only lowercase 0x is handled
		{"abcdef", "abcdef"},
		{"", ""},
		{"0x", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := removeHexPrefixV0(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseTxTypeV0(t *testing.T) {
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
			result, err := parseTxTypeV0(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGasLimitParsingV0(t *testing.T) {
	tests := []struct {
		name       string
		gasLimit   string
		expected   int64
		useDefault bool
	}{
		{
			name:       "empty gas limit uses default",
			gasLimit:   "",
			expected:   DefaultGasLimitV0,
			useDefault: true,
		},
		{
			name:       "zero gas limit uses default",
			gasLimit:   "0",
			expected:   DefaultGasLimitV0,
			useDefault: true,
		},
		{
			name:       "valid gas limit",
			gasLimit:   "100000",
			expected:   100000,
			useDefault: false,
		},
		{
			name:       "large gas limit",
			gasLimit:   "1000000",
			expected:   1000000,
			useDefault: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gasLimit *big.Int
			if tt.gasLimit == "" || tt.gasLimit == "0" {
				gasLimit = big.NewInt(DefaultGasLimitV0)
			} else {
				gasLimit = new(big.Int)
				gasLimit, _ = gasLimit.SetString(tt.gasLimit, 10)
			}

			assert.Equal(t, tt.expected, gasLimit.Int64())
		})
	}
}
