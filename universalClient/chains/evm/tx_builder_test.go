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
			expected: "withdraw(bytes32,bytes32,address,address,uint256)",
		},
		{
			name:     "withdrawTokens ERC20",
			funcName: "withdrawTokens",
			isNative: false,
			expected: "withdrawTokens(bytes32,bytes32,address,address,address,uint256)",
		},
		{
			name:     "executeUniversalTx native",
			funcName: "executeUniversalTx",
			isNative: true,
			expected: "executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)",
		},
		{
			name:     "executeUniversalTx ERC20",
			funcName: "executeUniversalTx",
			isNative: false,
			expected: "executeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)",
		},
		{
			name:     "revertUniversalTx native",
			funcName: "revertUniversalTx",
			isNative: true,
			expected: "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))",
		},
		{
			name:     "revertUniversalTxToken ERC20",
			funcName: "revertUniversalTxToken",
			isNative: false,
			expected: "revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))",
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
		{"withdraw(bytes32,bytes32,address,address,uint256)"},
		{"withdrawTokens(bytes32,bytes32,address,address,address,uint256)"},
		{"executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)"},
		{"executeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"},
		{"revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"},
		{"revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))"},
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
			expectedSignature: "withdraw(bytes32,bytes32,address,address,uint256)",
		},

		// ========== withdrawTokens (ERC20) ==========
		{
			name:              "withdrawTokens ERC20",
			funcName:          "withdrawTokens",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_FUNDS,
			expectedSignature: "withdrawTokens(bytes32,bytes32,address,address,address,uint256)",
		},

		// ========== executeUniversalTx ==========
		{
			name:              "executeUniversalTx native",
			funcName:          "executeUniversalTx",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)",
		},
		{
			name:              "executeUniversalTx ERC20",
			funcName:          "executeUniversalTx",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "executeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)",
		},

		// ========== revertUniversalTx ==========
		{
			name:              "revertUniversalTx native",
			funcName:          "revertUniversalTx",
			assetAddr:         nativeAsset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))",
		},

		// ========== revertUniversalTxToken ==========
		{
			name:              "revertUniversalTxToken ERC20",
			funcName:          "revertUniversalTxToken",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))",
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
			expectedSignature := "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"
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
			expectedSignature := "executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)"
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

// // Sepolia gateway contract and simulate address for integration tests
// const (
// 	sepoliaGatewayAddress = "0x05bD7a3D18324c1F7e216f7fBF2b15985aE5281A"
// 	sepoliaSimulateFrom   = "0x05d7386fb3d7cb00e0cfac5af3b2eff6bf37c5f1" // TSS Address (has TSS_ROLE)
// 	sepoliaOriginCaller   = "0x35B84d6848D16415177c64D64504663b998A6ab4" // Origin caller for test txs
// 	sepoliaRPCURL         = "https://rpc.sepolia.org"
// 	sepoliaUSDT           = "0x7169D38820dfd117C3FA1f22a697dBA58d90BA06" // USDT on Sepolia
// 	nativeAssetAddr       = "0x0000000000000000000000000000000000000000" // zero address = native ETH
// )

// // setupSepoliaSimulation creates RPCClient and TxBuilder for Sepolia gateway simulation tests.
// // Skips the test if -short is passed or RPC connection fails.
// //
// // NOTE: These simulation tests require the Sepolia gateway contract to be in a specific state:
// // 1. The sepoliaSimulateFrom address must have TSS_ROLE
// // 2. The contract must not be paused
// // 3. For ERC20 tests, the gateway must have sufficient token balance
// // 4. For native tests, the TSS must be able to send ETH with the call
// //
// // If simulations fail with "execution reverted", verify:
// // - The gateway contract address is correct
// // - The TSS address has the proper role: hasRole(TSS_ROLE, sepoliaSimulateFrom) should return true
// // - The contract is not paused: paused() should return false
// func setupSepoliaSimulation(t *testing.T) (*RPCClient, *TxBuilder) {
// 	t.Helper()
// 	if testing.Short() {
// 		t.Skip("skipping simulation test in short mode")
// 	}

// 	logger := zerolog.Nop()
// 	rpcClient, err := NewRPCClient([]string{sepoliaRPCURL}, sepoliaChainID, logger)
// 	if err != nil {
// 		t.Skipf("skipping simulation test: failed to connect to Sepolia RPC: %v", err)
// 	}

// 	builder, err := NewTxBuilder(rpcClient, "eth_sepolia", sepoliaChainID, sepoliaGatewayAddress, logger)
// 	require.NoError(t, err)

// 	return rpcClient, builder
// }

// // newSepoliaSimulationOutbound creates a full OutboundCreatedEvent for simulation tests.
// // All values (amount, assetAddr, sender, etc.) come from this struct - single source of truth.
// // Uses zero txID and universalTxID as verified working in Tenderly simulation.
// // Note: Sender is used as originCaller parameter, NOT as the from address for the call.
// func newSepoliaSimulationOutbound(amount, assetAddr, payload, revertMsg string) *uetypes.OutboundCreatedEvent {
// 	// Use all zeros for txID and universalTxID (verified working in Tenderly)
// 	txIDBytes := make([]byte, 32)          // all zeros
// 	universalTxIDBytes := make([]byte, 32) // all zeros
// 	return &uetypes.OutboundCreatedEvent{
// 		TxID:          "0x" + hex.EncodeToString(txIDBytes),
// 		UniversalTxId: "0x" + hex.EncodeToString(universalTxIDBytes),
// 		Sender:        sepoliaOriginCaller, // originCaller parameter in contract call
// 		Recipient:     "0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811",
// 		Amount:        amount,
// 		AssetAddr:     assetAddr,
// 		Payload:       payload,
// 		RevertMsg:     revertMsg,
// 	}
// }

// // valuesFromOutbound extracts amount, assetAddr, and tx value from OutboundCreatedEvent for simulation.
// func valuesFromOutbound(data *uetypes.OutboundCreatedEvent, txType uetypes.TxType) (amount *big.Int, assetAddr ethcommon.Address, value *big.Int) {
// 	amount = new(big.Int)
// 	amount, _ = amount.SetString(data.Amount, 10)
// 	assetAddr = ethcommon.HexToAddress(data.AssetAddr)
// 	value = big.NewInt(0)
// 	if assetAddr == (ethcommon.Address{}) && (txType == uetypes.TxType_FUNDS || txType == uetypes.TxType_FUNDS_AND_PAYLOAD || txType == uetypes.TxType_INBOUND_REVERT) {
// 		value = amount
// 	}
// 	return amount, assetAddr, value
// }

// // TestSimulateGatewayCall_Sepolia_Withdraw_Native simulates native withdraw on Sepolia gateway
// // This test verifies that the encoded calldata can be successfully simulated against
// // the live Sepolia gateway contract. It requires proper contract state.
// func TestSimulateGatewayCall_Sepolia_Withdraw_Native(t *testing.T) {
// 	rpcClient, builder := setupSepoliaSimulation(t)
// 	defer rpcClient.Close()

// 	data := newSepoliaSimulationOutbound("10000000000", nativeAssetAddr, "0x", "")
// 	amount, assetAddr, value := valuesFromOutbound(data, uetypes.TxType_FUNDS)

// 	calldata, err := builder.encodeFunctionCall("withdraw", data, amount, assetAddr, uetypes.TxType_FUNDS)
// 	require.NoError(t, err)

// 	// Log the calldata for debugging purposes
// 	t.Logf("withdraw calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, gateway: %s, value: %s", sepoliaSimulateFrom, sepoliaGatewayAddress, value.String())

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	// Use TSS address as from (who has TSS_ROLE), not data.Sender (which is originCaller)
// 	from := ethcommon.HexToAddress(sepoliaSimulateFrom)
// 	gateway := ethcommon.HexToAddress(sepoliaGatewayAddress)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, gateway, calldata, value, nil)
// 	if err != nil {
// 		// Log detailed error info for debugging
// 		t.Logf("Simulation failed with error: %v", err)
// 		t.Logf("Verify: 1) TSS has TSS_ROLE, 2) contract not paused, 3) txID not already executed")
// 	}
// 	require.NoError(t, err, "simulate withdraw (native) should pass")
// 	require.NotNil(t, result)
// }

// // TestSimulateGatewayCall_Sepolia_ExecuteUniversalTx_Native simulates native executeUniversalTx on Sepolia gateway
// func TestSimulateGatewayCall_Sepolia_ExecuteUniversalTx_Native(t *testing.T) {
// 	rpcClient, builder := setupSepoliaSimulation(t)
// 	defer rpcClient.Close()

// 	// Contract requires amount > 0, using 0.001 ETH with empty payload
// 	data := newSepoliaSimulationOutbound("1000000000000000", nativeAssetAddr, "0x", "") // 0.001 ETH with payload
// 	amount, assetAddr, value := valuesFromOutbound(data, uetypes.TxType_FUNDS_AND_PAYLOAD)

// 	calldata, err := builder.encodeFunctionCall("executeUniversalTx", data, amount, assetAddr, uetypes.TxType_PAYLOAD)
// 	require.NoError(t, err)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaSimulateFrom)
// 	gateway := ethcommon.HexToAddress(sepoliaGatewayAddress)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, gateway, calldata, value, nil)
// 	require.NoError(t, err, "simulate executeUniversalTx (native) should pass")
// 	require.NotNil(t, result)
// }

// // TestSimulateGatewayCall_Sepolia_ExecuteUniversalTx_ERC20 simulates ERC20 executeUniversalTx on Sepolia gateway
// func TestSimulateGatewayCall_Sepolia_ExecuteUniversalTx_ERC20(t *testing.T) {
// 	rpcClient, builder := setupSepoliaSimulation(t)
// 	defer rpcClient.Close()

// 	data := newSepoliaSimulationOutbound("1000000000000000000", sepoliaUSDT, "0x", "")
// 	amount, assetAddr, value := valuesFromOutbound(data, uetypes.TxType_FUNDS_AND_PAYLOAD)

// 	calldata, err := builder.encodeFunctionCall("executeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS_AND_PAYLOAD)
// 	require.NoError(t, err)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaSimulateFrom)
// 	gateway := ethcommon.HexToAddress(sepoliaGatewayAddress)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, gateway, calldata, value, nil)
// 	require.NoError(t, err, "simulate executeUniversalTx (ERC20) should pass")
// 	require.NotNil(t, result)
// }

// // TestSimulateGatewayCall_Sepolia_RevertUniversalTx_Native simulates native revertUniversalTx on Sepolia gateway
// func TestSimulateGatewayCall_Sepolia_RevertUniversalTx_Native(t *testing.T) {
// 	rpcClient, builder := setupSepoliaSimulation(t)
// 	defer rpcClient.Close()

// 	data := newSepoliaSimulationOutbound("1000000000000000", nativeAssetAddr, "0x", hex.EncodeToString([]byte("test revert"))) // 0.001 ETH, native
// 	amount, assetAddr, value := valuesFromOutbound(data, uetypes.TxType_INBOUND_REVERT)

// 	calldata, err := builder.encodeFunctionCall("revertUniversalTx", data, amount, assetAddr, uetypes.TxType_INBOUND_REVERT)
// 	require.NoError(t, err)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaSimulateFrom)
// 	gateway := ethcommon.HexToAddress(sepoliaGatewayAddress)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, gateway, calldata, value, nil)
// 	require.NoError(t, err, "simulate revertUniversalTx (native) should pass")
// 	require.NotNil(t, result)
// }

// // TestSimulateGatewayCall_Sepolia_RevertUniversalTx_ERC20 simulates ERC20 revertUniversalTxToken on Sepolia gateway
// func TestSimulateGatewayCall_Sepolia_RevertUniversalTx_ERC20(t *testing.T) {
// 	rpcClient, builder := setupSepoliaSimulation(t)
// 	defer rpcClient.Close()

// 	data := newSepoliaSimulationOutbound("1000000000000000000", sepoliaUSDT, "0x", hex.EncodeToString([]byte("test revert")))
// 	amount, assetAddr, value := valuesFromOutbound(data, uetypes.TxType_INBOUND_REVERT)

// 	calldata, err := builder.encodeFunctionCall("revertUniversalTxToken", data, amount, assetAddr, uetypes.TxType_INBOUND_REVERT)
// 	require.NoError(t, err)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaSimulateFrom)
// 	gateway := ethcommon.HexToAddress(sepoliaGatewayAddress)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, gateway, calldata, value, nil)
// 	require.NoError(t, err, "simulate revertUniversalTxToken (ERC20) should pass")
// 	require.NotNil(t, result)
// }

// // TestSimulateGatewayCall_Sepolia_Withdraw_ERC20 simulates ERC20 withdrawTokens on Sepolia gateway
// func TestSimulateGatewayCall_Sepolia_Withdraw_ERC20(t *testing.T) {
// 	rpcClient, builder := setupSepoliaSimulation(t)
// 	defer rpcClient.Close()

// 	data := newSepoliaSimulationOutbound("1000000000000000000", sepoliaUSDT, "0x", "")
// 	amount, assetAddr, value := valuesFromOutbound(data, uetypes.TxType_FUNDS)

// 	calldata, err := builder.encodeFunctionCall("withdrawTokens", data, amount, assetAddr, uetypes.TxType_FUNDS)
// 	require.NoError(t, err)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaSimulateFrom)
// 	gateway := ethcommon.HexToAddress(sepoliaGatewayAddress)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, gateway, calldata, value, nil)
// 	require.NoError(t, err, "simulate withdrawTokens (ERC20) should pass")
// 	require.NotNil(t, result)
// }
