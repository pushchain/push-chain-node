package evm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

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

// vaultRefreshInterval is how often we re-fetch the vault address from the gateway
const vaultRefreshInterval = 10 * time.Minute

// vaultCallSelector is the 4-byte selector for VAULT() public getter
// keccak256("VAULT()") = 0x71e99dc2...
var vaultCallSelector = crypto.Keccak256([]byte("VAULT()"))[:4]

// RevertInstructions represents the struct for revert instruction in contracts
// Matches: struct RevertInstructions { address revertRecipient; bytes revertMsg; }
type RevertInstructions struct {
	RevertRecipient ethcommon.Address
	RevertMsg       []byte
}

// TxBuilder implements OutboundTxBuilder for EVM chains using Vault + Gateway contracts.
// The vault address is fetched from the gateway's VAULT() public variable and refreshed
// periodically so that vault upgrades are picked up automatically.
//
// Routing:
//   - FUNDS, FUNDS_AND_PAYLOAD, PAYLOAD → Vault.finalizeUniversalTx
//   - INBOUND_REVERT (native)           → Gateway.revertUniversalTx
//   - INBOUND_REVERT (ERC20)            → Vault.revertUniversalTxToken
type TxBuilder struct {
	rpcClient      *RPCClient
	chainID        string
	chainIDInt     int64
	gatewayAddress ethcommon.Address
	logger         zerolog.Logger

	// vault address cache
	vaultMu        sync.RWMutex
	vaultAddress   ethcommon.Address
	vaultFetchedAt time.Time
}

// NewTxBuilder creates a new EVM transaction builder for Vault + Gateway.
// It attempts to fetch the vault address from the gateway contract's VAULT() public variable.
// If the initial fetch fails, the builder is still created — individual requests will fail
// until the vault address is successfully fetched on the next refresh attempt.
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

	gwAddr := ethcommon.HexToAddress(gatewayAddress)
	if gwAddr == (ethcommon.Address{}) {
		return nil, fmt.Errorf("invalid gateway address: %s", gatewayAddress)
	}

	tb := &TxBuilder{
		rpcClient:      rpcClient,
		chainID:        chainID,
		chainIDInt:     chainIDInt,
		gatewayAddress: gwAddr,
		logger:         logger.With().Str("component", "evm_tx_builder").Str("chain", chainID).Logger(),
	}

	// Best-effort fetch of vault address from gateway at startup
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	vaultAddr, err := tb.fetchVaultAddress(ctx)
	if err != nil {
		tb.logger.Warn().Err(err).
			Str("gateway", gwAddr.Hex()).
			Msg("failed to fetch vault address from gateway on init, will retry on first request")
	} else {
		tb.vaultAddress = vaultAddr
		tb.vaultFetchedAt = time.Now()
		tb.logger.Info().
			Str("vault", vaultAddr.Hex()).
			Str("gateway", gwAddr.Hex()).
			Msg("vault address fetched from gateway")
	}

	return tb, nil
}

// getVaultAddress returns the cached vault address, refreshing in the background if stale.
// Returns an error if the vault address has never been successfully fetched.
// If stale, triggers an async refresh but returns the last known good address.
func (tb *TxBuilder) getVaultAddress() (ethcommon.Address, error) {
	tb.vaultMu.RLock()
	addr := tb.vaultAddress
	fetchedAt := tb.vaultFetchedAt
	tb.vaultMu.RUnlock()

	// Never fetched successfully
	if addr == (ethcommon.Address{}) {
		// Try a synchronous fetch before failing
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		newAddr, err := tb.fetchVaultAddress(ctx)
		if err != nil {
			return ethcommon.Address{}, fmt.Errorf("vault address not available: %w", err)
		}

		tb.vaultMu.Lock()
		tb.vaultAddress = newAddr
		tb.vaultFetchedAt = time.Now()
		tb.vaultMu.Unlock()

		tb.logger.Info().Str("vault", newAddr.Hex()).Msg("vault address fetched from gateway (deferred)")
		return newAddr, nil
	}

	// Stale — refresh in background, return current
	if time.Since(fetchedAt) > vaultRefreshInterval {
		go tb.tryRefreshVaultAddress()
	}

	return addr, nil
}

// tryRefreshVaultAddress attempts to refresh the vault address from the gateway.
// On failure, the stale address continues to be used.
func (tb *TxBuilder) tryRefreshVaultAddress() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	newAddr, err := tb.fetchVaultAddress(ctx)
	if err != nil {
		tb.logger.Warn().Err(err).Msg("failed to refresh vault address from gateway, using stale")
		return
	}

	tb.vaultMu.Lock()
	oldAddr := tb.vaultAddress
	tb.vaultAddress = newAddr
	tb.vaultFetchedAt = time.Now()
	tb.vaultMu.Unlock()

	if oldAddr != newAddr {
		tb.logger.Info().
			Str("old_vault", oldAddr.Hex()).
			Str("new_vault", newAddr.Hex()).
			Msg("vault address updated from gateway")
	}
}

// fetchVaultAddress calls the gateway's VAULT() public getter to retrieve the vault address.
func (tb *TxBuilder) fetchVaultAddress(ctx context.Context) (ethcommon.Address, error) {
	result, err := tb.rpcClient.CallContract(ctx, tb.gatewayAddress, vaultCallSelector, nil)
	if err != nil {
		return ethcommon.Address{}, fmt.Errorf("VAULT() call failed: %w", err)
	}

	if len(result) < 32 {
		return ethcommon.Address{}, fmt.Errorf("VAULT() returned invalid data (len=%d)", len(result))
	}

	addr := ethcommon.BytesToAddress(result[12:32])
	if addr == (ethcommon.Address{}) {
		return ethcommon.Address{}, fmt.Errorf("VAULT() returned zero address")
	}

	return addr, nil
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

	gasLimit := parseGasLimit(data.GasLimit)

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

	txValue, toAddress, err := tb.resolveTxParams(funcName, assetAddr, amount)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tx params: %w", err)
	}

	tx := types.NewTransaction(
		nonce,
		toAddress,
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
		GasPrice:    gasPrice,
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

	txValue, toAddress, err := tb.resolveTxParams(funcName, assetAddr, amount)
	if err != nil {
		return "", fmt.Errorf("failed to resolve tx params: %w", err)
	}

	gasLimitForTx := parseGasLimit(data.GasLimit)

	tx := types.NewTransaction(
		req.Nonce,
		toAddress,
		txValue,
		gasLimitForTx.Uint64(),
		req.GasPrice,
		txData,
	)

	signer := types.NewEIP155Signer(big.NewInt(tb.chainIDInt))

	signedTx, err := tx.WithSignature(signer, signature)
	if err != nil {
		return "", fmt.Errorf("failed to apply signature: %w", err)
	}

	txHashStr := signedTx.Hash().Hex()

	if _, err := tb.rpcClient.BroadcastTransaction(ctx, signedTx); err != nil {
		return txHashStr, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	tb.logger.Info().
		Str("tx_hash", txHashStr).
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

// resolveTxParams determines the transaction value and destination address.
//
// Routing:
//   - finalizeUniversalTx → vault (value = amount for native)
//   - revertUniversalTxToken → vault (value = 0, ERC20 only)
//   - revertUniversalTx → gateway (value = amount, native only)
func (tb *TxBuilder) resolveTxParams(funcName string, assetAddr ethcommon.Address, amount *big.Int) (txValue *big.Int, toAddress ethcommon.Address, err error) {
	isNative := assetAddr == (ethcommon.Address{})

	switch funcName {
	case "revertUniversalTx":
		// Native revert goes to gateway — no vault needed
		return amount, tb.gatewayAddress, nil

	case "finalizeUniversalTx":
		vaultAddr, err := tb.getVaultAddress()
		if err != nil {
			return nil, ethcommon.Address{}, fmt.Errorf("cannot route to vault: %w", err)
		}
		if isNative {
			return amount, vaultAddr, nil
		}
		return big.NewInt(0), vaultAddr, nil

	case "revertUniversalTxToken":
		vaultAddr, err := tb.getVaultAddress()
		if err != nil {
			return nil, ethcommon.Address{}, fmt.Errorf("cannot route to vault: %w", err)
		}
		return big.NewInt(0), vaultAddr, nil

	default:
		vaultAddr, err := tb.getVaultAddress()
		if err != nil {
			return nil, ethcommon.Address{}, fmt.Errorf("cannot route to vault: %w", err)
		}
		return big.NewInt(0), vaultAddr, nil
	}
}

// determineFunctionName determines the function name based on TxType and asset type.
//
// Routing:
//   - FUNDS, FUNDS_AND_PAYLOAD, PAYLOAD → Vault.finalizeUniversalTx
//   - INBOUND_REVERT (native)           → Gateway.revertUniversalTx
//   - INBOUND_REVERT (ERC20)            → Vault.revertUniversalTxToken
func (tb *TxBuilder) determineFunctionName(txType uetypes.TxType, assetAddr ethcommon.Address) string {
	isNative := assetAddr == (ethcommon.Address{})

	switch txType {
	case uetypes.TxType_FUNDS, uetypes.TxType_FUNDS_AND_PAYLOAD, uetypes.TxType_PAYLOAD:
		return "finalizeUniversalTx"

	case uetypes.TxType_INBOUND_REVERT:
		if isNative {
			return "revertUniversalTx"
		}
		return "revertUniversalTxToken"

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
		// finalizeUniversalTx(bytes32 txId, bytes32 universalTxId, address pushAccount, address token, address target, uint256 amount, bytes data)
		arguments = abi.Arguments{
			{Type: bytes32Type}, // txId
			{Type: bytes32Type}, // universalTxId
			{Type: addressType}, // pushAccount
			{Type: addressType}, // token
			{Type: addressType}, // target
			{Type: uint256Type}, // amount
			{Type: bytesType},   // data
		}
		values = []interface{}{txID32, universalTxID, pushAccount, assetAddr, target, amount, payloadBytes}

	case "revertUniversalTx":
		// Gateway: revertUniversalTx(bytes32 txId, bytes32 universalTxId, uint256 amount, RevertInstructions revertInstruction)
		revertMsgBytes, err := hex.DecodeString(removeHexPrefix(data.RevertMsg))
		if err != nil {
			revertMsgBytes = []byte{}
		}
		revertInstructionType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "revertRecipient", Type: "address"},
			{Name: "revertMsg", Type: "bytes"},
		})
		arguments = abi.Arguments{
			{Type: bytes32Type},           // txId
			{Type: bytes32Type},           // universalTxId
			{Type: uint256Type},           // amount
			{Type: revertInstructionType}, // revertInstruction
		}
		values = []interface{}{txID32, universalTxID, amount, RevertInstructions{
			RevertRecipient: target,
			RevertMsg:       revertMsgBytes,
		}}

	case "revertUniversalTxToken":
		// Vault: revertUniversalTxToken(bytes32 txId, bytes32 universalTxId, address token, uint256 amount, RevertInstructions revertInstruction)
		revertMsgBytesToken, err := hex.DecodeString(removeHexPrefix(data.RevertMsg))
		if err != nil {
			revertMsgBytesToken = []byte{}
		}
		revertInstructionType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
			{Name: "revertRecipient", Type: "address"},
			{Name: "revertMsg", Type: "bytes"},
		})
		arguments = abi.Arguments{
			{Type: bytes32Type},           // txId
			{Type: bytes32Type},           // universalTxId
			{Type: addressType},           // token
			{Type: uint256Type},           // amount
			{Type: revertInstructionType}, // revertInstruction
		}
		values = []interface{}{txID32, universalTxID, assetAddr, amount, RevertInstructions{
			RevertRecipient: target,
			RevertMsg:       revertMsgBytesToken,
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
		// Gateway: revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))
		return "revertUniversalTx(bytes32,bytes32,uint256,(address,bytes))"

	case "revertUniversalTxToken":
		// Vault: revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))
		return "revertUniversalTxToken(bytes32,bytes32,address,uint256,(address,bytes))"

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

// parseGasLimit parses gas limit string, returning default if empty or zero
func parseGasLimit(gasLimitStr string) *big.Int {
	if gasLimitStr == "" || gasLimitStr == "0" {
		return big.NewInt(DefaultGasLimit)
	}
	gasLimit := new(big.Int)
	gasLimit, ok := gasLimit.SetString(gasLimitStr, 10)
	if !ok {
		return big.NewInt(DefaultGasLimit)
	}
	return gasLimit
}
