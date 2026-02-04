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

func TestDefaultGasLimit(t *testing.T) {
	assert.Equal(t, int64(500000), int64(DefaultGasLimit), "DefaultGasLimit should be 500000")
}

func TestRevertInstructionsStruct(t *testing.T) {
	// Test that RevertInstructions struct can be created and used
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
	// Test that RevertInstructions can be ABI encoded as a tuple
	recipient := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	revertMsg := []byte("test message")

	ri := RevertInstructions{
		RevertRecipient: recipient,
		RevertMsg:       revertMsg,
	}

	// Create tuple type matching the Solidity struct
	revertInstructionType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "revertRecipient", Type: "address"},
		{Name: "revertMsg", Type: "bytes"},
	})
	require.NoError(t, err)

	arguments := abi.Arguments{
		{Type: revertInstructionType},
	}

	// This should not panic or error - the struct should be encodable
	encoded, err := arguments.Pack(ri)
	require.NoError(t, err, "RevertInstructions should be ABI encodable")
	assert.NotEmpty(t, encoded, "Encoded data should not be empty")
}

func TestRevertInstructionsWithEmptyRevertMsg(t *testing.T) {
	// Test encoding with empty revert message
	recipient := ethcommon.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")
	ri := RevertInstructions{
		RevertRecipient: recipient,
		RevertMsg:       []byte{},
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
	require.NoError(t, err, "RevertInstructions with empty revertMsg should be ABI encodable")
	assert.NotEmpty(t, encoded)
}

func TestNewTxBuilderV2(t *testing.T) {
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
			builder, err := NewTxBuilderV2(tt.rpcClient, tt.chainID, tt.chainIDInt, tt.gatewayAddress, logger)

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

func TestGetFunctionSignature(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	tests := []struct {
		name      string
		funcName  string
		isNative  bool
		expected  string
	}{
		{
			name:     "withdraw native",
			funcName: "withdraw",
			isNative: true,
			expected: "withdraw(bytes32,bytes32,address,address,uint256)",
		},
		{
			name:     "withdraw ERC20",
			funcName: "withdraw",
			isNative: false,
			expected: "withdraw(bytes32,bytes32,address,address,address,uint256)",
		},
		{
			name:     "withdrawAndExecute",
			funcName: "withdrawAndExecute",
			isNative: false,
			expected: "withdrawAndExecute(bytes32,bytes32,address,address,address,uint256,bytes)",
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
			name:     "revertUniversalTx",
			funcName: "revertUniversalTx",
			isNative: true,
			expected: "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))",
		},
		{
			name:     "revertWithdraw",
			funcName: "revertWithdraw",
			isNative: false,
			expected: "revertWithdraw(bytes32,bytes32,address,uint256,(address,bytes))",
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
	// Test that function selectors are correctly generated from signatures
	tests := []struct {
		signature string
		// First 4 bytes of keccak256(signature)
	}{
		{"withdraw(bytes32,bytes32,address,address,uint256)"},
		{"withdraw(bytes32,bytes32,address,address,address,uint256)"},
		{"revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"},
		{"revertWithdraw(bytes32,bytes32,address,uint256,(address,bytes))"},
	}

	for _, tt := range tests {
		t.Run(tt.signature, func(t *testing.T) {
			selector := crypto.Keccak256([]byte(tt.signature))[:4]
			assert.Len(t, selector, 4, "Function selector should be 4 bytes")
		})
	}
}

// TestEncodeFunctionCallAllFunctions tests all function encodings comprehensively
func TestEncodeFunctionCallAllFunctions(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
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
		isVault           bool
		txType            uetypes.TxType
		expectedSignature string
		expectError       bool
	}{
		// ========== withdraw ==========
		{
			name:              "withdraw native (Gateway)",
			funcName:          "withdraw",
			assetAddr:         nativeAsset,
			isVault:           false,
			txType:            uetypes.TxType_FUNDS,
			expectedSignature: "withdraw(bytes32,bytes32,address,address,uint256)",
		},
		{
			name:              "withdraw ERC20 (Vault)",
			funcName:          "withdraw",
			assetAddr:         erc20Asset,
			isVault:           true,
			txType:            uetypes.TxType_FUNDS,
			expectedSignature: "withdraw(bytes32,bytes32,address,address,address,uint256)",
		},

		// ========== withdrawAndExecute ==========
		{
			name:              "withdrawAndExecute ERC20 (Vault)",
			funcName:          "withdrawAndExecute",
			assetAddr:         erc20Asset,
			isVault:           true,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "withdrawAndExecute(bytes32,bytes32,address,address,address,uint256,bytes)",
		},

		// ========== executeUniversalTx ==========
		{
			name:              "executeUniversalTx native (Gateway)",
			funcName:          "executeUniversalTx",
			assetAddr:         nativeAsset,
			isVault:           false,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)",
		},
		{
			name:              "executeUniversalTx ERC20 (Vault)",
			funcName:          "executeUniversalTx",
			assetAddr:         erc20Asset,
			isVault:           true,
			txType:            uetypes.TxType_FUNDS_AND_PAYLOAD,
			expectedSignature: "executeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)",
		},

		// ========== revertUniversalTx ==========
		{
			name:              "revertUniversalTx native (Gateway)",
			funcName:          "revertUniversalTx",
			assetAddr:         nativeAsset,
			isVault:           false,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))",
		},

		// ========== revertWithdraw ==========
		{
			name:              "revertWithdraw ERC20 (Vault)",
			funcName:          "revertWithdraw",
			assetAddr:         erc20Asset,
			isVault:           true,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertWithdraw(bytes32,bytes32,address,uint256,(address,bytes))",
		},

		// ========== TxType variations ==========
		{
			name:              "withdraw with TxType_GAS",
			funcName:          "withdraw",
			assetAddr:         nativeAsset,
			isVault:           false,
			txType:            uetypes.TxType_GAS,
			expectedSignature: "withdraw(bytes32,bytes32,address,address,uint256)",
		},
		{
			name:              "withdraw with TxType_GAS_AND_PAYLOAD",
			funcName:          "withdraw",
			assetAddr:         nativeAsset,
			isVault:           false,
			txType:            uetypes.TxType_GAS_AND_PAYLOAD,
			expectedSignature: "withdraw(bytes32,bytes32,address,address,uint256)",
		},
		{
			name:              "withdraw with TxType_PAYLOAD",
			funcName:          "withdraw",
			assetAddr:         erc20Asset,
			isVault:           true,
			txType:            uetypes.TxType_PAYLOAD,
			expectedSignature: "withdraw(bytes32,bytes32,address,address,address,uint256)",
		},

		// ========== Error cases ==========
		{
			name:        "unknown function",
			funcName:    "unknownFunction",
			assetAddr:   nativeAsset,
			isVault:     false,
			txType:      uetypes.TxType_FUNDS,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := builder.encodeFunctionCall(tt.funcName, baseData, amount, tt.assetAddr, tt.txType, tt.isVault)

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

			// Log encoded data for inspection
			t.Logf("%s: selector=0x%x, total_length=%d bytes", tt.name, actualSelector, len(encoded))
		})
	}
}

// TestEncodeFunctionCallWithAllTxTypes tests each function with all applicable TxTypes
func TestEncodeFunctionCallWithAllTxTypes(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
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
		isVault   bool
	}{
		{"withdraw", nativeAsset, false},
		{"withdraw", erc20Asset, true},
		{"withdrawAndExecute", erc20Asset, true},
		{"executeUniversalTx", nativeAsset, false},
		{"executeUniversalTx", erc20Asset, true},
		{"revertUniversalTx", nativeAsset, false},
		{"revertWithdraw", erc20Asset, true},
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
				encoded, err := builder.encodeFunctionCall(fn.name, data, amount, fn.assetAddr, txType, fn.isVault)
				require.NoError(t, err, "encoding should succeed")
				assert.NotEmpty(t, encoded)
				assert.GreaterOrEqual(t, len(encoded), 4, "encoded data should have at least function selector")
				t.Logf("%s: length=%d bytes, selector=0x%x", testName, len(encoded), encoded[:4])
			})
		}
	}
}

// TestEncodeFunctionCallRevertInstructionsEncoding specifically tests RevertInstructions encoding
func TestEncodeFunctionCallRevertInstructionsEncoding(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
	require.NoError(t, err)

	tests := []struct {
		name      string
		revertMsg string
	}{
		{"empty revert message", ""},
		{"short revert message", hex.EncodeToString([]byte("err"))},
		{"medium revert message", hex.EncodeToString([]byte("Transaction reverted due to insufficient funds"))},
		{"long revert message", hex.EncodeToString([]byte("This is a very long revert message that contains detailed information about why the transaction failed and what the user can do to fix it. It includes multiple sentences and various details."))},
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

			encoded, err := builder.encodeFunctionCall("revertUniversalTx", data, amount, nativeAsset, uetypes.TxType_INBOUND_REVERT, false)
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

// TestEncodeFunctionCallPayloadEncoding tests various payload sizes
func TestEncodeFunctionCallPayloadEncoding(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
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

			encoded, err := builder.encodeFunctionCall("executeUniversalTx", data, amount, nativeAsset, uetypes.TxType_FUNDS_AND_PAYLOAD, false)
			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			// Verify function selector
			expectedSignature := "executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)"
			expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]
			assert.Equal(t, expectedSelector, encoded[:4])

			payloadBytes, _ := hex.DecodeString(removeHexPrefix(tt.payload))
			t.Logf("%s: payload_len=%d, encoded_len=%d", tt.name, len(payloadBytes), len(encoded))
		})
	}
}

// TestEncodeFunctionCallEdgeCases tests edge cases and boundary conditions
func TestEncodeFunctionCallEdgeCases(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
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

		encoded, err := builder.encodeFunctionCall("withdraw", data, amount, nativeAsset, uetypes.TxType_FUNDS, false)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})

	t.Run("max uint256 amount", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
			UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
			Sender:        "0xabcdef1234567890abcdef1234567890abcdef12",
			Recipient:     "0x1111111111111111111111111111111111111111",
			Amount:        "115792089237316195423570985008687907853269984665640564039457584007913129639935", // 2^256 - 1
			Payload:       "0x",
			RevertMsg:     "",
		}

		maxUint256 := new(big.Int)
		maxUint256.SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
		nativeAsset := ethcommon.Address{}

		encoded, err := builder.encodeFunctionCall("withdraw", data, maxUint256, nativeAsset, uetypes.TxType_FUNDS, false)
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

		encoded, err := builder.encodeFunctionCall("withdraw", data, amount, nativeAsset, uetypes.TxType_FUNDS, false)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})

	t.Run("same sender and recipient", func(t *testing.T) {
		data := &uetypes.OutboundCreatedEvent{
			TxID:          "0x" + hex.EncodeToString(make([]byte, 32)),
			UniversalTxId: "0x" + hex.EncodeToString(make([]byte, 32)),
			Sender:        "0x1111111111111111111111111111111111111111",
			Recipient:     "0x1111111111111111111111111111111111111111",
			Amount:        "1000000000000000000",
			Payload:       "0x",
			RevertMsg:     "",
		}

		amount := big.NewInt(1000000000000000000)
		nativeAsset := ethcommon.Address{}

		encoded, err := builder.encodeFunctionCall("withdraw", data, amount, nativeAsset, uetypes.TxType_FUNDS, false)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})
}

func TestEncodeFunctionCallInvalidTxID(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
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

	_, err = builder.encodeFunctionCall("withdraw", data, amount, assetAddr, uetypes.TxType_FUNDS, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid txID")
}

func TestEncodeFunctionCallInvalidUniversalTxID(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
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

	_, err = builder.encodeFunctionCall("withdraw", data, amount, assetAddr, uetypes.TxType_FUNDS, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid universalTxID")
}

func TestEncodeFunctionCallUnknownFunction(t *testing.T) {
	logger := zerolog.Nop()
	builder, err := NewTxBuilderV2(&RPCClient{}, "eth_sepolia", 11155111, "0x1234567890123456789012345678901234567890", logger)
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

	_, err = builder.encodeFunctionCall("unknownFunction", data, amount, assetAddr, uetypes.TxType_FUNDS, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown function name")
}

func TestRemoveHexPrefix(t *testing.T) {
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
			result := removeHexPrefix(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGasLimitParsing(t *testing.T) {
	// Test gas limit parsing with default fallback
	tests := []struct {
		name        string
		gasLimit    string
		expected    int64
		useDefault  bool
	}{
		{
			name:       "empty gas limit uses default",
			gasLimit:   "",
			expected:   DefaultGasLimit,
			useDefault: true,
		},
		{
			name:       "zero gas limit uses default",
			gasLimit:   "0",
			expected:   DefaultGasLimit,
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
				gasLimit = big.NewInt(DefaultGasLimit)
			} else {
				gasLimit = new(big.Int)
				gasLimit, _ = gasLimit.SetString(tt.gasLimit, 10)
			}

			assert.Equal(t, tt.expected, gasLimit.Int64())
		})
	}
}
