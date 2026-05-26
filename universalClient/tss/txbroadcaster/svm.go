package txbroadcaster

import (
	"context"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// broadcastSVM broadcasts a signed Solana transaction and moves the event to
// its next state.
//
// Three phases, top to bottom:
//
//   1. If local clock is past the signed deadline, check the cluster's own
//      clock (latest finalized block time) before giving up. The cluster
//      clock — not the host clock — is what the gateway program enforces
//      against, so it's the authoritative cutoff.
//   2. Broadcast.
//   3. On broadcast error, check whether a peer landed the same signed tx.
//
// The give-up cutoff is exactly `clusterTime > deadline`. The finalized block
// time lags the on-chain `Clock::unix_timestamp` by ~13s, so by the time our
// reading crosses the deadline the program has already been rejecting new
// attempts. Cluster staleness is not handled here: the resolver gates the
// irreversible REVERT vote on freshness, and falling through to "broadcast"
// is the safe direction if our cluster view is unreliable.
//
// Outcomes:
//   - BROADCASTED(real-hash)  → broadcast succeeded
//   - BROADCASTED("")         → peer landed it, or cluster confirmed expiry
//   - stay SIGNED             → retry next tick
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
	txID := outboundData.TxID
	deadline := data.SigningDeadline
	now := time.Now().Unix()

	// Past local deadline — confirm with the cluster before giving up.
	if deadline > 0 && now > deadline {
		executed, clusterTime, checkErr := builder.IsAlreadyExecuted(ctx, txID)
		log := b.logger.With().
			Str("event_id", event.EventID).Str("chain", chainID).
			Int64("signing_deadline", deadline).Int64("cluster_block_time", clusterTime).Logger()

		switch {
		case checkErr != nil:
			log.Debug().Err(checkErr).Msg("SVM cluster check failed at deadline, retry next tick")
			return
		case executed:
			log.Info().Msg("SVM tx executed by peer past local deadline, marking BROADCASTED")
			b.markBroadcasted(event, chainID, "")
			return
		case clusterTime > deadline:
			log.Warn().Msg("SVM deadline cluster-confirmed expired, marking BROADCASTED for resolver REVERT")
			b.markBroadcasted(event, chainID, "")
			return
		}
		// Cluster says still inside the window (or freshness unknown) — broadcast.
	}

	// Broadcast attempt.
	txHash, broadcastErr := builder.BroadcastOutboundSigningRequest(ctx, signingReq, &outboundData, signature)
	if broadcastErr == nil {
		b.markBroadcasted(event, chainID, txHash)
		return
	}

	// Race: a peer may have landed the same signed tx in the meantime.
	if executed, _, _ := builder.IsAlreadyExecuted(ctx, txID); executed {
		b.logger.Info().Err(broadcastErr).Str("event_id", event.EventID).Str("chain", chainID).
			Msg("SVM broadcast failed but tx executed on chain (race), marking BROADCASTED")
		b.markBroadcasted(event, chainID, "")
		return
	}

	b.logger.Info().Err(broadcastErr).
		Str("event_id", event.EventID).Str("chain", chainID).
		Int64("signing_deadline", deadline).
		Msg("SVM broadcast failed, staying SIGNED for next tick")
}
