package common

import (
	"context"
	"math/big"

	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ChainClient defines the interface for chain-specific implementations
type ChainClient interface {
	// Start initializes and starts the chain client
	Start(ctx context.Context) error

	// Stop gracefully shuts down the chain client
	Stop() error

	// IsHealthy checks if the chain client is operational
	IsHealthy() bool

	// GetTxBuilder returns the TxBuilder for this chain
	// Returns an error if txBuilder is not supported for this chain (e.g., Push chain)
	GetTxBuilder() (TxBuilder, error)
}

// FundMigrationData contains the data needed to build a fund migration transaction.
// Populated by the coordinator from the migration event + derived addresses.
type FundMigrationData struct {
	From     string   // Old TSS address (derived from old pubkey)
	To       string   // New TSS address (derived from current pubkey)
	GasPrice *big.Int // Gas price from the migration event
	GasLimit uint64   // Gas limit from the migration event
}

// UnsignedSigningReq contains the request for signing an outbound transaction
type UnsignedSigningReq struct {
	SigningHash []byte // Hash to be signed by TSS
	Nonce       uint64 // evm - TSS Address nonce | svm - PDA nonce
}

// TxBuilder builds and broadcasts transactions for outbound transfers
type TxBuilder interface {
	// GetOutboundSigningRequest creates a signing request from outbound event data
	GetOutboundSigningRequest(ctx context.Context, data *uetypes.OutboundCreatedEvent, nonce uint64) (*UnsignedSigningReq, error)

	// GetNextNonce returns the next nonce for the given signer on this chain (for seeding local nonce).
	// useFinalized: for EVM, if true use finalized block nonce (aggressive/replace stuck); if false use pending. SVM ignores this.
	GetNextNonce(ctx context.Context, signerAddress string, useFinalized bool) (uint64, error)

	// BroadcastOutboundSigningRequest assembles and broadcasts a signed transaction from the signing request, event data, and signature
	BroadcastOutboundSigningRequest(ctx context.Context, req *UnsignedSigningReq, data *uetypes.OutboundCreatedEvent, signature []byte) (string, error)

	// VerifyBroadcastedTx checks the status of a broadcasted transaction on the destination chain.
	// Returns (found, blockHeight, confirmations, status, error):
	// - found=false: tx not found or not yet mined
	// - found=true: tx exists on-chain
	//   - blockHeight: the block in which the tx was mined
	//   - confirmations: number of blocks since the tx was mined (0 = just mined)
	//   - status: 0 = failed/reverted, 1 = success
	VerifyBroadcastedTx(ctx context.Context, txHash string) (found bool, blockHeight uint64, confirmations uint64, status uint8, err error)

	// IsAlreadyExecuted checks whether a transaction with the given txID has already been
	// executed on the destination chain (e.g., by another relayer).
	// For SVM: checks if the ExecutedTx PDA exists on-chain.
	// For EVM: returns false (EVM uses nonce-based replay protection).
	IsAlreadyExecuted(ctx context.Context, txID string) (bool, error)

	// GetGasFeeUsed returns the gas fee used by a transaction on the destination chain.
	// EVM: fetches receipt and returns gasUsed * effectiveGasPrice as decimal string.
	// SVM: returns "0" (gas accounting is handled via vault gasFee reimbursement).
	// Returns "0" if the transaction is not found.
	GetGasFeeUsed(ctx context.Context, txHash string) (string, error)

	// GetFundMigrationSigningRequest builds a native token transfer for fund migration,
	// transferring the maximum possible balance (balance minus gas cost).
	GetFundMigrationSigningRequest(ctx context.Context, data *FundMigrationData, nonce uint64) (*UnsignedSigningReq, error)

	// BroadcastFundMigrationTx assembles and broadcasts a signed fund migration transaction.
	BroadcastFundMigrationTx(ctx context.Context, req *UnsignedSigningReq, data *FundMigrationData, signature []byte) (string, error)
}

// UniversalTx Payload
type UniversalTx struct {
	SourceChain         string                   `json:"sourceChain"`
	LogIndex            uint                     `json:"logIndex"`
	Sender              string                   `json:"sender"`
	Recipient           string                   `json:"recipient"`
	Token               string                   `json:"bridgeToken"`
	Amount              string `json:"bridgeAmount"` // uint256 as decimal string
	RawPayload          string `json:"rawPayload,omitempty"` // hex-encoded raw payload bytes from source chain
	VerificationData    string `json:"verificationData"`
	RevertFundRecipient string                   `json:"revertFundRecipient,omitempty"`
	TxType              uint                     `json:"txType"`              // enum backing uint as decimal string
	FromCEA             bool                     `json:"fromCEA"`             // true if inbound is initiated by a CEA
}

// OutboundEvent represents an outbound observation event from the gateway contract
// Event structure:
// - txID at 1st indexed position (bytes32)
// - universalTxID at 2nd indexed position (bytes32)
type OutboundEvent struct {
	TxID          string `json:"tx_id"`                      // bytes32 hex-encoded (0x...)
	UniversalTxID string `json:"universal_tx_id"`            // bytes32 hex-encoded (0x...)
	GasFeeUsed    string `json:"gas_fee_used,omitempty"`     // gas fee used in wei (decimal string)
}

