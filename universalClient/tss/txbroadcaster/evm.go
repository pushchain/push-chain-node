package txbroadcaster

import (
	"context"
	"encoding/hex"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// broadcastEVM broadcasts a signed EVM transaction using nonce-based gating.
//
// The broadcaster only marks BROADCASTED when there is evidence the nonce was
// consumed (successful broadcast or finalized nonce advanced). On any other
// error, the event stays SIGNED and retries next tick. If it stays stuck, the
// coordinator's patience mechanism recovers it via finalized-nonce after ~200s.
//
// Flow:
//  1. Pre-check: if event nonce < finalized nonce → nonce already consumed → BROADCASTED
//  2. Broadcast the signed tx
//  3. Success → BROADCASTED
//  4. Error → re-check finalized nonce:
//     - nonce consumed → BROADCASTED (race: another node got it between step 1 and 2)
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

	eventNonce := data.SigningData.Nonce
	tssAddress := ""
	if b.getTSSAddress != nil {
		tssAddress, _ = b.getTSSAddress(ctx)
	}

	// Step 1: Pre-check — if finalized nonce already advanced past our event nonce,
	// the tx was consumed (by us or another node). No need to broadcast.
	finalizedNonce, err := builder.GetNextNonce(ctx, tssAddress, true)
	if err != nil {
		b.logger.Debug().Err(err).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("failed to get finalized nonce, will try broadcast anyway")
	} else if eventNonce < finalizedNonce {
		b.logger.Info().Str("event_id", event.EventID).Str("chain", chainID).
			Uint64("event_nonce", eventNonce).Uint64("finalized_nonce", finalizedNonce).
			Msg("nonce already consumed, marking BROADCASTED")
		b.markBroadcasted(event, chainID, "")
		return
	}

	// Step 2: Broadcast
	outboundData := data.OutboundCreatedEvent
	txHash, broadcastErr := builder.BroadcastOutboundSigningRequest(ctx, signingReq, &outboundData, signature)

	// Step 3: Success
	if broadcastErr == nil {
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Step 4: Broadcast failed — re-check finalized nonce to see if the nonce was
	// consumed between our pre-check and broadcast attempt (race with another node).
	finalizedNonce, err = builder.GetNextNonce(ctx, tssAddress, true)
	if err == nil && eventNonce < finalizedNonce {
		b.logger.Info().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Uint64("event_nonce", eventNonce).Uint64("finalized_nonce", finalizedNonce).
			Msg("broadcast failed but nonce already consumed, marking BROADCASTED")
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Nonce not consumed — transient error (RPC down, gas issues, etc.).
	// Keep as SIGNED and retry next tick.
	b.logger.Debug().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
		Msg("broadcast failed, will retry next tick")
}
