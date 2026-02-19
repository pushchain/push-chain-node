package txbroadcaster

import (
	"context"
	"encoding/hex"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// broadcastSVM broadcasts a signed Solana transaction using PDA nonce ordering.
//
// SVM transactions are strictly ordered by PDA nonce — only the tx matching
// the current on-chain nonce can be broadcast. The broadcaster only marks
// BROADCASTED when there is evidence the nonce was consumed (successful broadcast
// or on-chain nonce advanced). On any other error, the event stays SIGNED and
// retries next tick. If it stays stuck, the coordinator's patience mechanism
// recovers it via finalized-nonce after ~200s.
//
// Flow:
//  1. Pre-check: if event nonce < on-chain nonce → nonce already consumed → BROADCASTED
//  2. If event nonce > on-chain nonce → skip (earlier nonce must process first)
//  3. event nonce == on-chain nonce → broadcast
//  4. Success → BROADCASTED
//  5. Error → re-check on-chain nonce:
//     - nonce consumed → BROADCASTED (another node got it between step 1 and 3)
//     - nonce NOT consumed → keep SIGNED, retry next tick
func (b *Broadcaster) broadcastSVM(ctx context.Context, event *store.Event, data *SignedEventData, chainID string) {
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

	// Strip recovery byte if 65 bytes
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

	// Step 1: Pre-check — if on-chain nonce already advanced past our event nonce,
	// the tx was consumed (by us or another node). No need to broadcast.
	onChainNonce, err := builder.GetNextNonce(ctx, "", false)
	if err != nil {
		b.logger.Debug().Err(err).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("failed to get on-chain nonce, will retry next tick")
		return
	}

	if eventNonce < onChainNonce {
		b.logger.Info().Str("event_id", event.EventID).Str("chain", chainID).
			Uint64("event_nonce", eventNonce).Uint64("on_chain_nonce", onChainNonce).
			Msg("nonce already consumed, marking BROADCASTED")
		b.markBroadcasted(event, chainID, "")
		return
	}

	// Step 2: Not our turn — earlier nonce must be processed first
	if eventNonce > onChainNonce {
		b.logger.Debug().Str("event_id", event.EventID).Str("chain", chainID).
			Uint64("event_nonce", eventNonce).Uint64("on_chain_nonce", onChainNonce).
			Msg("waiting for earlier nonce to process first")
		return
	}

	// Step 3: event nonce == on-chain nonce → broadcast
	outboundData := data.OutboundCreatedEvent
	txHash, broadcastErr := builder.BroadcastOutboundSigningRequest(ctx, signingReq, &outboundData, signature)

	// Step 4: Success
	if broadcastErr == nil {
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Step 5: Broadcast failed — re-check on-chain nonce to see if the nonce was
	// consumed between our pre-check and broadcast attempt (race with another node).
	// TODO: on tx revert, broadcast nonceIncrement instruction (sends TSS sig to PDA
	// to increment nonce even on failure). If nonceIncrement also fails, check if
	// nonce already incremented — another node did it.
	onChainNonce, err = builder.GetNextNonce(ctx, "", false)
	if err == nil && eventNonce < onChainNonce {
		b.logger.Info().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Uint64("event_nonce", eventNonce).Uint64("on_chain_nonce", onChainNonce).
			Msg("broadcast failed but nonce already consumed, marking BROADCASTED")
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Nonce not consumed — transient error (RPC down, gas issues, etc.).
	// Keep as SIGNED and retry next tick.
	b.logger.Warn().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
		Uint64("nonce", eventNonce).Msg("broadcast failed, will retry next tick")
}
