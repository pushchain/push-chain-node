package txbroadcaster

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// broadcastSVM broadcasts a signed Solana transaction.
//
// With the V2 gateway contract, Solana transactions either land atomically or
// fully revert — there is no partial state. Unlike EVM (where reverted txs still
// consume nonce and land on-chain), a failed Solana CPI means nothing is created
// on-chain (no ExecutedTx PDA, no event emitted).
//
// Flow:
//  1. Broadcast the signed tx
//  2. Success → BROADCASTED with tx hash
//  3. Error → check if ExecutedTx PDA exists on-chain:
//     - PDA exists (another relayer already processed it) → BROADCASTED
//     - PDA not found (permanent failure: bad payload, simulation error) → BROADCASTED
//     with empty tx hash, resolver will verify and REVERT
//     - PDA check fails (RPC truly down) → stay SIGNED, retry next tick
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
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Broadcast failed — check PDA to distinguish permanent vs transient failure.
	executed, execErr := builder.IsAlreadyExecuted(ctx, outboundData.TxID)
	if execErr != nil {
		// RPC truly down (both broadcast and PDA check failed) — stay SIGNED, retry next tick.
		b.logger.Debug().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("SVM broadcast failed and PDA check unreachable, will retry next tick")
		return
	}

	if executed {
		// Another relayer already executed this tx.
		b.logger.Info().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("broadcast failed but tx already executed on-chain, marking BROADCASTED")
		b.markBroadcasted(event, chainID, "")
		return
	}

	// RPC is reachable but PDA not found — permanent failure (bad payload, simulation error).
	// Mark BROADCASTED with empty hash so resolver can verify and REVERT.
	b.logger.Warn().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
		Msg("SVM broadcast failed and PDA not found, marking BROADCASTED for resolver to REVERT")
	b.markBroadcasted(event, chainID, "")
}
