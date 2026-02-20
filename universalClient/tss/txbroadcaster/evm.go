package txbroadcaster

import (
	"context"
	"encoding/hex"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// broadcastEVM broadcasts a signed EVM transaction.
//
// All validators produce the same signed tx (deterministic TSS output), so the
// tx hash is known before broadcasting (computed from the assembled signed tx).
//
// Flow:
//  1. Build and broadcast the signed tx (tx hash is always returned, even on error)
//  2. Success → BROADCASTED with tx hash
//  3. Error → check finalized nonce on chain:
//     - nonce consumed (tx landed) → BROADCASTED with tx hash
//     - nonce NOT consumed → keep SIGNED, retry next tick
func (b *Broadcaster) broadcastEVM(ctx context.Context, event *store.Event, data *SignedEventData, chainID string) {
	signingReq, err := reconstructSigningReq(data.SigningData)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to reconstruct signing request")
		return
	}

	signature, err := hex.DecodeString(data.SigningData.Signature)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to decode signature")
		return
	}

	// Strip recovery byte if 65 bytes (broadcast expects 64-byte signature)
	if len(signature) == 65 {
		signature = signature[:64]
	}

	client, err := b.chains.GetClient(chainID)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to get chain client")
		return
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to get tx builder")
		return
	}

	// Broadcast — tx hash is computed before sending, so it's returned even on RPC error
	outboundData := data.OutboundCreatedEvent
	txHash, broadcastErr := builder.BroadcastOutboundSigningRequest(ctx, signingReq, &outboundData, signature)

	if broadcastErr == nil {
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Broadcast failed — check if the tx landed on chain anyway (another node, or "already known")
	if txHash == "" {
		// Tx couldn't even be assembled (bad signature, invalid data) — permanent error, retry won't help
		b.logger.Warn().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("failed to assemble tx, will retry next tick")
		return
	}

	eventNonce := data.SigningData.Nonce
	tssAddress := ""
	if b.getTSSAddress != nil {
		tssAddress, _ = b.getTSSAddress(ctx)
	}

	finalizedNonce, err := builder.GetNextNonce(ctx, tssAddress, true)
	if err == nil && eventNonce < finalizedNonce {
		// Nonce consumed — tx is on chain. Mark BROADCASTED so the resolver can verify it.
		b.logger.Info().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Str("tx_hash", txHash).
			Uint64("event_nonce", eventNonce).Uint64("finalized_nonce", finalizedNonce).
			Msg("broadcast failed but tx already on chain, marking BROADCASTED")
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Nonce not consumed — transient error (RPC down, gas issues, etc.).
	// Keep as SIGNED and retry next tick.
	b.logger.Debug().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
		Msg("broadcast failed, will retry next tick")
}
