package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/rs/zerolog"

	chaincommon "github.com/pushchain/push-chain-node/universalClient/chains/common"
)

// OutboundTxBuilder builds outbound transactions for EVM chains.
type OutboundTxBuilder struct {
	client       *Client
	chainID      *big.Int
	caipChainID  string
	gatewayAddr  common.Address
	tssPublicKey *ecdsa.PublicKey // TSS public key for deriving sender address
	logger       zerolog.Logger
}

// NewOutboundTxBuilder creates a new EVM outbound transaction builder.
func NewOutboundTxBuilder(
	client *Client,
	gatewayAddr common.Address,
	tssPublicKey *ecdsa.PublicKey,
	logger zerolog.Logger,
) *OutboundTxBuilder {
	return &OutboundTxBuilder{
		client:       client,
		chainID:      big.NewInt(client.chainID),
		caipChainID:  client.GetConfig().Chain,
		gatewayAddr:  gatewayAddr,
		tssPublicKey: tssPublicKey,
		logger:       logger.With().Str("component", "evm_outbound_builder").Logger(),
	}
}

// BuildTransaction creates an unsigned EVM transaction from outbound data.
// gasPrice is provided by the caller (from pushcore oracle).
func (b *OutboundTxBuilder) BuildTransaction(ctx context.Context, data *chaincommon.OutboundTxData, gasPrice *big.Int) (*chaincommon.OutboundTxResult, error) {
	if data == nil {
		return nil, fmt.Errorf("outbound data is nil")
	}
	if gasPrice == nil {
		return nil, fmt.Errorf("gas price is nil")
	}

	b.logger.Debug().
		Str("tx_id", data.TxID).
		Str("recipient", data.Recipient).
		Str("amount", data.Amount).
		Str("gas_price", gasPrice.String()).
		Msg("building EVM outbound transaction")

	// Parse amount
	amount, ok := new(big.Int).SetString(data.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", data.Amount)
	}

	// Parse gas limit
	gasLimit, ok := new(big.Int).SetString(data.GasLimit, 10)
	if !ok {
		gasLimit = big.NewInt(21000) // Default gas limit
	}

	// Get nonce for TSS address
	tssAddr := b.getTSSAddress()
	nonce, err := b.getNonce(ctx, tssAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %w", err)
	}

	// Build transaction data (call to gateway contract)
	txData, err := b.buildGatewayCallData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to build gateway call data: %w", err)
	}

	// Create the transaction
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit.Uint64(),
		To:       &b.gatewayAddr,
		Value:    amount,
		Data:     txData,
	})

	// Get the signer
	signer := types.NewEIP155Signer(b.chainID)

	// Get the signing hash
	signingHash := signer.Hash(tx)

	// Serialize the unsigned transaction
	rawTx, err := rlp.EncodeToBytes(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %w", err)
	}

	return &chaincommon.OutboundTxResult{
		RawTx:       rawTx,
		SigningHash: signingHash[:],
		Nonce:       nonce,
		GasPrice:    gasPrice,
		GasLimit:    gasLimit.Uint64(),
		ChainID:     b.caipChainID,
	}, nil
}

// AssembleSignedTransaction combines the unsigned transaction with the TSS signature.
func (b *OutboundTxBuilder) AssembleSignedTransaction(unsignedTx []byte, signature []byte, recoveryID byte) ([]byte, error) {
	if len(signature) != 64 {
		return nil, fmt.Errorf("invalid signature length: expected 64, got %d", len(signature))
	}

	// Decode the unsigned transaction
	var tx types.Transaction
	if err := rlp.DecodeBytes(unsignedTx, &tx); err != nil {
		return nil, fmt.Errorf("failed to decode unsigned transaction: %w", err)
	}

	// Create the signature with recovery ID
	// EIP-155: V = chainID * 2 + 35 + recoveryID
	v := new(big.Int).Mul(b.chainID, big.NewInt(2))
	v.Add(v, big.NewInt(35))
	v.Add(v, big.NewInt(int64(recoveryID)))

	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])

	// Create signed transaction
	signer := types.NewEIP155Signer(b.chainID)
	signedTx, err := tx.WithSignature(signer, append(append(r.Bytes(), s.Bytes()...), v.Bytes()...))
	if err != nil {
		return nil, fmt.Errorf("failed to add signature to transaction: %w", err)
	}

	// Serialize the signed transaction
	signedTxBytes, err := rlp.EncodeToBytes(signedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode signed transaction: %w", err)
	}

	return signedTxBytes, nil
}

// BroadcastTransaction sends the signed transaction to the network.
func (b *OutboundTxBuilder) BroadcastTransaction(ctx context.Context, signedTx []byte) (string, error) {
	// Decode the signed transaction
	var tx types.Transaction
	if err := rlp.DecodeBytes(signedTx, &tx); err != nil {
		return "", fmt.Errorf("failed to decode signed transaction: %w", err)
	}

	// Send the transaction
	var txHash string
	err := b.client.executeWithFailover(ctx, "broadcast_tx", func(client *ethclient.Client) error {
		if err := client.SendTransaction(ctx, &tx); err != nil {
			return err
		}
		txHash = tx.Hash().Hex()
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	b.logger.Info().
		Str("tx_hash", txHash).
		Msg("outbound transaction broadcasted")

	return txHash, nil
}

// GetChainID returns the chain identifier.
func (b *OutboundTxBuilder) GetChainID() string {
	return b.caipChainID
}

// getTSSAddress derives the TSS address from the public key.
func (b *OutboundTxBuilder) getTSSAddress() common.Address {
	if b.tssPublicKey == nil {
		return common.Address{}
	}
	return crypto.PubkeyToAddress(*b.tssPublicKey)
}

// getNonce gets the current nonce for an address.
func (b *OutboundTxBuilder) getNonce(ctx context.Context, addr common.Address) (uint64, error) {
	var nonce uint64
	err := b.client.executeWithFailover(ctx, "get_nonce", func(client *ethclient.Client) error {
		var innerErr error
		nonce, innerErr = client.PendingNonceAt(ctx, addr)
		return innerErr
	})
	return nonce, err
}

// buildGatewayCallData builds the call data for the gateway contract.
// This encodes the function call to execute the outbound transaction.
func (b *OutboundTxBuilder) buildGatewayCallData(data *chaincommon.OutboundTxData) ([]byte, error) {
	// Parse recipient address
	if !common.IsHexAddress(data.Recipient) {
		return nil, fmt.Errorf("invalid recipient address: %s", data.Recipient)
	}
	recipient := common.HexToAddress(data.Recipient)

	// Parse amount
	amount, ok := new(big.Int).SetString(data.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", data.Amount)
	}

	// Parse asset address
	var assetAddr common.Address
	if data.AssetAddr != "" && data.AssetAddr != "0x" {
		if !common.IsHexAddress(data.AssetAddr) {
			return nil, fmt.Errorf("invalid asset address: %s", data.AssetAddr)
		}
		assetAddr = common.HexToAddress(data.AssetAddr)
	}

	// Parse payload
	var payload []byte
	if data.Payload != "" && data.Payload != "0x" {
		payloadHex := strings.TrimPrefix(data.Payload, "0x")
		var err error
		payload, err = hex.DecodeString(payloadHex)
		if err != nil {
			return nil, fmt.Errorf("invalid payload hex: %w", err)
		}
	}

	// Build the ABI-encoded call data
	// Function: executeOutbound(bytes32 txId, address recipient, uint256 amount, address asset, bytes payload)
	// Selector: first 4 bytes of keccak256("executeOutbound(bytes32,address,uint256,address,bytes)")

	// For now, return a simple transfer call data
	// In production, this should be the actual gateway contract ABI encoding
	callData := buildExecuteOutboundCallData(data.TxID, recipient, amount, assetAddr, payload)

	return callData, nil
}

// buildExecuteOutboundCallData creates the ABI-encoded call data for executeOutbound.
func buildExecuteOutboundCallData(txID string, recipient common.Address, amount *big.Int, asset common.Address, payload []byte) []byte {
	// Function selector for executeOutbound(bytes32,address,uint256,address,bytes)
	// keccak256("executeOutbound(bytes32,address,uint256,address,bytes)")[:4]
	selector := crypto.Keccak256([]byte("executeOutbound(bytes32,address,uint256,address,bytes)"))[:4]

	// Encode txID as bytes32
	txIDBytes := common.HexToHash(txID)

	// Build the call data
	// This is a simplified encoding - in production use go-ethereum's abi package
	data := make([]byte, 0, 4+32*5+len(payload))
	data = append(data, selector...)
	data = append(data, txIDBytes[:]...)
	data = append(data, common.LeftPadBytes(recipient[:], 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(asset[:], 32)...)

	// Dynamic data offset for payload (5 * 32 = 160)
	offset := big.NewInt(160)
	data = append(data, common.LeftPadBytes(offset.Bytes(), 32)...)

	// Payload length
	payloadLen := big.NewInt(int64(len(payload)))
	data = append(data, common.LeftPadBytes(payloadLen.Bytes(), 32)...)

	// Payload data (padded to 32 bytes)
	if len(payload) > 0 {
		paddedPayload := make([]byte, ((len(payload)+31)/32)*32)
		copy(paddedPayload, payload)
		data = append(data, paddedPayload...)
	}

	return data
}
