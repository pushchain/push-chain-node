package common

import (
	"context"
	"math/big"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// OutboundTxData is an alias for uexecutortypes.OutboundCreatedEvent.
// This represents the data needed to create an outbound transaction.
type OutboundTxData = uexecutortypes.OutboundCreatedEvent

// OutboundTxResult represents the result of building an outbound transaction.
type OutboundTxResult struct {
	// RawTx is the serialized unsigned transaction ready for signing
	RawTx []byte `json:"raw_tx"`

	// SigningHash is the hash that needs to be signed by TSS
	// For EVM: keccak256 hash of the transaction
	// For Solana: the message hash
	SigningHash []byte `json:"signing_hash"`

	// Nonce is the transaction nonce (EVM only)
	Nonce uint64 `json:"nonce,omitempty"`

	// GasPrice is the gas price used (EVM only)
	GasPrice *big.Int `json:"gas_price,omitempty"`

	// GasLimit is the gas limit used
	GasLimit uint64 `json:"gas_limit"`

	// ChainID is the destination chain ID
	ChainID string `json:"chain_id"`

	// Blockhash is the recent blockhash used (Solana only)
	Blockhash []byte `json:"blockhash,omitempty"`
}

// OutboundTxBuilder defines the interface for building outbound transactions.
// Each chain type (EVM, SVM) implements this interface.
type OutboundTxBuilder interface {
	// BuildTransaction creates an unsigned transaction from outbound data.
	// gasPrice: the gas price from on-chain oracle (passed by coordinator)
	// Fetches nonce from destination chain.
	// Returns the transaction result containing the raw tx and signing hash.
	BuildTransaction(ctx context.Context, data *OutboundTxData, gasPrice *big.Int) (*OutboundTxResult, error)

	// AssembleSignedTransaction combines the unsigned transaction with the TSS signature.
	// Returns the fully signed transaction ready for broadcast.
	AssembleSignedTransaction(unsignedTx []byte, signature []byte, recoveryID byte) ([]byte, error)

	// BroadcastTransaction sends the signed transaction to the network.
	// Returns the transaction hash.
	BroadcastTransaction(ctx context.Context, signedTx []byte) (string, error)

	// GetChainID returns the chain identifier this builder is configured for.
	GetChainID() string
}

// OutboundTxBuilderFactory creates OutboundTxBuilder instances for different chains.
type OutboundTxBuilderFactory interface {
	// CreateBuilder creates an OutboundTxBuilder for the specified chain.
	CreateBuilder(chainID string) (OutboundTxBuilder, error)

	// SupportsChain returns true if the factory can create a builder for the chain.
	SupportsChain(chainID string) bool
}
