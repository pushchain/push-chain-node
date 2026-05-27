package txresolver

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/txflow"
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
//
// Shared types (SignedOutboundData / SigningData) and helpers (DecodeSigningData,
// ReadSignedNonce, ReadFundMigrationSigner, CheckNonce, NonceVerdict) live in
// tss/txflow so the broadcaster applies the exact same rules.

// resolveOutboundEVM resolves a BROADCASTED outbound on an EVM chain.
func (r *Resolver) resolveOutboundEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string) {
	log := r.logger.With().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Str("chain", chainID).
		Str("tx_hash", rawTxHash).Logger()

	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		log.Warn().Err(err).Msg("failed to extract outbound IDs")
		return
	}

	builder, err := r.getBuilder(chainID)
	if err != nil {
		log.Debug().Err(err).Msg("failed to get tx builder, will retry next tick")
		return
	}

	found, blockHeight, confirmations, status, vErr := builder.VerifyBroadcastedTx(ctx, rawTxHash)
	if vErr != nil {
		log.Debug().Err(vErr).Msg("tx verification error, will retry next tick")
		return
	}

	if found {
		if confirmations < r.chains.GetStandardConfirmations(chainID) {
			return
		}
		if status == 0 {
			gasFeeUsed, fErr := builder.GetGasFeeUsed(ctx, rawTxHash)
			if fErr != nil {
				log.Debug().Err(fErr).Msg("failed to fetch gas fee for reverted tx, will retry next tick")
				return
			}
			_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, rawTxHash, blockHeight, gasFeeUsed,
				"tx execution reverted on destination chain")
			return
		}
		if uerr := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusCompleted}); uerr != nil {
			log.Warn().Err(uerr).Msg("failed to mark event COMPLETED")
			return
		}
		log.Info().Msg("event marked as COMPLETED")
		return
	}

	signer, signedNonce, ok := r.outboundSigner(ctx, event)
	if !ok {
		return
	}
	verdict, finalizedNonce := txflow.CheckNonce(ctx, builder, signer, signedNonce)
	nlog := log.With().Uint64("signed_nonce", signedNonce).Uint64("finalized_nonce", finalizedNonce).Logger()
	switch verdict {
	case txflow.NonceUnknown:
		nlog.Debug().Msg("could not fetch finalized nonce, will retry next tick")
	case txflow.NonceConsumed:
		nlog.Debug().Msg("EVM outbound tx not found and nonce already finalized → REVERT")
		_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "0",
			"tx not executed on destination chain")
	case txflow.NonceAvailable:
		r.rewindToSigned(event, chainID, signedNonce, finalizedNonce)
	}
}

// resolveFundMigrationEVM mirrors resolveOutboundEVM. The signer comes from
// the event payload (OldTssPubkey) instead of the current TSS, and the
// success/failure path uses the fund-migration voting helper which both votes
// and marks the event in a single step.
func (r *Resolver) resolveFundMigrationEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string, migrationID uint64) {
	log := r.logger.With().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Str("chain", chainID).
		Str("tx_hash", rawTxHash).Logger()

	builder, err := r.getBuilder(chainID)
	if err != nil {
		log.Debug().Err(err).Msg("failed to get tx builder, will retry next tick")
		return
	}

	found, _, confirmations, status, vErr := builder.VerifyBroadcastedTx(ctx, rawTxHash)
	if vErr != nil {
		log.Debug().Err(vErr).Msg("fund migration tx verification error, will retry next tick")
		return
	}

	if found {
		if confirmations < r.chains.GetStandardConfirmations(chainID) {
			return
		}
		r.voteFundMigrationAndMark(ctx, event, migrationID, rawTxHash, status != 0)
		return
	}

	signer, signedNonce, ok := txflow.ReadFundMigrationSigner(event)
	if !ok {
		log.Warn().Msg("fund migration tx not found and signer info unavailable, staying BROADCASTED")
		return
	}
	verdict, finalizedNonce := txflow.CheckNonce(ctx, builder, signer, signedNonce)
	nlog := log.With().Uint64("signed_nonce", signedNonce).Uint64("finalized_nonce", finalizedNonce).Logger()
	switch verdict {
	case txflow.NonceUnknown:
		nlog.Debug().Msg("could not fetch finalized nonce, will retry next tick")
	case txflow.NonceConsumed:
		nlog.Debug().Msg("EVM fund migration tx not found and nonce already finalized → REVERT")
		r.voteFundMigrationAndMark(ctx, event, migrationID, "", false)
	case txflow.NonceAvailable:
		r.rewindToSigned(event, chainID, signedNonce, finalizedNonce)
	}
}

// outboundSigner resolves the outbound signer + signed nonce, logging and
// returning ok=false when either is unavailable so the caller can defer
// without dragging the resolver-level guards into the main flow.
func (r *Resolver) outboundSigner(ctx context.Context, event *store.Event) (string, uint64, bool) {
	log := r.logger.With().Str("event_id", event.EventID).Logger()

	signedNonce, ok := txflow.ReadSignedNonce(event)
	if !ok {
		log.Warn().Msg("EVM tx not found and signed nonce unavailable, staying BROADCASTED")
		return "", 0, false
	}
	if r.getTSSAddress == nil {
		log.Warn().Msg("EVM tx not found and no TSS-address resolver configured, staying BROADCASTED")
		return "", 0, false
	}
	addr, err := r.getTSSAddress(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("could not fetch TSS address, will retry next tick")
		return "", 0, false
	}
	return addr, signedNonce, true
}

// rewindToSigned moves a BROADCASTED event back to SIGNED so the broadcaster
// will re-broadcast on the next tick. Used when the EVM tx hash isn't visible
// on chain but the signed nonce is still available — covers mempool drops.
func (r *Resolver) rewindToSigned(event *store.Event, chainID string, signedNonce, finalizedNonce uint64) {
	log := r.logger.With().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Str("chain", chainID).
		Uint64("signed_nonce", signedNonce).
		Uint64("finalized_nonce", finalizedNonce).Logger()

	if err := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusSigned}); err != nil {
		log.Warn().Err(err).Msg("failed to rewind event to SIGNED for re-broadcast")
		return
	}
	log.Debug().Msg("event marked as SIGNED")
}
