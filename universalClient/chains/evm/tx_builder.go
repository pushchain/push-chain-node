package evm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// DefaultGasLimitV0 is used when gas limit is not provided in the outbound event data
const DefaultGasLimitV0 = 500000

// RevertInstructionsV0 represents the struct for revert instruction in UniversalGatewayV0 contract
// Matches: struct RevertInstructions { address fundRecipient; bytes revertMsg; }
type RevertInstructionsV0 struct {
	FundRecipient ethcommon.Address
	RevertMsg     []byte
}

// TxBuilder implements OutboundTxBuilder for EVM chains using UniversalGatewayV0 contract
type TxBuilder struct {
	rpcClient      *RPCClient
	chainID        string
	chainIDInt     int64
	gatewayAddress ethcommon.Address
	logger         zerolog.Logger
}

// NewTxBuilder creates a new EVM transaction builder for UniversalGatewayV0
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
	nonce uint64,
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

	// Parse gas limit, use default if not provided
	var gasLimit *big.Int
	if data.GasLimit == "" || data.GasLimit == "0" {
		gasLimit = big.NewInt(DefaultGasLimitV0)
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
	txType, err := parseTxTypeV0(data.TxType)
	if err != nil {
		return nil, fmt.Errorf("invalid tx type: %w", err)
	}

	// Determine function name based on TxType and asset type
	funcName := tb.determineFunctionName(txType, assetAddr)

	// Encode function call
	txData, err := tb.encodeFunctionCall(funcName, data, amount, assetAddr, txType)
	if err != nil {
		return nil, fmt.Errorf("failed to encode function call: %w", err)
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

	// Create unsigned transaction (always to gateway - no separate vault in V0)
	tx := types.NewTransaction(
		nonce,
		tb.gatewayAddress,
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
		Nonce:       nonce,
		GasPrice:    gasPrice,
	}, nil
}

// GetNextNonce returns the next nonce for the signer. useFinalized: if true, use nonce at latest block (aggressive, allows replacing stuck txs); if false, use pending.
func (tb *TxBuilder) GetNextNonce(ctx context.Context, signerAddress string, useFinalized bool) (uint64, error) {
	if signerAddress == "" {
		return 0, fmt.Errorf("signerAddress is required")
	}
	signerAddr := ethcommon.HexToAddress(signerAddress)
	if useFinalized {
		// nil blockNum means use latest block
		return tb.rpcClient.GetFinalizedNonce(ctx, signerAddr, nil)
	}
	return tb.rpcClient.GetPendingNonce(ctx, signerAddr)
}

// BroadcastOutboundSigningRequest assembles and broadcasts a signed transaction
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
	txType, err := parseTxTypeV0(data.TxType)
	if err != nil {
		return "", fmt.Errorf("invalid tx type: %w", err)
	}

	// Determine function name based on TxType and asset type
	funcName := tb.determineFunctionName(txType, assetAddr)

	// Encode function call
	txData, err := tb.encodeFunctionCall(funcName, data, amount, assetAddr, txType)
	if err != nil {
		return "", fmt.Errorf("failed to encode function call: %w", err)
	}

	// Determine transaction value
	var txValue *big.Int
	isNative := assetAddr == (ethcommon.Address{})
	if isNative && (txType == uetypes.TxType_FUNDS || txType == uetypes.TxType_FUNDS_AND_PAYLOAD || txType == uetypes.TxType_INBOUND_REVERT) {
		txValue = amount
	} else {
		txValue = big.NewInt(0)
	}

	// Parse gas limit from event data
	var gasLimitForTx *big.Int
	if data.GasLimit == "" || data.GasLimit == "0" {
		gasLimitForTx = big.NewInt(DefaultGasLimitV0)
	} else {
		gasLimitForTx = new(big.Int)
		gasLimitForTx, ok = gasLimitForTx.SetString(data.GasLimit, 10)
		if !ok {
			return "", fmt.Errorf("invalid gas limit: %s", data.GasLimit)
		}
	}

	// Reconstruct the unsigned transaction
	tx := types.NewTransaction(
		req.Nonce,
		tb.gatewayAddress,
		txValue,
		gasLimitForTx.Uint64(),
		req.GasPrice,
		txData,
	)

	// Create EIP-155 signer
	signer := types.NewEIP155Signer(big.NewInt(tb.chainIDInt))
	txHash := signer.Hash(tx)

	// Auto-detect recovery ID
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

	// Create signed transaction
	signedTx, err := tx.WithSignature(signer, sigWithRecovery)
	if err != nil {
		return "", fmt.Errorf("failed to apply signature: %w", err)
	}

	// Compute tx hash before broadcast so we can return it even on failure
	txHashStr := signedTx.Hash().Hex()

	// Broadcast using RPC client
	if _, err := tb.rpcClient.BroadcastTransaction(ctx, signedTx); err != nil {
		return txHashStr, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	tb.logger.Info().
		Str("tx_hash", txHashStr).
		Msg("transaction broadcast successfully")

	return txHashStr, nil
}

// removeHexPrefixV0 removes the 0x prefix from a hex string
func removeHexPrefixV0(s string) string {
	if len(s) >= 2 && s[0:2] == "0x" {
		return s[2:]
	}
	return s
}

// parseTxTypeV0 parses the TxType string to uetypes.TxType enum
func parseTxTypeV0(txTypeStr string) (uetypes.TxType, error) {
	txTypeStr = strings.TrimSpace(strings.ToUpper(txTypeStr))

	if val, ok := uetypes.TxType_value[txTypeStr]; ok {
		return uetypes.TxType(val), nil
	}

	if num, err := strconv.ParseInt(txTypeStr, 10, 32); err == nil {
		return uetypes.TxType(num), nil
	}

	return uetypes.TxType_UNSPECIFIED_TX, fmt.Errorf("unknown tx type: %s", txTypeStr)
}

// determineFunctionName determines the function name based on TxType and asset type
// UniversalGatewayV0 functions:
// - withdraw(bytes,bytes32,address,address,uint256) - native withdrawal
// - withdrawTokens(bytes,bytes32,address,address,address,uint256) - ERC20 withdrawal
// - executeUniversalTx(bytes32,address,address,uint256,bytes) - native with payload
// - executeUniversalTx(bytes32,address,address,address,uint256,bytes) - ERC20 with payload
// - revertUniversalTx(bytes32,uint256,(address,bytes)) - native revert
// - revertUniversalTxToken(bytes32,address,uint256,(address,bytes)) - ERC20 revert
func (tb *TxBuilder) determineFunctionName(txType uetypes.TxType, assetAddr ethcommon.Address) string {
	isNative := assetAddr == (ethcommon.Address{})

	switch txType {
	case uetypes.TxType_FUNDS:
		if isNative {
			return "withdraw"
		}
		return "withdrawTokens"

	case uetypes.TxType_FUNDS_AND_PAYLOAD, uetypes.TxType_PAYLOAD:
		return "executeUniversalTx"

	case uetypes.TxType_INBOUND_REVERT:
		if isNative {
			return "revertUniversalTx"
		}
		return "revertUniversalTxToken"

	default:
		return "executeUniversalTx"
	}
}

// encodeFunctionCall encodes the function call based on UniversalGatewayV0 contract
func (tb *TxBuilder) encodeFunctionCall(
	funcName string,
	data *uetypes.OutboundCreatedEvent,
	amount *big.Int,
	assetAddr ethcommon.Address,
	txType uetypes.TxType,
) ([]byte, error) {
	// Parse txID as bytes (not bytes32 for withdraw function)
	txIDBytes, err := hex.DecodeString(removeHexPrefixV0(data.TxID))
	if err != nil {
		return nil, fmt.Errorf("invalid txID: %s", data.TxID)
	}

	// Parse universalTxID as bytes32
	universalTxIDBytes, err := hex.DecodeString(removeHexPrefixV0(data.UniversalTxId))
	if err != nil || len(universalTxIDBytes) != 32 {
		return nil, fmt.Errorf("invalid universalTxID: %s", data.UniversalTxId)
	}
	var universalTxID [32]byte
	copy(universalTxID[:], universalTxIDBytes)

	// For executeUniversalTx and revert functions, txID is bytes32
	var txID32 [32]byte
	if len(txIDBytes) == 32 {
		copy(txID32[:], txIDBytes)
	} else if len(txIDBytes) > 0 {
		// Pad or truncate to 32 bytes
		copy(txID32[32-len(txIDBytes):], txIDBytes)
	}

	originCaller := ethcommon.HexToAddress(data.Sender)
	recipient := ethcommon.HexToAddress(data.Recipient)

	// Parse payload bytes
	payloadBytes, err := hex.DecodeString(removeHexPrefixV0(data.Payload))
	if err != nil {
		payloadBytes = []byte{}
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
		// withdraw(bytes32 txID, bytes32 universalTxID, address originCaller, address to, uint256 amount)
		arguments = abi.Arguments{
			{Type: bytes32Type}, // txID
			{Type: bytes32Type}, // universalTxID
			{Type: addressType}, // originCaller
			{Type: addressType}, // to
			{Type: uint256Type}, // amount
		}
		values = []interface{}{txID32, universalTxID, originCaller, recipient, amount}

	case "withdrawTokens":
		// withdrawTokens(bytes32 txID, bytes32 universalTxID, address originCaller, address token, address to, uint256 amount)
		arguments = abi.Arguments{
			{Type: bytes32Type}, // txID
			{Type: bytes32Type}, // universalTxID
			{Type: addressType}, // originCaller
			{Type: addressType}, // token
			{Type: addressType}, // to
			{Type: uint256Type}, // amount
		}
		values = []interface{}{txID32, universalTxID, originCaller, assetAddr, recipient, amount}

	case "executeUniversalTx":
		if isNative {
			// executeUniversalTx(bytes32 txID, bytes32 universalTxID, address originCaller, address target, uint256 amount, bytes payload)
			// Selector: 0x434cfde4
			arguments = abi.Arguments{
				{Type: bytes32Type}, // txID
				{Type: bytes32Type}, // universalTxID
				{Type: addressType}, // originCaller
				{Type: addressType}, // target
				{Type: uint256Type}, // amount
				{Type: bytesType},   // payload
			}
			values = []interface{}{txID32, universalTxID, originCaller, recipient, amount, payloadBytes}
		} else {
			// executeUniversalTx(bytes32 txID, bytes32 universalTxID, address originCaller, address token, address target, uint256 amount, bytes payload)
			// Selector: 0xc442a98e
			arguments = abi.Arguments{
				{Type: bytes32Type}, // txID
				{Type: bytes32Type}, // universalTxID
				{Type: addressType}, // originCaller
				{Type: addressType}, // token
				{Type: addressType}, // target
				{Type: uint256Type}, // amount
				{Type: bytesType},   // payload
			}
			values = []interface{}{txID32, universalTxID, originCaller, assetAddr, recipient, amount, payloadBytes}
		}

	case "revertUniversalTx":
		// revertUniversalTx(bytes32 txID, bytes32 universalTxID, uint256 amount, RevertInstructions revertInstruction)
		// Selector: 0x09e6d7cd
		revertMsgBytes, err := hex.DecodeString(removeHexPrefixV0(data.RevertMsg))
		if err != nil {
			revertMsgBytes = []byte{}
		}
		revertInstructionType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "fundRecipient", Type: "address"},
			{Name: "revertMsg", Type: "bytes"},
		})
		arguments = abi.Arguments{
			{Type: bytes32Type},           // txID
			{Type: bytes32Type},           // universalTxID
			{Type: uint256Type},           // amount
			{Type: revertInstructionType}, // revertInstruction
		}
		values = []interface{}{txID32, universalTxID, amount, RevertInstructionsV0{
			FundRecipient: recipient,
			RevertMsg:     revertMsgBytes,
		}}

	case "revertUniversalTxToken":
		// revertUniversalTxToken(bytes32 txID, bytes32 universalTxID, address token, uint256 amount, RevertInstructions revertInstruction)
		// Selector: 0x9fea040a
		revertMsgBytesToken, err := hex.DecodeString(removeHexPrefixV0(data.RevertMsg))
		if err != nil {
			revertMsgBytesToken = []byte{}
		}
		revertInstructionType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "fundRecipient", Type: "address"},
			{Name: "revertMsg", Type: "bytes"},
		})
		arguments = abi.Arguments{
			{Type: bytes32Type},           // txID
			{Type: bytes32Type},           // universalTxID
			{Type: addressType},           // token
			{Type: uint256Type},           // amount
			{Type: revertInstructionType}, // revertInstruction
		}
		values = []interface{}{txID32, universalTxID, assetAddr, amount, RevertInstructionsV0{
			FundRecipient: recipient,
			RevertMsg:     revertMsgBytesToken,
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

// VerifyBroadcastedTx checks the status of a broadcasted transaction on the EVM chain.
func (tb *TxBuilder) VerifyBroadcastedTx(ctx context.Context, txHash string) (found bool, confirmations uint64, status uint8, err error) {
	hash := ethcommon.HexToHash(txHash)
	receipt, err := tb.rpcClient.GetTransactionReceipt(ctx, hash)
	if err != nil {
		// Transaction not found or not yet mined
		return false, 0, 0, nil
	}

	// Calculate confirmations from current block
	var confs uint64
	latestBlock, err := tb.rpcClient.GetLatestBlock(ctx)
	if err == nil && latestBlock >= receipt.BlockNumber.Uint64() {
		confs = latestBlock - receipt.BlockNumber.Uint64() + 1
	}

	// receipt.Status: 1 = success, 0 = reverted
	return true, confs, uint8(receipt.Status), nil
}

// getFunctionSignature returns the full function signature for ABI encoding
// Based on UniversalGatewayV0 contract
func (tb *TxBuilder) getFunctionSignature(funcName string, isNative bool) string {
	switch funcName {
	case "withdraw":
		// withdraw(bytes32,bytes32,address,address,uint256)
		return "withdraw(bytes32,bytes32,address,address,uint256)"

	case "withdrawTokens":
		// withdrawTokens(bytes32,bytes32,address,address,address,uint256)
		return "withdrawTokens(bytes32,bytes32,address,address,address,uint256)"

	case "executeUniversalTx":
		if isNative {
			// executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes) - selector 0x434cfde4
			return "executeUniversalTx(bytes32,bytes32,address,address,uint256,bytes)"
		}
		// executeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes) - selector 0xc442a98e
		return "executeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"

	case "revertUniversalTx":
		// revertUniversalTx(bytes32,bytes32,uint256,(address,bytes)) - selector 0x09e6d7cd
		return "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"

	case "revertUniversalTxToken":
		// revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes)) - selector 0x9fea040a
		return "revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))"

	default:
		return ""
	}
}
