package evm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// DefaultGasLimit is used when gas limit is not provided in the outbound event data
const DefaultGasLimit = 500000

// RevertInstructions represents the struct for revert instruction in gateway/vault contracts
type RevertInstructions struct {
	RevertRecipient ethcommon.Address
	RevertMsg       []byte
}

// TxBuilder implements OutboundTxBuilder for EVM chains
type TxBuilder struct {
	rpcClient      *RPCClient
	chainID        string
	chainIDInt     int64
	gatewayAddress ethcommon.Address
	vaultAddress   *ethcommon.Address // Cached vault address (nil if not fetched yet)
	vaultMu        sync.RWMutex       // Protects vaultAddress
	logger         zerolog.Logger
}

// NewTxBuilder creates a new EVM transaction builder
func NewTxBuilder(
	rpcClient *RPCClient,
	chainID string,
	chainIDInt int64,
	gatewayAddress string,
	logger zerolog.Logger,
) (*TxBuilder, error) {
	if rpcClient == nil {
		return nil, fmt.Errorf("rpcClient is required")
	}
	if chainID == "" {
		return nil, fmt.Errorf("chainID is required")
	}
	if gatewayAddress == "" {
		return nil, fmt.Errorf("gatewayAddress is required")
	}

	addr := ethcommon.HexToAddress(gatewayAddress)
	if addr == (ethcommon.Address{}) {
		return nil, fmt.Errorf("invalid gateway address: %s", gatewayAddress)
	}

	return &TxBuilder{
		rpcClient:      rpcClient,
		chainID:        chainID,
		chainIDInt:     chainIDInt,
		gatewayAddress: addr,
		logger:         logger.With().Str("component", "evm_tx_builder").Str("chain", chainID).Logger(),
	}, nil
}

// GetOutboundSigningRequest creates a signing request from outbound event data
func (tb *TxBuilder) GetOutboundSigningRequest(
	ctx context.Context,
	data *uetypes.OutboundCreatedEvent,
	gasPrice *big.Int,
	signerAddress string,
) (*common.UnSignedOutboundTxReq, error) {
	if data == nil {
		return nil, fmt.Errorf("outbound event data is nil")
	}
	if data.TxID == "" {
		return nil, fmt.Errorf("txID is required")
	}
	if data.DestinationChain == "" {
		return nil, fmt.Errorf("destinationChain is required")
	}
	if gasPrice == nil {
		return nil, fmt.Errorf("gasPrice is required")
	}
	if signerAddress == "" {
		return nil, fmt.Errorf("signerAddress is required")
	}

	// Parse signer address
	signerAddr := ethcommon.HexToAddress(signerAddress)

	// Get nonce for the signer address
	nonce, err := tb.rpcClient.GetNonceAt(ctx, signerAddr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce for signer %s: %w", signerAddress, err)
	}

	// Parse gas limit, use default if not provided
	var gasLimit *big.Int
	if data.GasLimit == "" || data.GasLimit == "0" {
		gasLimit = big.NewInt(DefaultGasLimit)
	} else {
		gasLimit = new(big.Int)
		var ok bool
		gasLimit, ok = gasLimit.SetString(data.GasLimit, 10)
		if !ok {
			return nil, fmt.Errorf("invalid gas limit: %s", data.GasLimit)
		}
	}

	// Parse amount
	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", data.Amount)
	}

	// Parse asset address
	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

	// Parse TxType
	txType, err := parseTxType(data.TxType)
	if err != nil {
		return nil, fmt.Errorf("invalid tx type: %w", err)
	}

	// Determine target contract (gateway or vault)
	targetContract, err := tb.determineTargetContract(ctx, assetAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to determine target contract: %w", err)
	}

	// Determine if we're calling vault or gateway
	isVault := targetContract != tb.gatewayAddress

	// Determine function name based on TxType, asset type, and target contract
	funcName := tb.determineFunctionName(txType, assetAddr, isVault)

	// Encode function call
	txData, err := tb.encodeFunctionCall(funcName, data, amount, assetAddr, txType, isVault)
	if err != nil {
		return nil, fmt.Errorf("failed to encode function call: %w", err)
	}

	// Determine transaction value
	// For native token transactions (withdraw, executeUniversalTx native, revertUniversalTx),
	// the value must equal the amount
	var txValue *big.Int
	isNative := assetAddr == (ethcommon.Address{})
	if isNative && (txType == uetypes.TxType_FUNDS || txType == uetypes.TxType_FUNDS_AND_PAYLOAD || txType == uetypes.TxType_INBOUND_REVERT) {
		txValue = amount
	} else {
		txValue = big.NewInt(0)
	}

	// Create unsigned transaction
	tx := types.NewTransaction(
		nonce,
		targetContract,
		txValue,
		gasLimit.Uint64(),
		gasPrice,
		txData,
	)

	// Calculate signing hash (EIP-155)
	signer := types.NewEIP155Signer(big.NewInt(tb.chainIDInt))
	txHash := signer.Hash(tx).Bytes()

	return &common.UnSignedOutboundTxReq{
		SigningHash: txHash,
		Signer:      signerAddress,
		Nonce:       nonce,
		GasPrice:    gasPrice,
	}, nil
}

// BroadcastOutboundSigningRequest assembles and broadcasts a signed transaction from the signing request, event data, and signature
func (tb *TxBuilder) BroadcastOutboundSigningRequest(
	ctx context.Context,
	req *common.UnSignedOutboundTxReq,
	data *uetypes.OutboundCreatedEvent,
	signature []byte,
) (string, error) {
	if req == nil {
		return "", fmt.Errorf("signing request is nil")
	}
	if data == nil {
		return "", fmt.Errorf("outbound event data is nil")
	}
	if len(signature) != 64 {
		return "", fmt.Errorf("signature must be 64 bytes, got %d", len(signature))
	}

	// Parse amount
	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return "", fmt.Errorf("invalid amount: %s", data.Amount)
	}

	// Parse asset address
	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

	// Parse TxType
	txType, err := parseTxType(data.TxType)
	if err != nil {
		return "", fmt.Errorf("invalid tx type: %w", err)
	}

	// Determine target contract (gateway or vault)
	targetContract, err := tb.determineTargetContract(ctx, assetAddr)
	if err != nil {
		return "", fmt.Errorf("failed to determine target contract: %w", err)
	}

	// Determine if we're calling vault or gateway
	isVault := targetContract != tb.gatewayAddress

	// Determine function name based on TxType, asset type, and target contract
	funcName := tb.determineFunctionName(txType, assetAddr, isVault)

	// Encode function call
	txData, err := tb.encodeFunctionCall(funcName, data, amount, assetAddr, txType, isVault)
	if err != nil {
		return "", fmt.Errorf("failed to encode function call: %w", err)
	}

	// Determine transaction value
	// For native token transactions, the value must equal the amount
	var txValue *big.Int
	isNative := assetAddr == (ethcommon.Address{})
	if isNative && (txType == uetypes.TxType_FUNDS || txType == uetypes.TxType_FUNDS_AND_PAYLOAD || txType == uetypes.TxType_INBOUND_REVERT) {
		txValue = amount
	} else {
		txValue = big.NewInt(0)
	}

	// Parse gas limit from event data, use default if not provided
	var gasLimitForTx *big.Int
	if data.GasLimit == "" || data.GasLimit == "0" {
		gasLimitForTx = big.NewInt(DefaultGasLimit)
	} else {
		gasLimitForTx = new(big.Int)
		gasLimitForTx, ok = gasLimitForTx.SetString(data.GasLimit, 10)
		if !ok {
			return "", fmt.Errorf("invalid gas limit: %s", data.GasLimit)
		}
	}

	// Reconstruct the unsigned transaction from the request and event data
	tx := types.NewTransaction(
		req.Nonce,
		targetContract,
		txValue,
		gasLimitForTx.Uint64(),
		req.GasPrice,
		txData,
	)

	// Create EIP-155 signer
	signer := types.NewEIP155Signer(big.NewInt(tb.chainIDInt))
	txHash := signer.Hash(tx)

	// Auto-detect recovery ID by attempting to recover the public key
	var v byte
	found := false
	for testV := byte(0); testV < 4; testV++ {
		sigWithRecovery := make([]byte, 65)
		copy(sigWithRecovery[:64], signature)
		sigWithRecovery[64] = testV

		_, err := crypto.SigToPub(txHash.Bytes(), sigWithRecovery)
		if err == nil {
			v = testV
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("failed to determine recovery ID for signature")
	}

	// Construct the full signature with recovery ID
	sigWithRecovery := make([]byte, 65)
	copy(sigWithRecovery[:64], signature)
	sigWithRecovery[64] = v

	// Create signed transaction using WithSignature
	signedTx, err := tx.WithSignature(signer, sigWithRecovery)
	if err != nil {
		return "", fmt.Errorf("failed to apply signature: %w", err)
	}

	// Broadcast using RPC client
	txHashStr, err := tb.rpcClient.BroadcastTransaction(ctx, signedTx)
	if err != nil {
		return "", fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	tb.logger.Info().
		Str("tx_hash", txHashStr).
		Msg("transaction broadcast successfully")

	return txHashStr, nil
}

// Helper functions

func removeHexPrefix(s string) string {
	if len(s) >= 2 && s[0:2] == "0x" {
		return s[2:]
	}
	return s
}

// parseTxType parses the TxType string to uetypes.TxType enum
func parseTxType(txTypeStr string) (uetypes.TxType, error) {
	// Remove any whitespace and convert to uppercase
	txTypeStr = strings.TrimSpace(strings.ToUpper(txTypeStr))

	// Try to parse as enum name
	if val, ok := uetypes.TxType_value[txTypeStr]; ok {
		return uetypes.TxType(val), nil
	}

	// Try to parse as number
	if num, err := strconv.ParseInt(txTypeStr, 10, 32); err == nil {
		return uetypes.TxType(num), nil
	}

	return uetypes.TxType_UNSPECIFIED_TX, fmt.Errorf("unknown tx type: %s", txTypeStr)
}

// determineTargetContract determines whether to call gateway or vault contract
// Returns gateway address if assetAddr is zero, otherwise returns vault address
func (tb *TxBuilder) determineTargetContract(ctx context.Context, assetAddr ethcommon.Address) (ethcommon.Address, error) {
	// If asset address is zero, use gateway
	if assetAddr == (ethcommon.Address{}) {
		return tb.gatewayAddress, nil
	}

	// Otherwise, get vault address from gateway contract
	// Gateway contract has a public variable: address public vault;
	// We need to call the vault() getter function
	vaultAddr, err := tb.getVaultAddress(ctx)
	if err != nil {
		return ethcommon.Address{}, fmt.Errorf("failed to get vault address: %w", err)
	}

	return vaultAddr, nil
}

// getVaultAddress gets the vault address from the gateway contract (cached)
// Fetches it once and caches it for subsequent calls
// Gateway contract has: address public VAULT;
func (tb *TxBuilder) getVaultAddress(ctx context.Context) (ethcommon.Address, error) {
	// Check cache first
	tb.vaultMu.RLock()
	if tb.vaultAddress != nil {
		addr := *tb.vaultAddress
		tb.vaultMu.RUnlock()
		return addr, nil
	}
	tb.vaultMu.RUnlock()

	// Cache miss - fetch from contract
	tb.vaultMu.Lock()
	defer tb.vaultMu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have fetched it)
	if tb.vaultAddress != nil {
		return *tb.vaultAddress, nil
	}

	// Encode the function selector for VAULT() getter
	// VAULT() function selector: keccak256("VAULT()")[:4]
	vaultSelector := crypto.Keccak256([]byte("VAULT()"))[:4]

	// Call the contract
	result, err := tb.rpcClient.CallContract(ctx, tb.gatewayAddress, vaultSelector, nil)
	if err != nil {
		return ethcommon.Address{}, fmt.Errorf("failed to call VAULT() on gateway: %w", err)
	}

	// Decode the result (address is 32 bytes, but we only need the last 20)
	if len(result) < 32 {
		return ethcommon.Address{}, fmt.Errorf("invalid vault address result length: %d", len(result))
	}

	// Address is in the last 20 bytes of the 32-byte result
	var addr ethcommon.Address
	copy(addr[:], result[12:32])

	// Cache the address
	tb.vaultAddress = &addr

	tb.logger.Info().
		Str("vault_address", addr.Hex()).
		Msg("fetched and cached vault address from gateway")

	return addr, nil
}

// determineFunctionName determines the function name based on TxType and target contract
// Gateway functions (for native tokens):
// - withdraw - for native token withdrawal (no payload)
// - executeUniversalTx - for native tokens with payload
// - revertUniversalTx - for native token revert
// Vault functions (for ERC20 tokens):
// - withdraw - for ERC20 token withdrawal (no payload)
// - withdrawAndExecute - for ERC20 tokens with payload
// - revertWithdraw - for ERC20 token revert
func (tb *TxBuilder) determineFunctionName(txType uetypes.TxType, assetAddr ethcommon.Address, isVault bool) string {
	// Gateway is used for native tokens, vault is used for ERC20 tokens
	if isVault {
		// Vault functions (ERC20 only)
		switch txType {
		case uetypes.TxType_FUNDS:
			return "withdraw"
		case uetypes.TxType_FUNDS_AND_PAYLOAD:
			return "withdrawAndExecute"
		case uetypes.TxType_INBOUND_REVERT:
			return "revertWithdraw"
		default:
			// Vault doesn't handle payload-only, fallback to withdrawAndExecute
			return "withdrawAndExecute"
		}
	} else {
		// Gateway functions (native only)
		switch txType {
		case uetypes.TxType_FUNDS:
			return "withdraw"
		case uetypes.TxType_FUNDS_AND_PAYLOAD:
			return "executeUniversalTx"
		case uetypes.TxType_PAYLOAD:
			return "executeUniversalTx"
		case uetypes.TxType_INBOUND_REVERT:
			return "revertUniversalTx"
		default:
			return "executeUniversalTx"
		}
	}
}

// encodeFunctionCall encodes the function call with ABI encoding based on UniversalGateway and Vault contracts
func (tb *TxBuilder) encodeFunctionCall(
	funcName string,
	data *uetypes.OutboundCreatedEvent,
	amount *big.Int,
	assetAddr ethcommon.Address,
	txType uetypes.TxType,
	isVault bool,
) ([]byte, error) {
	// Parse common fields
	txIDBytes, err := hex.DecodeString(removeHexPrefix(data.TxID))
	if err != nil || len(txIDBytes) != 32 {
		return nil, fmt.Errorf("invalid txID: %s", data.TxID)
	}
	var txID [32]byte
	copy(txID[:], txIDBytes)

	universalTxIDBytes, err := hex.DecodeString(removeHexPrefix(data.UniversalTxId))
	if err != nil || len(universalTxIDBytes) != 32 {
		return nil, fmt.Errorf("invalid universalTxID: %s", data.UniversalTxId)
	}
	var universalTxID [32]byte
	copy(universalTxID[:], universalTxIDBytes)

	originCaller := ethcommon.HexToAddress(data.Sender)
	recipient := ethcommon.HexToAddress(data.Recipient)

	// Parse payload bytes
	payloadBytes, err := hex.DecodeString(removeHexPrefix(data.Payload))
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %w", err)
	}

	// Get function signature and selector
	isNative := assetAddr == (ethcommon.Address{})
	funcSignature := tb.getFunctionSignature(funcName, isNative)
	funcSelector := crypto.Keccak256([]byte(funcSignature))[:4]

	// Create ABI type definitions
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	addressType, _ := abi.NewType("address", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	bytesType, _ := abi.NewType("bytes", "", nil)

	var arguments abi.Arguments
	var values []interface{}

	switch funcName {
	case "withdraw":
		// Gateway (native): withdraw(bytes32 txID, bytes32 universalTxID, address originCaller, address to, uint256 amount)
		// Vault (ERC20): withdraw(bytes32 txID, bytes32 universalTxID, address originCaller, address token, address to, uint256 amount)
		if isNative {
			// Gateway withdraw (native)
			arguments = abi.Arguments{
				{Type: bytes32Type}, // txID
				{Type: bytes32Type}, // universalTxID
				{Type: addressType}, // originCaller
				{Type: addressType}, // to
				{Type: uint256Type}, // amount
			}
			values = []interface{}{txID, universalTxID, originCaller, recipient, amount}
		} else {
			// Vault withdraw (ERC20)
			arguments = abi.Arguments{
				{Type: bytes32Type}, // txID
				{Type: bytes32Type}, // universalTxID
				{Type: addressType}, // originCaller
				{Type: addressType}, // token
				{Type: addressType}, // to
				{Type: uint256Type}, // amount
			}
			values = []interface{}{txID, universalTxID, originCaller, assetAddr, recipient, amount}
		}

	case "withdrawAndExecute":
		// Vault: withdrawAndExecute(bytes32 txID, bytes32 universalTxID, address originCaller, address token, address target, uint256 amount, bytes calldata data)
		arguments = abi.Arguments{
			{Type: bytes32Type}, // txID
			{Type: bytes32Type}, // universalTxID
			{Type: addressType}, // originCaller
			{Type: addressType}, // token
			{Type: addressType}, // target
			{Type: uint256Type}, // amount
			{Type: bytesType},   // data
		}
		values = []interface{}{txID, universalTxID, originCaller, assetAddr, recipient, amount, payloadBytes}

	case "executeUniversalTx":
		if isNative {
			// executeUniversalTx(bytes32 txID, bytes32 universalTxID, address originCaller, address target, uint256 amount, bytes calldata payload)
			arguments = abi.Arguments{
				{Type: bytes32Type}, // txID
				{Type: bytes32Type}, // universalTxID
				{Type: addressType}, // originCaller
				{Type: addressType}, // target
				{Type: uint256Type}, // amount
				{Type: bytesType},   // payload
			}
			// For native executeUniversalTx, target is the recipient
			values = []interface{}{txID, universalTxID, originCaller, recipient, amount, payloadBytes}
		} else {
			// executeUniversalTx(bytes32 txID, bytes32 universalTxID, address originCaller, address token, address target, uint256 amount, bytes calldata payload)
			arguments = abi.Arguments{
				{Type: bytes32Type}, // txID
				{Type: bytes32Type}, // universalTxID
				{Type: addressType}, // originCaller
				{Type: addressType}, // token
				{Type: addressType}, // target
				{Type: uint256Type}, // amount
				{Type: bytesType},   // payload
			}
			// For ERC20 executeUniversalTx, target is the recipient
			values = []interface{}{txID, universalTxID, originCaller, assetAddr, recipient, amount, payloadBytes}
		}

	case "revertUniversalTx":
		// revertUniversalTx(bytes32 txID, bytes32 universalTxID, uint256 amount, RevertInstructions calldata revertInstruction)
		// RevertInstructions struct: { address revertRecipient; bytes revertMsg; }
		revertMsgBytes, err := hex.DecodeString(removeHexPrefix(data.RevertMsg))
		if err != nil {
			revertMsgBytes = []byte{} // Default to empty if decoding fails
		}
		revertInstructionType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "revertRecipient", Type: "address"},
			{Name: "revertMsg", Type: "bytes"},
		})
		arguments = abi.Arguments{
			{Type: bytes32Type},           // txID
			{Type: bytes32Type},           // universalTxID
			{Type: uint256Type},           // amount
			{Type: revertInstructionType}, // revertInstruction
		}
		values = []interface{}{txID, universalTxID, amount, RevertInstructions{
			RevertRecipient: recipient,
			RevertMsg:       revertMsgBytes,
		}}

	case "revertWithdraw":
		// Vault: revertWithdraw(bytes32 txID, bytes32 universalTxID, address token, uint256 amount, RevertInstructions calldata revertInstruction)
		// RevertInstructions struct: { address revertRecipient; bytes revertMsg; }
		revertMsgBytesWithdraw, err := hex.DecodeString(removeHexPrefix(data.RevertMsg))
		if err != nil {
			revertMsgBytesWithdraw = []byte{} // Default to empty if decoding fails
		}
		revertInstructionType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "revertRecipient", Type: "address"},
			{Name: "revertMsg", Type: "bytes"},
		})
		arguments = abi.Arguments{
			{Type: bytes32Type},           // txID
			{Type: bytes32Type},           // universalTxID
			{Type: addressType},           // token
			{Type: uint256Type},           // amount
			{Type: revertInstructionType}, // revertInstruction
		}
		values = []interface{}{txID, universalTxID, assetAddr, amount, RevertInstructions{
			RevertRecipient: recipient,
			RevertMsg:       revertMsgBytesWithdraw,
		}}

	default:
		return nil, fmt.Errorf("unknown function name: %s", funcName)
	}

	// Encode the arguments
	encodedArgs, err := arguments.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack arguments: %w", err)
	}

	// Combine function selector with encoded arguments
	txData := append(funcSelector, encodedArgs...)

	return txData, nil
}

// getFunctionSignature returns the full function signature for ABI encoding
// Based on UniversalGateway and Vault contract signatures
func (tb *TxBuilder) getFunctionSignature(funcName string, isNative bool) string {
	switch funcName {
	case "withdraw":
		// Gateway (native): withdraw(bytes32,bytes32,address,address,uint256)
		// Vault (ERC20): withdraw(bytes32,bytes32,address,address,address,uint256)
		if isNative {
			return "withdraw(bytes32,bytes32,address,address,uint256)"
		}
		return "withdraw(bytes32,bytes32,address,address,address,uint256)"
	case "withdrawAndExecute":
		// Vault: withdrawAndExecute(bytes32,bytes32,address,address,address,uint256,bytes)
		return "withdrawAndExecute(bytes32,bytes32,address,address,address,uint256,bytes)"
	case "executeUniversalTx":
		if isNative {
			return "executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)"
		}
		return "executeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"
	case "revertUniversalTx":
		return "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"
	case "revertWithdraw":
		// Vault: revertWithdraw(bytes32,bytes32,address,uint256,(address,bytes))
		return "revertWithdraw(bytes32,bytes32,address,uint256,(address,bytes))"
	default:
		return ""
	}
}
