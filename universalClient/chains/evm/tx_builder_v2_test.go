package evm

import (
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

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// testVaultAddress is a non-zero address used as the vault in tests
const testVaultAddress = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// newTestTxBuilderV2 creates a TxBuilderV2 for unit tests by directly setting the
// vault address, bypassing the constructor's RPC call to VAULT().
func newTestTxBuilderV2(t *testing.T) *TxBuilderV2 {
	t.Helper()
	logger := zerolog.Nop()
	gwAddr := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	vAddr := ethcommon.HexToAddress(testVaultAddress)

	return &TxBuilderV2{
		rpcClient:      &RPCClient{},
		chainID:        "eth_sepolia",
		chainIDInt:     11155111,
		gatewayAddress: gwAddr,
		vaultAddress:   vAddr,
		vaultFetchedAt: time.Now(),
		logger:         logger.With().Str("component", "evm_tx_builder_v2").Logger(),
	}
}

func TestDefaultGasLimitV2(t *testing.T) {
	assert.Equal(t, int64(500000), int64(DefaultGasLimitV2))
}

func TestRevertInstructionsV2Struct(t *testing.T) {
	recipient := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	revertMsg := []byte("test revert message")

	ri := RevertInstructionsV2{
		RevertRecipient: recipient,
		RevertMsg:       revertMsg,
	}

	assert.Equal(t, recipient, ri.RevertRecipient)
	assert.Equal(t, revertMsg, ri.RevertMsg)
}

func TestRevertInstructionsV2ABIEncoding(t *testing.T) {
	recipient := ethcommon.HexToAddress("0x1234567890123456789012345678901234567890")
	revertMsg := []byte("test message")

	ri := RevertInstructionsV2{
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
	require.NoError(t, err, "RevertInstructionsV2 should be ABI encodable")
	assert.NotEmpty(t, encoded)
}

func TestGetFunctionSignatureV2(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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
			signature := builder.getFunctionSignatureV2(tt.funcName, tt.isNative)
			assert.Equal(t, tt.expected, signature)
		})
	}
}

func TestFunctionSelectorGenerationV2(t *testing.T) {
	tests := []struct {
		signature string
	}{
		{"finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"},
		{"revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"},
		{"revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))"},
	}

	for _, tt := range tests {
		t.Run(tt.signature, func(t *testing.T) {
			selector := crypto.Keccak256([]byte(tt.signature))[:4]
			assert.Len(t, selector, 4)
		})
	}
}

func TestDetermineFunctionNameV2(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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
		{"INBOUND_REVERT ERC20", uetypes.TxType_INBOUND_REVERT, erc20Asset, "revertUniversalTxToken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcName := builder.determineFunctionNameV2(tt.txType, tt.assetAddr)
			assert.Equal(t, tt.expectedFunc, funcName)
		})
	}
}

func TestResolveTxParamsV2(t *testing.T) {
	builder := newTestTxBuilderV2(t)

	nativeAsset := ethcommon.Address{}
	erc20Asset := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	amount := big.NewInt(1000000000000000000)

	tests := []struct {
		name          string
		funcName      string
		assetAddr     ethcommon.Address
		expectedTo    ethcommon.Address
		expectedValue *big.Int
	}{
		{
			name:          "finalizeUniversalTx native → vault with value",
			funcName:      "finalizeUniversalTx",
			assetAddr:     nativeAsset,
			expectedTo:    ethcommon.HexToAddress(testVaultAddress),
			expectedValue: amount,
		},
		{
			name:          "finalizeUniversalTx ERC20 → vault no value",
			funcName:      "finalizeUniversalTx",
			assetAddr:     erc20Asset,
			expectedTo:    ethcommon.HexToAddress(testVaultAddress),
			expectedValue: big.NewInt(0),
		},
		{
			name:          "revertUniversalTx native → gateway with value",
			funcName:      "revertUniversalTx",
			assetAddr:     nativeAsset,
			expectedTo:    ethcommon.HexToAddress("0x1234567890123456789012345678901234567890"),
			expectedValue: amount,
		},
		{
			name:          "revertUniversalTxToken ERC20 → vault no value",
			funcName:      "revertUniversalTxToken",
			assetAddr:     erc20Asset,
			expectedTo:    ethcommon.HexToAddress(testVaultAddress),
			expectedValue: big.NewInt(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txValue, toAddr, err := builder.resolveTxParams(tt.funcName, tt.assetAddr, amount)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedTo, toAddr)
			assert.Equal(t, tt.expectedValue.Int64(), txValue.Int64())
		})
	}
}

func TestEncodeFunctionCallV2AllFunctions(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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
			expectedSignature: "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))",
		},
		{
			name:              "revertUniversalTxToken ERC20",
			funcName:          "revertUniversalTxToken",
			assetAddr:         erc20Asset,
			txType:            uetypes.TxType_INBOUND_REVERT,
			expectedSignature: "revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))",
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
			encoded, err := builder.encodeFunctionCallV2(tt.funcName, baseData, amount, tt.assetAddr, tt.txType)

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

func TestEncodeFunctionCallV2RevertInstructionsEncoding(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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

			encoded, err := builder.encodeFunctionCallV2("revertUniversalTx", data, amount, nativeAsset, uetypes.TxType_INBOUND_REVERT)
			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			expectedSignature := "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"
			expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]
			assert.Equal(t, expectedSelector, encoded[:4])
		})
	}
}

func TestEncodeFunctionCallV2PayloadEncoding(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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

			encoded, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, nativeAsset, uetypes.TxType_FUNDS_AND_PAYLOAD)
			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			expectedSignature := "finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"
			expectedSelector := crypto.Keccak256([]byte(expectedSignature))[:4]
			assert.Equal(t, expectedSelector, encoded[:4])
		})
	}
}

func TestEncodeFunctionCallV2EdgeCases(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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

		encoded, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, nativeAsset, uetypes.TxType_FUNDS)
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

		encoded, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, maxUint256, nativeAsset, uetypes.TxType_FUNDS)
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)
	})
}

func TestEncodeFunctionCallV2InvalidTxID(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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

	_, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid txID")
}

func TestEncodeFunctionCallV2InvalidUniversalTxID(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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

	_, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid universalTxID")
}

func TestRemoveHexPrefixV2(t *testing.T) {
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
			result := removeHexPrefixV2(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseTxTypeV2(t *testing.T) {
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
			result, err := parseTxTypeV2(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseGasLimitV2(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"empty uses default", "", DefaultGasLimitV2},
		{"zero uses default", "0", DefaultGasLimitV2},
		{"valid gas limit", "100000", 100000},
		{"large gas limit", "1000000", 1000000},
		{"invalid uses default", "not-a-number", DefaultGasLimitV2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGasLimitV2(tt.input)
			assert.Equal(t, tt.expected, result.Int64())
		})
	}
}

// TestFinalizeUniversalTxUnifiedEncoding verifies that finalizeUniversalTx is used
// for all non-revert tx types (replacing withdraw, withdrawTokens, executeUniversalTx)
func TestFinalizeUniversalTxUnifiedEncoding(t *testing.T) {
	builder := newTestTxBuilderV2(t)

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
				funcName := builder.determineFunctionNameV2(txType, asset.addr)
				assert.Equal(t, "finalizeUniversalTx", funcName)

				encoded, err := builder.encodeFunctionCallV2(funcName, data, amount, asset.addr, txType)
				require.NoError(t, err)
				assert.Equal(t, expectedSelector, encoded[:4])
			})
		}
	}
}

func TestGetVaultAddressReturnsCached(t *testing.T) {
	builder := newTestTxBuilderV2(t)

	// Vault was set at construction with time.Now(), so it's fresh
	addr, err := builder.getVaultAddress()
	require.NoError(t, err)
	assert.Equal(t, ethcommon.HexToAddress(testVaultAddress), addr)
}

func TestGetVaultAddressStaleTriggersRefresh(t *testing.T) {
	builder := newTestTxBuilderV2(t)

	// Make the cache stale
	builder.vaultFetchedAt = time.Now().Add(-2 * vaultRefreshInterval)

	// Should still return the cached address (refresh is async, and will fail
	// since we have no real RPC — that's fine, stale address is kept)
	addr, err := builder.getVaultAddress()
	require.NoError(t, err)
	assert.Equal(t, ethcommon.HexToAddress(testVaultAddress), addr)

	// Give the goroutine a moment to attempt and fail refresh
	time.Sleep(50 * time.Millisecond)

	// Address should still be the same (failed refresh keeps stale)
	addr, err = builder.getVaultAddress()
	require.NoError(t, err)
	assert.Equal(t, ethcommon.HexToAddress(testVaultAddress), addr)
}

func TestGetVaultAddressFailsWhenNeverFetched(t *testing.T) {
	logger := zerolog.Nop()
	// Create builder with zero vault address (simulates failed init fetch)
	builder := &TxBuilderV2{
		rpcClient:      &RPCClient{},
		chainID:        "eth_sepolia",
		chainIDInt:     11155111,
		gatewayAddress: ethcommon.HexToAddress("0x1234567890123456789012345678901234567890"),
		logger:         logger.With().Str("component", "evm_tx_builder_v2").Logger(),
	}

	// Should fail since vault was never fetched and RPC is not real
	_, err := builder.getVaultAddress()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vault address not available")
}

func TestVaultCallSelector(t *testing.T) {
	// Verify the VAULT() selector is correct
	expected := crypto.Keccak256([]byte("VAULT()"))[:4]
	assert.Equal(t, expected, vaultCallSelector)
	t.Logf("VAULT() selector: 0x%x", vaultCallSelector)
}

// =============================================================================
// Sepolia V2 Simulation Tests
//
// These tests simulate contract calls (eth_call) against the live V2 gateway
// and vault contracts on Sepolia. They verify that the encoded calldata is
// accepted by the on-chain contracts.
//
// Prerequisites:
// 1. sepoliaV2SimulateFrom must have TSS_ROLE on both gateway and vault
// 2. Contracts must not be paused
// 3. For ERC20 tests, the vault must have sufficient token balance
// 4. The gateway's VAULT() must return a valid vault address
//
// These tests are skipped in -short mode and when RPC connection fails.
// =============================================================================

// const (
// 	sepoliaV2GatewayAddress = "0x4DCab975cDe839632db6695e2e936A29ce3e325E"
// 	sepoliaV2SimulateFrom   = "0x05d7386fb3d7cb00e0cfac5af3b2eff6bf37c5f1" // TSS Address (has TSS_ROLE)
// 	sepoliaV2OriginCaller   = "0x35B84d6848D16415177c64D64504663b998A6ab4" // Push account / origin caller
// 	sepoliaV2RPCURL         = "https://rpc.sepolia.org"
// 	sepoliaV2ChainID        = int64(11155111)
// 	sepoliaV2USDT           = "0x7169D38820dfd117C3FA1f22a697dBA58d90BA06" // USDT on Sepolia
// 	sepoliaV2NativeAsset    = "0x0000000000000000000000000000000000000000"
// )

// // setupSepoliaV2Simulation creates RPCClient and TxBuilderV2 for Sepolia V2 simulation tests.
// // Skips the test if -short is passed or RPC connection fails.
// func setupSepoliaV2Simulation(t *testing.T) (*RPCClient, *TxBuilderV2) {
// 	t.Helper()
// 	if testing.Short() {
// 		t.Skip("skipping simulation test in short mode")
// 	}

// 	logger := zerolog.Nop()
// 	rpcClient, err := NewRPCClient([]string{sepoliaV2RPCURL}, sepoliaV2ChainID, logger)
// 	if err != nil {
// 		t.Skipf("skipping simulation test: failed to connect to Sepolia RPC: %v", err)
// 	}

// 	builder, err := NewTxBuilderV2(rpcClient, "eip155:11155111", sepoliaV2ChainID, sepoliaV2GatewayAddress, logger)
// 	if err != nil {
// 		t.Skipf("skipping simulation test: failed to create TxBuilderV2: %v", err)
// 	}

// 	// Log the vault address fetched from gateway
// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err, "vault address should be available after init")
// 	t.Logf("V2 Gateway: %s", sepoliaV2GatewayAddress)
// 	t.Logf("V2 Vault (from VAULT()): %s", vaultAddr.Hex())
// 	t.Logf("V2 TSS: %s", sepoliaV2SimulateFrom)

// 	return rpcClient, builder
// }

// // encodeMulticallPayload ABI-encodes a Multicall[] array for use as the `data` param in finalizeUniversalTx.
// // The CEA contract expects: abi.encode(Multicall[]) where Multicall is (address to, uint256 value, bytes data).
// // Pass nil or empty slice for an empty multicall (no-op / funds-only).
// func encodeMulticallPayload(t *testing.T, calls []struct {
// 	To    ethcommon.Address
// 	Value *big.Int
// 	Data  []byte
// }) string {
// 	t.Helper()

// 	// ABI type for Multicall[] = tuple(address,uint256,bytes)[]
// 	multicallArrayType, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
// 		{Name: "to", Type: "address"},
// 		{Name: "value", Type: "uint256"},
// 		{Name: "data", Type: "bytes"},
// 	})
// 	require.NoError(t, err)

// 	type Multicall struct {
// 		To    ethcommon.Address
// 		Value *big.Int
// 		Data  []byte
// 	}

// 	var multicalls []Multicall
// 	for _, c := range calls {
// 		multicalls = append(multicalls, Multicall{To: c.To, Value: c.Value, Data: c.Data})
// 	}
// 	if multicalls == nil {
// 		multicalls = []Multicall{}
// 	}

// 	args := abi.Arguments{{Type: multicallArrayType}}
// 	encoded, err := args.Pack(multicalls)
// 	require.NoError(t, err)

// 	return "0x" + hex.EncodeToString(encoded)
// }

// // newSepoliaV2SimulationOutbound creates a full OutboundCreatedEvent for V2 simulation tests.
// // Uses unique txID per test to avoid "PayloadExecuted" revert from duplicate txIDs.
// // Sender is used as pushAccount parameter (origin caller), NOT as the from address.
// func newSepoliaV2SimulationOutbound(t *testing.T, amount, assetAddr, payload, revertMsg string) *uetypes.OutboundCreatedEvent {
// 	t.Helper()
// 	// Use test name hash for unique txID to avoid isExecuted collision
// 	txIDBytes := crypto.Keccak256([]byte(t.Name() + time.Now().String()))
// 	universalTxIDBytes := crypto.Keccak256([]byte("utx-" + t.Name() + time.Now().String()))

// 	return &uetypes.OutboundCreatedEvent{
// 		TxID:          "0x" + hex.EncodeToString(txIDBytes),
// 		UniversalTxId: "0x" + hex.EncodeToString(universalTxIDBytes),
// 		Sender:        sepoliaV2OriginCaller,
// 		Recipient:     "0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811",
// 		Amount:        amount,
// 		AssetAddr:     assetAddr,
// 		Payload:       payload,
// 		RevertMsg:     revertMsg,
// 	}
// }

// // TestSimulateV2_FetchVaultFromGateway verifies that VAULT() can be read from the gateway
// func TestSimulateV2_FetchVaultFromGateway(t *testing.T) {
// 	if testing.Short() {
// 		t.Skip("skipping simulation test in short mode")
// 	}

// 	logger := zerolog.Nop()
// 	rpcClient, err := NewRPCClient([]string{sepoliaV2RPCURL}, sepoliaV2ChainID, logger)
// 	if err != nil {
// 		t.Skipf("skipping: failed to connect to Sepolia RPC: %v", err)
// 	}
// 	defer rpcClient.Close()

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	gwAddr := ethcommon.HexToAddress(sepoliaV2GatewayAddress)
// 	result, err := rpcClient.CallContract(ctx, gwAddr, vaultCallSelector, nil)
// 	require.NoError(t, err, "VAULT() call should succeed")
// 	require.True(t, len(result) >= 32, "VAULT() should return at least 32 bytes")

// 	vaultAddr := ethcommon.BytesToAddress(result[12:32])
// 	assert.NotEqual(t, ethcommon.Address{}, vaultAddr, "VAULT() should not return zero address")
// 	t.Logf("VAULT() returned: %s", vaultAddr.Hex())
// }

// // ---------- 1. Native Revert (Gateway) ----------

// // TestSimulateV2_RevertUniversalTx_Native simulates native revertUniversalTx on Sepolia gateway
// func TestSimulateV2_RevertUniversalTx_Native(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	revertMsg := hex.EncodeToString([]byte("test revert"))
// 	data := newSepoliaV2SimulationOutbound(t, "1000000000000000", sepoliaV2NativeAsset, "0x", revertMsg) // 0.001 ETH
// 	amount := new(big.Int)
// 	amount.SetString(data.Amount, 10)
// 	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

// 	calldata, err := builder.encodeFunctionCallV2("revertUniversalTx", data, amount, assetAddr, uetypes.TxType_INBOUND_REVERT)
// 	require.NoError(t, err)

// 	t.Logf("revertUniversalTx (native) calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, gateway: %s, value: %s", sepoliaV2SimulateFrom, sepoliaV2GatewayAddress, amount.String())

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaV2SimulateFrom)
// 	gateway := ethcommon.HexToAddress(sepoliaV2GatewayAddress)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, gateway, calldata, amount, nil)
// 	if err != nil {
// 		t.Logf("Simulation failed: %v", err)
// 		t.Logf("Verify: 1) TSS has onlyTSS on gateway, 2) gateway not paused, 3) txID not already executed")
// 	}
// 	require.NoError(t, err, "simulate revertUniversalTx (native) should pass")
// 	require.NotNil(t, result)
// }

// // ---------- 2. Token Revert (Vault) ----------

// // TestSimulateV2_RevertUniversalTxToken_ERC20 simulates ERC20 revertUniversalTxToken on Sepolia vault
// func TestSimulateV2_RevertUniversalTxToken_ERC20(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	revertMsg := hex.EncodeToString([]byte("test revert token"))
// 	data := newSepoliaV2SimulationOutbound(t, "1000000", sepoliaV2USDT, "0x", revertMsg) // 1 USDT (6 decimals)
// 	amount := new(big.Int)
// 	amount.SetString(data.Amount, 10)
// 	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

// 	calldata, err := builder.encodeFunctionCallV2("revertUniversalTxToken", data, amount, assetAddr, uetypes.TxType_INBOUND_REVERT)
// 	require.NoError(t, err)

// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err)

// 	t.Logf("revertUniversalTxToken (ERC20) calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, vault: %s, token: %s", sepoliaV2SimulateFrom, vaultAddr.Hex(), sepoliaV2USDT)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaV2SimulateFrom)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, vaultAddr, calldata, big.NewInt(0), nil)
// 	if err != nil {
// 		t.Logf("Simulation failed: %v", err)
// 		t.Logf("Verify: 1) TSS has TSS_ROLE on vault, 2) vault has token balance, 3) token is supported")
// 	}
// 	require.NoError(t, err, "simulate revertUniversalTxToken (ERC20) should pass")
// 	require.NotNil(t, result)
// }

// // ---------- 3. Native FinalizeUniversalTx — no payload (Vault) ----------

// // TestSimulateV2_FinalizeUniversalTx_Native simulates native finalizeUniversalTx (funds only, no payload)
// func TestSimulateV2_FinalizeUniversalTx_Native(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	emptyMulticall := encodeMulticallPayload(t, nil)
// 	data := newSepoliaV2SimulationOutbound(t, "10000000000", sepoliaV2NativeAsset, emptyMulticall, "") // ~10 gwei worth of ETH, no payload
// 	amount := new(big.Int)
// 	amount.SetString(data.Amount, 10)
// 	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

// 	calldata, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS)
// 	require.NoError(t, err)

// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err)

// 	t.Logf("finalizeUniversalTx (native, no payload) calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, vault: %s, value: %s", sepoliaV2SimulateFrom, vaultAddr.Hex(), amount.String())

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaV2SimulateFrom)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, vaultAddr, calldata, amount, nil)
// 	if err != nil {
// 		t.Logf("Simulation failed: %v", err)
// 		t.Logf("Verify: 1) TSS has TSS_ROLE on vault, 2) vault not paused, 3) txID not already executed")
// 	}
// 	require.NoError(t, err, "simulate finalizeUniversalTx (native, no payload) should pass")
// 	require.NotNil(t, result)
// }

// // ---------- 4. ERC20 FinalizeUniversalTx — no payload (Vault) ----------

// // TestSimulateV2_FinalizeUniversalTx_ERC20 simulates ERC20 finalizeUniversalTx (funds only, no payload)
// func TestSimulateV2_FinalizeUniversalTx_ERC20(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	emptyMulticall := encodeMulticallPayload(t, nil)
// 	data := newSepoliaV2SimulationOutbound(t, "1000000", sepoliaV2USDT, emptyMulticall, "") // 1 USDT (6 decimals), no payload
// 	amount := new(big.Int)
// 	amount.SetString(data.Amount, 10)
// 	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

// 	calldata, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS)
// 	require.NoError(t, err)

// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err)

// 	t.Logf("finalizeUniversalTx (ERC20, no payload) calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, vault: %s, token: %s", sepoliaV2SimulateFrom, vaultAddr.Hex(), sepoliaV2USDT)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaV2SimulateFrom)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, vaultAddr, calldata, big.NewInt(0), nil)
// 	if err != nil {
// 		t.Logf("Simulation failed: %v", err)
// 		t.Logf("Verify: 1) TSS has TSS_ROLE on vault, 2) vault has token balance, 3) token is supported")
// 	}
// 	require.NoError(t, err, "simulate finalizeUniversalTx (ERC20, no payload) should pass")
// 	require.NotNil(t, result)
// }

// // ---------- 5. Native FinalizeUniversalTx — with payload (Vault) ----------

// // TestSimulateV2_FinalizeUniversalTx_NativeWithPayload simulates native finalizeUniversalTx with funds + payload
// func TestSimulateV2_FinalizeUniversalTx_NativeWithPayload(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	// Multicall: send 0.001 ETH to recipient
// 	recipient := ethcommon.HexToAddress("0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811")
// 	nativeMulticall := encodeMulticallPayload(t, []struct {
// 		To    ethcommon.Address
// 		Value *big.Int
// 		Data  []byte
// 	}{{To: recipient, Value: big.NewInt(1000000000000000), Data: nil}})
// 	data := newSepoliaV2SimulationOutbound(t, "1000000000000000", sepoliaV2NativeAsset, nativeMulticall, "") // 0.001 ETH + payload
// 	amount := new(big.Int)
// 	amount.SetString(data.Amount, 10)
// 	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

// 	calldata, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS_AND_PAYLOAD)
// 	require.NoError(t, err)

// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err)

// 	t.Logf("finalizeUniversalTx (native+payload) calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, vault: %s, value: %s", sepoliaV2SimulateFrom, vaultAddr.Hex(), amount.String())

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaV2SimulateFrom)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, vaultAddr, calldata, amount, nil)
// 	if err != nil {
// 		t.Logf("Simulation failed: %v", err)
// 	}
// 	require.NoError(t, err, "simulate finalizeUniversalTx (native+payload) should pass")
// 	require.NotNil(t, result)
// }

// // ---------- 6. ERC20 FinalizeUniversalTx — with payload (Vault) ----------

// // TestSimulateV2_FinalizeUniversalTx_ERC20WithPayload simulates ERC20 finalizeUniversalTx with funds + payload
// func TestSimulateV2_FinalizeUniversalTx_ERC20WithPayload(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	// Multicall: transfer 1 USDT to recipient via ERC20.transfer
// 	erc20Recipient := ethcommon.HexToAddress("0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811")
// 	transferSel := crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
// 	transferArgs := abi.Arguments{
// 		{Type: func() abi.Type { t, _ := abi.NewType("address", "", nil); return t }()},
// 		{Type: func() abi.Type { t, _ := abi.NewType("uint256", "", nil); return t }()},
// 	}
// 	transferData, err := transferArgs.Pack(erc20Recipient, big.NewInt(1000000)) // 1 USDT (6 decimals)
// 	require.NoError(t, err)
// 	erc20TransferCalldata := append(transferSel, transferData...)
// 	usdtAddr := ethcommon.HexToAddress(sepoliaV2USDT)
// 	erc20Multicall := encodeMulticallPayload(t, []struct {
// 		To    ethcommon.Address
// 		Value *big.Int
// 		Data  []byte
// 	}{{To: usdtAddr, Value: big.NewInt(0), Data: erc20TransferCalldata}})
// 	data := newSepoliaV2SimulationOutbound(t, "1000000", sepoliaV2USDT, erc20Multicall, "") // 1 USDT (6 decimals) + payload
// 	amount := new(big.Int)
// 	amount.SetString(data.Amount, 10)
// 	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

// 	calldata, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_FUNDS_AND_PAYLOAD)
// 	require.NoError(t, err)

// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err)

// 	t.Logf("finalizeUniversalTx (ERC20+payload) calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, vault: %s, token: %s", sepoliaV2SimulateFrom, vaultAddr.Hex(), sepoliaV2USDT)

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaV2SimulateFrom)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, vaultAddr, calldata, big.NewInt(0), nil)
// 	if err != nil {
// 		t.Logf("Simulation failed: %v", err)
// 	}
// 	require.NoError(t, err, "simulate finalizeUniversalTx (ERC20+payload) should pass")
// 	require.NotNil(t, result)
// }

// // ---------- 7. FinalizeUniversalTx — payload only, no funds (Vault) ----------

// // TestSimulateV2_FinalizeUniversalTx_PayloadOnly simulates finalizeUniversalTx with only payload (no funds)
// // Uses native asset (zero address) with zero amount — pure contract execution via CEA
// func TestSimulateV2_FinalizeUniversalTx_PayloadOnly(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	// amount=0, native asset, non-empty payload → PAYLOAD tx type
// 	// Multicall: simple call to recipient with no value (pure execution)
// 	payloadRecipient := ethcommon.HexToAddress("0x28F1C7B4596D9db14f85c04DcBd867Bf4b14b811")
// 	payloadOnlyMulticall := encodeMulticallPayload(t, []struct {
// 		To    ethcommon.Address
// 		Value *big.Int
// 		Data  []byte
// 	}{{To: payloadRecipient, Value: big.NewInt(0), Data: []byte{0xde, 0xad, 0xbe, 0xef}}})
// 	data := newSepoliaV2SimulationOutbound(t, "0", sepoliaV2NativeAsset, payloadOnlyMulticall, "")
// 	amount := big.NewInt(0)
// 	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

// 	calldata, err := builder.encodeFunctionCallV2("finalizeUniversalTx", data, amount, assetAddr, uetypes.TxType_PAYLOAD)
// 	require.NoError(t, err)

// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err)

// 	t.Logf("finalizeUniversalTx (payload only) calldata: 0x%s", hex.EncodeToString(calldata))
// 	t.Logf("from (TSS): %s, vault: %s, value: 0", sepoliaV2SimulateFrom, vaultAddr.Hex())

// 	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
// 	defer cancel()

// 	from := ethcommon.HexToAddress(sepoliaV2SimulateFrom)
// 	result, err := rpcClient.CallContractWithFrom(ctx, from, vaultAddr, calldata, big.NewInt(0), nil)
// 	if err != nil {
// 		t.Logf("Simulation failed: %v", err)
// 	}
// 	require.NoError(t, err, "simulate finalizeUniversalTx (payload only) should pass")
// 	require.NotNil(t, result)
// }

// // ---------- Routing verification ----------

// // TestSimulateV2_FullFlow_ResolveTxParams verifies that resolveTxParams routes to the correct contract
// func TestSimulateV2_FullFlow_ResolveTxParams(t *testing.T) {
// 	rpcClient, builder := setupSepoliaV2Simulation(t)
// 	defer rpcClient.Close()

// 	vaultAddr, err := builder.getVaultAddress()
// 	require.NoError(t, err)
// 	gatewayAddr := ethcommon.HexToAddress(sepoliaV2GatewayAddress)

// 	amount := big.NewInt(1000000000000000) // 0.001 ETH
// 	nativeAsset := ethcommon.Address{}
// 	erc20Asset := ethcommon.HexToAddress(sepoliaV2USDT)

// 	tests := []struct {
// 		name       string
// 		funcName   string
// 		assetAddr  ethcommon.Address
// 		expectedTo ethcommon.Address
// 		expectVal  bool // true if txValue should equal amount
// 	}{
// 		{"finalizeUniversalTx native → vault", "finalizeUniversalTx", nativeAsset, vaultAddr, true},
// 		{"finalizeUniversalTx ERC20 → vault", "finalizeUniversalTx", erc20Asset, vaultAddr, false},
// 		{"revertUniversalTx native → gateway", "revertUniversalTx", nativeAsset, gatewayAddr, true},
// 		{"revertUniversalTxToken ERC20 → vault", "revertUniversalTxToken", erc20Asset, vaultAddr, false},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			txValue, toAddr, err := builder.resolveTxParams(tt.funcName, tt.assetAddr, amount)
// 			require.NoError(t, err)
// 			assert.Equal(t, tt.expectedTo, toAddr, "should route to correct contract")
// 			if tt.expectVal {
// 				assert.Equal(t, amount.Int64(), txValue.Int64(), "native calls should have value=amount")
// 			} else {
// 				assert.Equal(t, int64(0), txValue.Int64(), "ERC20 calls should have value=0")
// 			}
// 			t.Logf("%s → to=%s value=%s", tt.funcName, toAddr.Hex(), txValue.String())
// 		})
// 	}
// }
