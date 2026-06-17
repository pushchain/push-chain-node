package txflow

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
)

// NonceVerdict captures the outcome of comparing the signed nonce against
// the chain's finalized nonce. EVM-only — SVM does not use nonces this way.
type NonceVerdict int

const (
	// NonceUnknown means the RPC failed and the caller should defer the decision.
	NonceUnknown NonceVerdict = iota
	// NonceConsumed means the chain advanced past the signed nonce. Some other
	// tx took that slot; our signed tx can never land.
	NonceConsumed
	// NonceAvailable means the chain hasn't consumed the signed nonce yet. The
	// tx may still be in mempool, or was dropped — a re-broadcast may land it.
	NonceAvailable
)

// CheckNonce compares signedNonce against the chain's finalized nonce for
// `signer`. Used by:
//   - broadcaster (post-broadcast-error path) to detect "the tx already
//     landed via another node despite our RPC error"
//   - resolver (tx-not-found path) to distinguish dead tx (REVERT) from
//     mempool-drop (rewind to SIGNED).
//
// The returned finalizedNonce is for logging / observability.
func CheckNonce(ctx context.Context, builder common.TxBuilder, signer string, signedNonce uint64) (NonceVerdict, uint64) {
	finalizedNonce, err := builder.GetNextNonce(ctx, signer, true)
	if err != nil {
		return NonceUnknown, 0
	}
	if signedNonce < finalizedNonce {
		return NonceConsumed, finalizedNonce
	}
	return NonceAvailable, finalizedNonce
}
