package txresolver

import (
	"context"
	"encoding/json"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
)

// Decision flow for EVM-broadcasted events (outbound and fund migration both
// follow this shape):
//
//   - VerifyBroadcastedTx error                      → stay BROADCASTED (retry)
//   - Tx found, insufficient confirmations           → stay BROADCASTED (retry)
//   - Tx found, status=1 (success)                   → COMPLETED / vote success
//   - Tx found, status=0 (reverted on chain)         → REVERT  / vote failure with tx hash
//   - Tx not found, signed nonce < finalized nonce   → REVERT  / vote failure (another tx
//                                                                consumed our nonce slot)
//   - Tx not found, signed nonce >= finalized nonce  → rewind to SIGNED so the broadcaster
//                                                      re-broadcasts (covers mempool drop)
//   - Tx not found, nonce check unavailable          → stay BROADCASTED (retry)
//
// The nonce IS the give-up signal; there is no max-retry counter. The two
// flows differ only in (a) which vote function records success/failure and
// (b) where the signer address comes from — current TSS for outbound, OLD TSS
// (derived from the event's old pubkey) for fund migration.

// signedNonceEnvelope is the slice of any EVM-signed event payload the
// resolver consults on tx-not-found. OldTssPubkey is set only on fund
// migration events; it identifies the sender (and thus the address whose
// nonce gates the decision).
type signedNonceEnvelope struct {
	OldTssPubkey string `json:"old_tss_pubkey,omitempty"`
	SigningData  *struct {
		Nonce uint64 `json:"nonce"`
	} `json:"signing_data,omitempty"`
}

func readSignedNonce(event *store.Event) (uint64, bool) {
	var env signedNonceEnvelope
	if err := json.Unmarshal(event.EventData, &env); err != nil || env.SigningData == nil {
		return 0, false
	}
	return env.SigningData.Nonce, true
}

func readFundMigrationSigner(event *store.Event) (signer string, nonce uint64, ok bool) {
	var env signedNonceEnvelope
	if err := json.Unmarshal(event.EventData, &env); err != nil || env.SigningData == nil || env.OldTssPubkey == "" {
		return "", 0, false
	}
	addr, err := coordinator.DeriveEVMAddressFromPubkey(env.OldTssPubkey)
	if err != nil {
		return "", 0, false
	}
	return addr, env.SigningData.Nonce, true
}

// nonceVerdict captures the outcome of comparing the signed nonce against the
// chain's finalized nonce when a tx hash isn't on chain.
type nonceVerdict int

const (
	nonceUnknown   nonceVerdict = iota // RPC failed; defer the decision
	nonceConsumed                      // chain advanced past the signed nonce → tx is dead
	nonceAvailable                     // chain hasn't consumed the signed nonce yet → re-broadcast may still land
)

func checkNonce(ctx context.Context, builder common.TxBuilder, signer string, signedNonce uint64) (nonceVerdict, uint64) {
	finalizedNonce, err := builder.GetNextNonce(ctx, signer, true)
	if err != nil {
		return nonceUnknown, 0
	}
	if signedNonce < finalizedNonce {
		return nonceConsumed, finalizedNonce
	}
	return nonceAvailable, finalizedNonce
}

// resolveOutboundEVM resolves a BROADCASTED outbound on an EVM chain.
func (r *Resolver) resolveOutboundEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string) {
	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to extract outbound IDs")
		return
	}

	builder, err := r.getBuilder(chainID)
	if err != nil {
		r.logger.Debug().Err(err).Str("event_id", event.EventID).Msg("failed to get tx builder, will retry next tick")
		return
	}

	found, blockHeight, confirmations, status, vErr := builder.VerifyBroadcastedTx(ctx, rawTxHash)
	if vErr != nil {
		r.logger.Debug().Err(vErr).Str("event_id", event.EventID).Msg("tx verification error, will retry next tick")
		return
	}

	if found {
		if confirmations < r.chains.GetStandardConfirmations(chainID) {
			return
		}
		if status == 0 {
			gasFeeUsed := "0"
			if fee, fErr := builder.GetGasFeeUsed(ctx, rawTxHash); fErr == nil {
				gasFeeUsed = fee
			}
			_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, rawTxHash, blockHeight, gasFeeUsed,
				"tx execution reverted on destination chain")
			return
		}
		if uerr := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusCompleted}); uerr != nil {
			r.logger.Warn().Err(uerr).Str("event_id", event.EventID).Msg("failed to mark event COMPLETED")
			return
		}
		r.logger.Info().Str("event_id", event.EventID).Str("tx_hash", rawTxHash).Msg("outbound EVM tx marked COMPLETED")
		return
	}

	signer, signedNonce, ok := r.outboundSigner(ctx, event)
	if !ok {
		return
	}
	verdict, finalizedNonce := checkNonce(ctx, builder, signer, signedNonce)
	switch verdict {
	case nonceUnknown:
		r.logger.Debug().Str("event_id", event.EventID).Msg("could not fetch finalized nonce, will retry next tick")
	case nonceConsumed:
		r.logger.Info().Str("event_id", event.EventID).Str("chain", chainID).
			Uint64("signed_nonce", signedNonce).Uint64("finalized_nonce", finalizedNonce).
			Msg("EVM outbound tx not found and nonce already finalized → REVERT")
		_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "0",
			"tx not found on destination chain and nonce consumed by another tx")
	case nonceAvailable:
		r.rewindToSigned(event, chainID, signedNonce, finalizedNonce)
	}
}

// resolveFundMigrationEVM mirrors resolveOutboundEVM. The signer comes from
// the event payload (OldTssPubkey) instead of the current TSS, and the
// success/failure path uses the fund-migration voting helper which both votes
// and marks the event in a single step.
func (r *Resolver) resolveFundMigrationEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string, migrationID uint64) {
	builder, err := r.getBuilder(chainID)
	if err != nil {
		r.logger.Debug().Err(err).Str("event_id", event.EventID).Msg("failed to get tx builder, will retry next tick")
		return
	}

	found, _, confirmations, status, vErr := builder.VerifyBroadcastedTx(ctx, rawTxHash)
	if vErr != nil {
		r.logger.Debug().Err(vErr).Str("event_id", event.EventID).Msg("fund migration tx verification error, will retry next tick")
		return
	}

	if found {
		if confirmations < r.chains.GetStandardConfirmations(chainID) {
			return
		}
		r.voteFundMigrationAndMark(ctx, event, migrationID, rawTxHash, status != 0)
		return
	}

	signer, signedNonce, ok := readFundMigrationSigner(event)
	if !ok {
		r.logger.Warn().Str("event_id", event.EventID).
			Msg("fund migration tx not found and signer info unavailable, staying BROADCASTED")
		return
	}
	verdict, finalizedNonce := checkNonce(ctx, builder, signer, signedNonce)
	switch verdict {
	case nonceUnknown:
		r.logger.Debug().Str("event_id", event.EventID).Msg("could not fetch finalized nonce, will retry next tick")
	case nonceConsumed:
		r.logger.Info().Str("event_id", event.EventID).Str("chain", chainID).
			Uint64("signed_nonce", signedNonce).Uint64("finalized_nonce", finalizedNonce).
			Msg("EVM fund migration tx not found and nonce already finalized → REVERT")
		r.voteFundMigrationAndMark(ctx, event, migrationID, "", false)
	case nonceAvailable:
		r.rewindToSigned(event, chainID, signedNonce, finalizedNonce)
	}
}

// outboundSigner resolves the outbound signer + signed nonce, logging and
// returning ok=false when either is unavailable so the caller can defer
// without dragging the resolver-level guards into the main flow.
func (r *Resolver) outboundSigner(ctx context.Context, event *store.Event) (string, uint64, bool) {
	signedNonce, ok := readSignedNonce(event)
	if !ok {
		r.logger.Warn().Str("event_id", event.EventID).
			Msg("EVM tx not found and signed nonce unavailable, staying BROADCASTED")
		return "", 0, false
	}
	if r.getTSSAddress == nil {
		r.logger.Warn().Str("event_id", event.EventID).
			Msg("EVM tx not found and no TSS-address resolver configured, staying BROADCASTED")
		return "", 0, false
	}
	addr, err := r.getTSSAddress(ctx)
	if err != nil {
		r.logger.Debug().Err(err).Str("event_id", event.EventID).Msg("could not fetch TSS address, will retry next tick")
		return "", 0, false
	}
	return addr, signedNonce, true
}

// rewindToSigned moves a BROADCASTED event back to SIGNED so the broadcaster
// will re-broadcast on the next tick. Used when the EVM tx hash isn't visible
// on chain but the signed nonce is still available — covers mempool drops.
func (r *Resolver) rewindToSigned(event *store.Event, chainID string, signedNonce, finalizedNonce uint64) {
	if err := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusSigned}); err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to rewind event to SIGNED for re-broadcast")
		return
	}
	r.logger.Info().Str("event_id", event.EventID).Str("chain", chainID).
		Uint64("signed_nonce", signedNonce).Uint64("finalized_nonce", finalizedNonce).
		Msg("EVM tx not found and nonce un-consumed, rewound to SIGNED for re-broadcast")
}
