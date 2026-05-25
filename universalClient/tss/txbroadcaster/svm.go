package txbroadcaster

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// maxSVMBroadcastAttempts caps the number of failed broadcast attempts before
// the broadcaster gives up and marks the event for REVERT.
//
// TEMPORARY: a future signature-deadline mechanism will supersede this
// attempt-based cap with time-based finality. Until then, this prevents events
// from looping indefinitely on persistent failures (bad payload, downstream
// program upgrade, etc.).
const maxSVMBroadcastAttempts = 10

// broadcastSVM broadcasts a signed Solana transaction.
//
// Three branches:
//   - Success → BROADCASTED with tx hash.
//   - Tx already executed by another relayer → BROADCASTED with empty hash.
//   - Anything else → increment attempt counter; stay SIGNED until the cap is
//     hit, then mark BROADCASTED with empty hash so the resolver can REVERT.
//
// Branch 3 deliberately absorbs every other failure mode (RPC lag, race-lost,
// CPI failure, blockhash stale, transport error, etc.) — we don't classify
// them, we just retry. The attempt cap is the safety valve for genuinely
// stuck events.
func (b *Broadcaster) broadcastSVM(ctx context.Context, event *store.Event, data *SignedOutboundData, chainID string) {
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

	signingReq, signature, err := decodeSigningData(data.SigningData)
	if err != nil {
		b.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to decode signing data")
		return
	}

	outboundData := data.OutboundCreatedEvent
	txHash, broadcastErr := builder.BroadcastOutboundSigningRequest(ctx, signingReq, &outboundData, signature)

	if broadcastErr == nil {
		delete(b.svmBroadcastAttempts, event.EventID)
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Tx may have landed via another relayer — that ends the event cleanly.
	if executed, execErr := builder.IsAlreadyExecuted(ctx, outboundData.TxID); execErr == nil && executed {
		delete(b.svmBroadcastAttempts, event.EventID)
		b.logger.Info().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("broadcast failed but tx already executed on-chain, marking BROADCASTED")
		b.markBroadcasted(event, chainID, "")
		return
	}

	// Broadcast failed and tx isn't on chain. Count the attempt; cap or retry.
	attempts := b.svmBroadcastAttempts[event.EventID] + 1
	if attempts >= maxSVMBroadcastAttempts {
		delete(b.svmBroadcastAttempts, event.EventID)
		b.logger.Warn().Err(broadcastErr).Uint32("attempts", attempts).
			Str("event_id", event.EventID).Str("chain", chainID).
			Msg("SVM broadcast exhausted retry budget, marking BROADCASTED for resolver to REVERT")
		b.markBroadcasted(event, chainID, "")
		return
	}

	b.svmBroadcastAttempts[event.EventID] = attempts
	b.logger.Info().Err(broadcastErr).Uint32("attempts", attempts).
		Str("event_id", event.EventID).Str("chain", chainID).
		Msg("SVM broadcast failed, staying SIGNED for next-tick retry")
}
