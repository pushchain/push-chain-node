package svm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"

	chaincommon "github.com/pushchain/push-chain-node/universalClient/chains/common"
)

// OutboundTxBuilder builds outbound transactions for Solana chains.
type OutboundTxBuilder struct {
	client         *Client
	caipChainID    string
	gatewayProgram solana.PublicKey
	tssPublicKey   solana.PublicKey // TSS public key (ed25519)
	logger         zerolog.Logger
}

// NewOutboundTxBuilder creates a new Solana outbound transaction builder.
func NewOutboundTxBuilder(
	client *Client,
	gatewayProgram solana.PublicKey,
	tssPublicKey solana.PublicKey,
	logger zerolog.Logger,
) *OutboundTxBuilder {
	return &OutboundTxBuilder{
		client:         client,
		caipChainID:    client.GetConfig().Chain,
		gatewayProgram: gatewayProgram,
		tssPublicKey:   tssPublicKey,
		logger:         logger.With().Str("component", "svm_outbound_builder").Logger(),
	}
}

// BuildTransaction creates an unsigned Solana transaction from outbound data.
// gasPrice is accepted for interface compatibility but not used for Solana (uses compute units instead).
func (b *OutboundTxBuilder) BuildTransaction(ctx context.Context, data *chaincommon.OutboundTxData, gasPrice *big.Int) (*chaincommon.OutboundTxResult, error) {
	if data == nil {
		return nil, fmt.Errorf("outbound data is nil")
	}
	// Note: gasPrice is not used for Solana transactions (they use compute units)

	b.logger.Debug().
		Str("tx_id", data.TxID).
		Str("recipient", data.Recipient).
		Str("amount", data.Amount).
		Msg("building Solana outbound transaction")

	// Get recent blockhash
	recentBlockhash, err := b.getRecentBlockhash(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	// Build the instruction for the gateway program
	instruction, err := b.buildGatewayInstruction(data)
	if err != nil {
		return nil, fmt.Errorf("failed to build gateway instruction: %w", err)
	}

	// Create the transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		recentBlockhash,
		solana.TransactionPayer(b.tssPublicKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Get the message to sign
	messageBytes, err := tx.Message.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	// The signing hash is the message itself for Solana (ed25519 signs the message directly)
	signingHash := messageBytes

	// Serialize the unsigned transaction
	rawTx, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction: %w", err)
	}

	return &chaincommon.OutboundTxResult{
		RawTx:       rawTx,
		SigningHash: signingHash,
		ChainID:     b.caipChainID,
		Blockhash:   recentBlockhash[:],
	}, nil
}

// AssembleSignedTransaction combines the unsigned transaction with the TSS signature.
func (b *OutboundTxBuilder) AssembleSignedTransaction(unsignedTx []byte, signature []byte, recoveryID byte) ([]byte, error) {
	if len(signature) != 64 {
		return nil, fmt.Errorf("invalid signature length: expected 64, got %d", len(signature))
	}

	// Decode the unsigned transaction
	tx, err := solana.TransactionFromDecoder(bin.NewBinDecoder(unsignedTx))
	if err != nil {
		return nil, fmt.Errorf("failed to decode unsigned transaction: %w", err)
	}

	// Create Solana signature from the 64-byte signature
	var sig solana.Signature
	copy(sig[:], signature)

	// Add the signature to the transaction
	tx.Signatures = []solana.Signature{sig}

	// Serialize the signed transaction
	signedTx, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signed transaction: %w", err)
	}

	return signedTx, nil
}

// BroadcastTransaction sends the signed transaction to the network.
func (b *OutboundTxBuilder) BroadcastTransaction(ctx context.Context, signedTx []byte) (string, error) {
	// Decode the signed transaction
	tx, err := solana.TransactionFromDecoder(bin.NewBinDecoder(signedTx))
	if err != nil {
		return "", fmt.Errorf("failed to decode signed transaction: %w", err)
	}

	// Get RPC client
	rpcClient, err := b.client.getRPCClient()
	if err != nil {
		return "", fmt.Errorf("failed to get RPC client: %w", err)
	}

	// Send the transaction
	sig, err := rpcClient.SendTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	txHash := sig.String()
	b.logger.Info().
		Str("tx_hash", txHash).
		Msg("outbound transaction broadcasted")

	return txHash, nil
}

// GetChainID returns the chain identifier.
func (b *OutboundTxBuilder) GetChainID() string {
	return b.caipChainID
}

// getRecentBlockhash gets a recent blockhash for the transaction.
func (b *OutboundTxBuilder) getRecentBlockhash(ctx context.Context) (solana.Hash, error) {
	rpcClient, err := b.client.getRPCClient()
	if err != nil {
		return solana.Hash{}, fmt.Errorf("failed to get RPC client: %w", err)
	}

	resp, err := rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return solana.Hash{}, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	return resp.Value.Blockhash, nil
}

// buildGatewayInstruction builds the instruction for the gateway program.
func (b *OutboundTxBuilder) buildGatewayInstruction(data *chaincommon.OutboundTxData) (solana.Instruction, error) {
	// Parse recipient as Solana public key
	recipient, err := solana.PublicKeyFromBase58(data.Recipient)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}

	// Parse amount
	amount, ok := new(big.Int).SetString(data.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", data.Amount)
	}

	// Parse asset address (if provided)
	var assetMint solana.PublicKey
	if data.AssetAddr != "" && data.AssetAddr != "0x" {
		assetMint, err = solana.PublicKeyFromBase58(data.AssetAddr)
		if err != nil {
			return nil, fmt.Errorf("invalid asset address: %w", err)
		}
	}

	// Parse payload
	var payload []byte
	if data.Payload != "" && data.Payload != "0x" {
		payloadHex := strings.TrimPrefix(data.Payload, "0x")
		payload, err = hex.DecodeString(payloadHex)
		if err != nil {
			return nil, fmt.Errorf("invalid payload hex: %w", err)
		}
	}

	// Build the instruction data
	instructionData := buildExecuteOutboundInstructionData(data.TxID, amount, payload)

	// Build account metas
	accounts := []*solana.AccountMeta{
		{PublicKey: b.tssPublicKey, IsSigner: true, IsWritable: true},     // Payer/Signer
		{PublicKey: recipient, IsSigner: false, IsWritable: true},         // Recipient
		{PublicKey: b.gatewayProgram, IsSigner: false, IsWritable: false}, // Gateway program
	}

	// Add asset mint if token transfer
	if assetMint != (solana.PublicKey{}) {
		accounts = append(accounts, &solana.AccountMeta{
			PublicKey:  assetMint,
			IsSigner:   false,
			IsWritable: false,
		})
	}

	return &gatewayInstruction{
		programID: b.gatewayProgram,
		accounts:  accounts,
		data:      instructionData,
	}, nil
}

// gatewayInstruction implements solana.Instruction for the gateway program.
type gatewayInstruction struct {
	programID solana.PublicKey
	accounts  []*solana.AccountMeta
	data      []byte
}

func (i *gatewayInstruction) ProgramID() solana.PublicKey {
	return i.programID
}

func (i *gatewayInstruction) Accounts() []*solana.AccountMeta {
	return i.accounts
}

func (i *gatewayInstruction) Data() ([]byte, error) {
	return i.data, nil
}

// buildExecuteOutboundInstructionData creates the instruction data for executeOutbound.
func buildExecuteOutboundInstructionData(txID string, amount *big.Int, payload []byte) []byte {
	// Instruction discriminator for "execute_outbound"
	// This is typically the first 8 bytes of sha256("global:execute_outbound")
	discriminator := sha256.Sum256([]byte("global:execute_outbound"))

	// Build instruction data
	data := make([]byte, 0, 8+32+8+4+len(payload))

	// Discriminator (8 bytes)
	data = append(data, discriminator[:8]...)

	// TxID as bytes32 (32 bytes)
	txIDBytes := make([]byte, 32)
	txIDHex := strings.TrimPrefix(txID, "0x")
	if decoded, err := hex.DecodeString(txIDHex); err == nil {
		copy(txIDBytes, decoded)
	}
	data = append(data, txIDBytes...)

	// Amount as u64 (8 bytes, little-endian)
	amountBytes := make([]byte, 8)
	amountU64 := amount.Uint64()
	for i := 0; i < 8; i++ {
		amountBytes[i] = byte(amountU64 >> (8 * i))
	}
	data = append(data, amountBytes...)

	// Payload length (4 bytes, little-endian)
	payloadLen := uint32(len(payload))
	data = append(data, byte(payloadLen), byte(payloadLen>>8), byte(payloadLen>>16), byte(payloadLen>>24))

	// Payload data
	data = append(data, payload...)

	return data
}
