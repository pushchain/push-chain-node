package common

import (
	"context"
	"fmt"
	"math/big"

	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// EncodeUint256Result canonically encodes a balance/amount as abi.encode(uint256)
// so read results are byte-identical across validators and decodable by the
// requesting contract. The bounds check guards against a malicious RPC value
// that would not fit (FillBytes panics on overflow).
func EncodeUint256Result(v *big.Int) ([]byte, error) {
	if v == nil {
		v = big.NewInt(0)
	}
	if v.Sign() < 0 || v.BitLen() > 256 {
		return nil, fmt.Errorf("value out of uint256 range")
	}
	out := make([]byte, 32)
	v.FillBytes(out)
	return out, nil
}

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

	// GetReadRequestHandler returns the handler executing read requests
	// destined for this chain
	// Returns an error if reads are not available (e.g. client not started)
	GetReadRequestHandler() (ReadRequestHandler, error)
}

// FundMigrationData contains the data needed to build a fund migration transaction.
// Populated by the coordinator from the migration event + derived addresses.
type FundMigrationData struct {
	From     string   // Old TSS address (derived from old pubkey)
	To       string   // New TSS address (derived from current pubkey)
	GasPrice *big.Int // Gas price from the migration event
	GasLimit uint64   // Gas limit from the migration event
	L1GasFee *big.Int // Extra L1 data-availability fee (wei); 0 for non-L2 chains

	Balance *big.Int // if nil, builder queries chain
}

// UnsignedSigningReq contains the request for signing an outbound or fund-migration transaction.
type UnsignedSigningReq struct {
	SigningHash []byte // Hash to be signed by TSS
	Nonce       uint64 // evm - TSS Address nonce | svm - PDA nonce

	// TSSFundMigrationAmount is the native value swept for a fund-migration tx, fixed at
	// signing time. Nil for outbound. Must be reused verbatim at broadcast — re-querying
	// balance there races with a successful sweep from another validator.
	TSSFundMigrationAmount *big.Int `json:"TSSFundMigrationAmount,omitempty"`
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
	// For SVM: checks if the ExecutedTx PDA exists on-chain, AND returns the
	//   unix timestamp of the latest finalized block. Callers use this as the
	//   cluster's "now" to gate deadline-based give-up/REVERT decisions and to
	//   detect cluster halt or finalization stall (queryBlockTime far behind
	//   wall-clock). 0 means freshness couldn't be determined.
	// For EVM: returns (false, 0, nil). EVM uses nonce-based replay protection.
	IsAlreadyExecuted(ctx context.Context, txID string) (executed bool, queryBlockTime int64, err error)

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

// ReadRequestHandler executes a read request on one destination chain.
// Consumed by the push watcher's read processor.
type ReadRequestHandler interface {
	ExecuteRead(ctx context.Context, req *ucallbacktypes.ReadRequest) (*ucallbacktypes.ReadResult, error)
}

// NewReadErrorResult builds an ERROR observation carrying a deterministic error
// code. ResultData stays empty and only the code (never local error text) is
// voted, so every validator observing the same failure converges on one ballot.
func NewReadErrorResult(code ucallbacktypes.ReadErrorCode) *ucallbacktypes.ReadResult {
	return &ucallbacktypes.ReadResult{
		Status:    ucallbacktypes.ReadStatus_READ_STATUS_ERROR,
		ErrorCode: code,
	}
}
