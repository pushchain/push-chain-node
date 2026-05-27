package txresolver

import (
	"context"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/txflow"
)

// svmRevertSlackSeconds is the buffer past the signed deadline before the
// resolver finalizes REVERT. Gives an in-flight tx that's already confirmed
// time to reach `finalized` before we vote against it.
const svmRevertSlackSeconds int64 = 30

// svmClusterStaleSeconds is how far the latest finalized block's timestamp
// can lag wall-clock before the cluster is treated as halted or stalled —
// either case means our `finalized` queries may be missing recently-included
// txs, so we defer REVERT.
const svmClusterStaleSeconds int64 = 120

// resolveSVM checks the on-chain ExecutedTx PDA and moves the event to COMPLETED or REVERTED.
//
// The REVERT decision is gated on the cluster's own clock (latest finalized
// block timestamp returned by IsAlreadyExecuted) rather than the host's local
// clock. This catches three failure modes that would otherwise cause a false
// REVERT: host clock skew, full cluster halt (block time stops advancing),
// and finalization stalls (production continues but finalized lags).
//
//   - PDA exists                                  → COMPLETED.
//   - PDA check RPC error                         → stay BROADCASTED, retry.
//   - PDA absent + cluster time unknown (0)       → stay BROADCASTED, retry.
//   - PDA absent + cluster stale (>120s old)      → stay BROADCASTED, retry.
//   - PDA absent + cluster says still in window   → stay BROADCASTED, retry.
//   - PDA absent + cluster confirms past deadline → REVERT.
func (r *Resolver) resolveSVM(ctx context.Context, event *store.Event, chainID string) {
	log := r.logger.With().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Str("chain_id", chainID).Logger()

	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		log.Warn().Err(err).Msg("failed to extract outbound IDs for SVM resolve")
		return
	}
	log = log.With().Str("tx_id", txID).Logger()

	client, err := r.chains.GetClient(chainID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get chain client for SVM resolve")
		return
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get tx builder for SVM resolve")
		return
	}

	executed, clusterTime, err := builder.IsAlreadyExecuted(ctx, txID)
	if err != nil {
		log.Debug().Err(err).Msg("SVM PDA check failed, will retry next tick")
		return
	}

	if executed {
		if err := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusCompleted}); err != nil {
			log.Warn().Err(err).Msg("failed to mark SVM event COMPLETED")
			return
		}
		log.Info().Msg("event marked as COMPLETED")
		return
	}

	// PDA absent. Decide REVERT using the cluster's own clock so we don't
	// false-revert during halt/stall or host clock skew.
	deadline := txflow.ReadSigningDeadline(event)

	dlog := log.With().Int64("signing_deadline", deadline).Int64("cluster_block_time", clusterTime).Logger()
	switch {
	case clusterTime == 0:
		dlog.Debug().Msg("SVM cluster time unavailable, deferring REVERT decision")
		return
	case time.Now().Unix()-clusterTime > svmClusterStaleSeconds:
		dlog.Warn().Msg("SVM cluster appears stale, deferring REVERT")
		return
	case clusterTime <= deadline+svmRevertSlackSeconds:
		dlog.Debug().Msg("SVM PDA absent but cluster clock still inside deadline window, will retry next tick")
		return
	}

	_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "0", "tx not executed on destination chain")
}
