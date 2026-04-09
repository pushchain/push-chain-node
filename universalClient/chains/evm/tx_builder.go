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

// RevertInstructions represents the struct for revert instruction in contracts
// Matches: struct RevertInstructions { address revertRecipient; bytes revertMsg; }
type RevertInstructions struct {
	RevertRecipient ethcommon.Address
	RevertMsg       []byte
}

// TxBuilder implements OutboundTxBuilder for EVM chains using the Vault contract.
type TxBuilder struct {
	rpcClient      *RPCClient
	chainID        string
	chainIDInt     int64
	gatewayAddress ethcommon.Address
	vaultAddress   ethcommon.Address
	logger         zerolog.Logger
}

// NewTxBuilder creates a new EVM transaction builder for Vault + Gateway.
// The vault address is provided by the caller (fetched from the gateway by the client).
func NewTxBuilder(
	rpcClient *RPCClient,
	chainID string,
	chainIDInt int64,
	gatewayAddress string,
	vaultAddress ethcommon.Address,
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

	gwAddr := ethcommon.HexToAddress(gatewayAddress)
	if gwAddr == (ethcommon.Address{}) {
		return nil, fmt.Errorf("invalid gateway address: %s", gatewayAddress)
	}

	tb := &TxBuilder{
		rpcClient:      rpcClient,
		chainID:        chainID,
		chainIDInt:     chainIDInt,
		gatewayAddress: gwAddr,
		vaultAddress:   vaultAddress,
		logger:         logger.With().Str("component", "evm_tx_builder").Str("chain", chainID).Logger(),
	}

	tb.logger.Info().
		Str("vault", vaultAddress.Hex()).
		Str("gateway", gwAddr.Hex()).
		Msg("tx builder initialized")

	return tb, nil
}

// GetOutboundSigningRequest creates a signing request from outbound event data
func (tb *TxBuilder) GetOutboundSigningRequest(
	ctx context.Context,
	data *uetypes.OutboundCreatedEvent,
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

	gasPrice := new(big.Int)
	if data.GasPrice != "" {
		if _, ok := gasPrice.SetString(data.GasPrice, 10); !ok {
			return nil, fmt.Errorf("invalid gas price in event data: %s", data.GasPrice)
		}
	}
	if gasPrice.Sign() == 0 {
		return nil, fmt.Errorf("gas price is zero or missing in outbound event")
	}

	gasLimit, err := parseGasLimit(data.GasLimit)
	if err != nil {
		return nil, err
	}

	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", data.Amount)
	}

	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

	txType, err := parseTxType(data.TxType)
	if err != nil {
		return nil, fmt.Errorf("invalid tx type: %w", err)
	}

	funcName := tb.determineFunctionName(txType, assetAddr)

	txData, err := tb.encodeFunctionCall(funcName, data, amount, assetAddr, txType)
	if err != nil {
		return nil, fmt.Errorf("failed to encode function call: %w", err)
	}

	txValue := big.NewInt(0)
	if assetAddr == (ethcommon.Address{}) {
		txValue = amount
	}

	tx := types.NewTransaction(
		nonce,
		tb.vaultAddress,
		txValue,
		gasLimit.Uint64(),
		gasPrice,
		txData,
	)

	signer := types.NewEIP155Signer(big.NewInt(tb.chainIDInt))
	txHash := signer.Hash(tx).Bytes()

	return &common.UnSignedOutboundTxReq{
		SigningHash: txHash,
		Nonce:       nonce,
	}, nil
}

// GetNextNonce returns the next nonce for the signer.
func (tb *TxBuilder) GetNextNonce(ctx context.Context, signerAddress string, useFinalized bool) (uint64, error) {
	if signerAddress == "" {
		return 0, fmt.Errorf("signerAddress is required")
	}
	signerAddr := ethcommon.HexToAddress(signerAddress)
	if useFinalized {
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
	if len(signature) != 65 {
		return "", fmt.Errorf("signature must be 65 bytes [r(32)|s(32)|v(1)], got %d", len(signature))
	}

	amount := new(big.Int)
	amount, ok := amount.SetString(data.Amount, 10)
	if !ok {
		return "", fmt.Errorf("invalid amount: %s", data.Amount)
	}

	assetAddr := ethcommon.HexToAddress(data.AssetAddr)

	txType, err := parseTxType(data.TxType)
	if err != nil {
		return "", fmt.Errorf("invalid tx type: %w", err)
	}

	funcName := tb.determineFunctionName(txType, assetAddr)

	txData, err := tb.encodeFunctionCall(funcName, data, amount, assetAddr, txType)
	if err != nil {
		return "", fmt.Errorf("failed to encode function call: %w", err)
	}

	txValue := big.NewInt(0)
	if assetAddr == (ethcommon.Address{}) {
		txValue = amount
	}

	gasLimitForTx, err := parseGasLimit(data.GasLimit)
	if err != nil {
		return "", fmt.Errorf("invalid gas limit: %w", err)
	}

	gasPrice := new(big.Int)
	if data.GasPrice != "" {
		gasPrice.SetString(data.GasPrice, 10)
	}

	tx := types.NewTransaction(
		req.Nonce,
		tb.vaultAddress,
		txValue,
		gasLimitForTx.Uint64(),
		gasPrice,
		txData,
	)

	signer := types.NewEIP155Signer(big.NewInt(tb.chainIDInt))

	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		return "", fmt.Errorf("failed to apply signature: %w", err)
	}

	txHashStr := signedTx.Hash().Hex()

	// Recover and log sender address from the signed tx for diagnostics
	senderAddr, senderErr := signer.Sender(signedTx)
	senderStr := "(unknown)"
	if senderErr == nil {
		senderStr = senderAddr.Hex()
	}

	tb.logger.Info().
		Str("tx_hash", txHashStr).
		Str("sender", senderStr).
		Str("to", tb.vaultAddress.Hex()).
		Str("chain", tb.chainID).
		Uint64("nonce", req.Nonce).
		Str("gas_price", gasPrice.String()).
		Uint64("gas_limit", gasLimitForTx.Uint64()).
		Str("value", txValue.String()).
		Str("func", funcName).
		Msg("submitting vault tx to EVM chain")

	if _, err := tb.rpcClient.BroadcastTransaction(ctx, signedTx); err != nil {
		tb.logger.Warn().
			Err(err).
			Str("sender", senderStr).
			Str("to", tb.vaultAddress.Hex()).
			Str("chain", tb.chainID).
			Uint64("nonce", req.Nonce).
			Msg("BroadcastTransaction failed")
		return txHashStr, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	tb.logger.Info().
		Str("tx_hash", txHashStr).
		Str("sender", senderStr).
		Str("to", tb.vaultAddress.Hex()).
		Msg("transaction broadcast successfully")

	return txHashStr, nil
}

// VerifyBroadcastedTx checks the status of a broadcasted transaction on the EVM chain.
func (tb *TxBuilder) VerifyBroadcastedTx(ctx context.Context, txHash string) (found bool, blockHeight uint64, confirmations uint64, status uint8, err error) {
	hash := ethcommon.HexToHash(txHash)
	receipt, err := tb.rpcClient.GetTransactionReceipt(ctx, hash)
	if err != nil {
		return false, 0, 0, 0, nil
	}

	receiptBlock := receipt.BlockNumber.Uint64()

	var confs uint64
	latestBlock, err := tb.rpcClient.GetLatestBlock(ctx)
	if err == nil && latestBlock >= receiptBlock {
		confs = latestBlock - receiptBlock + 1
	}

	return true, receiptBlock, confs, uint8(receipt.Status), nil
}

// determineFunctionName determines the Vault function name based on TxType.
//
// Routing (all on Vault):
//   - FUNDS, FUNDS_AND_PAYLOAD, PAYLOAD → Vault.finalizeUniversalTx
//   - INBOUND_REVERT                    → Vault.revertUniversalTx
//   - RESCUE_FUNDS                      → Vault.rescueFunds
func (tb *TxBuilder) determineFunctionName(txType uetypes.TxType, assetAddr ethcommon.Address) string {
	switch txType {
	case uetypes.TxType_FUNDS, uetypes.TxType_FUNDS_AND_PAYLOAD, uetypes.TxType_PAYLOAD:
		return "finalizeUniversalTx"

	case uetypes.TxType_INBOUND_REVERT:
		return "revertUniversalTx"

	case uetypes.TxType_RESCUE_FUNDS:
		return "rescueFunds"

	default:
		return "finalizeUniversalTx"
	}
}

// encodeFunctionCall encodes the function call based on contract ABIs
func (tb *TxBuilder) encodeFunctionCall(
	funcName string,
	data *uetypes.OutboundCreatedEvent,
	amount *big.Int,
	assetAddr ethcommon.Address,
	_ uetypes.TxType,
) ([]byte, error) {
	txIDBytes, err := hex.DecodeString(removeHexPrefix(data.TxID))
	if err != nil {
		return nil, fmt.Errorf("invalid txID: %s", data.TxID)
	}

	universalTxIDBytes, err := hex.DecodeString(removeHexPrefix(data.UniversalTxId))
	if err != nil || len(universalTxIDBytes) != 32 {
		return nil, fmt.Errorf("invalid universalTxID: %s", data.UniversalTxId)
	}
	var universalTxID [32]byte
	copy(universalTxID[:], universalTxIDBytes)

	var txID32 [32]byte
	if len(txIDBytes) == 32 {
		copy(txID32[:], txIDBytes)
	} else if len(txIDBytes) > 0 {
		copy(txID32[32-len(txIDBytes):], txIDBytes)
	}

	pushAccount := ethcommon.HexToAddress(data.Sender)
	target := ethcommon.HexToAddress(data.Recipient)

	payloadBytes, err := hex.DecodeString(removeHexPrefix(data.Payload))
	if err != nil {
		payloadBytes = []byte{}
	}

	isNative := assetAddr == (ethcommon.Address{})
	funcSignature := tb.getFunctionSignature(funcName, isNative)
	funcSelector := crypto.Keccak256([]byte(funcSignature))[:4]

	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	addressType, _ := abi.NewType("address", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	bytesType, _ := abi.NewType("bytes", "", nil)

	var arguments abi.Arguments
	var values []interface{}

	switch funcName {
	case "finalizeUniversalTx":
		// Vault: finalizeUniversalTx(bytes32 subTxId, bytes32 universalTxId, address pushAccount, address recipient, address token, uint256 amount, bytes data)
		arguments = abi.Arguments{
			{Type: bytes32Type}, // subTxId
			{Type: bytes32Type}, // universalTxId
			{Type: addressType}, // pushAccount
			{Type: addressType}, // recipient
			{Type: addressType}, // token
			{Type: uint256Type}, // amount
			{Type: bytesType},   // data
		}
		values = []interface{}{txID32, universalTxID, pushAccount, target, assetAddr, amount, payloadBytes}

	case "revertUniversalTx", "rescueFunds":
		// Vault: revertUniversalTx(bytes32 subTxId, bytes32 universalTxId, address token, uint256 amount, RevertInstructions revertInstruction)
		// Vault: rescueFunds(bytes32 subTxId, bytes32 universalTxId, address token, uint256 amount, RevertInstructions revertInstruction)
		revertMsgBytes, err := hex.DecodeString(removeHexPrefix(data.RevertMsg))
		if err != nil {
			revertMsgBytes = []byte{}
		}
		revertInstructionType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "revertRecipient", Type: "address"},
			{Name: "revertMsg", Type: "bytes"},
		})
		arguments = abi.Arguments{
			{Type: bytes32Type},           // subTxId
			{Type: bytes32Type},           // universalTxId
			{Type: addressType},           // token
			{Type: uint256Type},           // amount
			{Type: revertInstructionType}, // revertInstruction
		}
		values = []interface{}{txID32, universalTxID, assetAddr, amount, RevertInstructions{
			RevertRecipient: target,
			RevertMsg:       revertMsgBytes,
		}}

	default:
		return nil, fmt.Errorf("unknown function name: %s", funcName)
	}

	encodedArgs, err := arguments.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack arguments: %w", err)
	}

	txData := append(funcSelector, encodedArgs...)

	return txData, nil
}

// getFunctionSignature returns the full function signature for ABI encoding
func (tb *TxBuilder) getFunctionSignature(funcName string, _ bool) string {
	switch funcName {
	case "finalizeUniversalTx":
		// Vault: finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)
		return "finalizeUniversalTx(bytes32,bytes32,address,address,address,uint256,bytes)"

	case "revertUniversalTx":
		// Vault: revertUniversalTx(bytes32,bytes32,address,uint256,(address,bytes))
		return "revertUniversalTx(bytes32,bytes32,address,uint256,(address,bytes))"

	case "rescueFunds":
		// Vault: rescueFunds(bytes32,bytes32,address,uint256,(address,bytes))
		return "rescueFunds(bytes32,bytes32,address,uint256,(address,bytes))"

	default:
		return ""
	}
}

// removeHexPrefix removes the 0x prefix from a hex string
func removeHexPrefix(s string) string {
	if len(s) >= 2 && s[0:2] == "0x" {
		return s[2:]
	}
	return s
}

// parseTxType parses the TxType string to uetypes.TxType enum
func parseTxType(txTypeStr string) (uetypes.TxType, error) {
	txTypeStr = strings.TrimSpace(strings.ToUpper(txTypeStr))

	if val, ok := uetypes.TxType_value[txTypeStr]; ok {
		return uetypes.TxType(val), nil
	}

	if num, err := strconv.ParseInt(txTypeStr, 10, 32); err == nil {
		return uetypes.TxType(num), nil
	}

	return uetypes.TxType_UNSPECIFIED_TX, fmt.Errorf("unknown tx type: %s", txTypeStr)
}

// parseGasLimit parses gas limit string, returning an error if empty or zero.
func parseGasLimit(gasLimitStr string) (*big.Int, error) {
	if gasLimitStr == "" || gasLimitStr == "0" {
		return nil, fmt.Errorf("gas limit is required in outbound event")
	}
	gasLimit := new(big.Int)
	gasLimit, ok := gasLimit.SetString(gasLimitStr, 10)
	if !ok {
		return nil, fmt.Errorf("invalid gas limit: %s", gasLimitStr)
	}
	return gasLimit, nil
}

// IsAlreadyExecuted returns false for EVM. EVM uses nonce-based replay protection,
// checked via GetNextNonce in the broadcaster.
func (tb *TxBuilder) IsAlreadyExecuted(ctx context.Context, txID string) (bool, error) {
	return false, nil
}

// GetGasFeeUsed returns the gas fee used by a transaction on the EVM chain.
// Fetches the receipt for gasUsed and the transaction for gasPrice, then returns
// gasUsed * gasPrice as a decimal string. Returns "0" if not found.
func (tb *TxBuilder) GetGasFeeUsed(ctx context.Context, txHash string) (string, error) {
	hash := ethcommon.HexToHash(txHash)
	receipt, err := tb.rpcClient.GetTransactionReceipt(ctx, hash)
	if err != nil {
		return "0", nil
	}

	tx, _, err := tb.rpcClient.GetTransactionByHash(ctx, hash)
	if err != nil {
		return "0", nil
	}

	gasUsed := new(big.Int).SetUint64(receipt.GasUsed)
	gasPrice := tx.GasPrice()
	if gasPrice == nil || gasPrice.Sign() == 0 {
		return "0", nil
	}

	gasFeeUsed := new(big.Int).Mul(gasUsed, gasPrice)
	return gasFeeUsed.String(), nil
}
