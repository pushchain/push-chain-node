package txresolver

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// resolveSVM checks the on-chain ExecutedTx PDA and moves the event to COMPLETED or REVERTED.
//
// With the V2 gateway contract, Solana transactions either land atomically or fully revert.
// A failed CPI means nothing is created on-chain (no PDA, no event). The resolver checks
// whether the ExecutedTx PDA exists to determine the outcome:
//
//   - PDA exists  → mark COMPLETED (success vote comes from destination chain event listening)
//   - PDA absent  → vote failure on Push chain and mark REVERTED (triggers user refund)
//   - RPC error   → stay BROADCASTED, retry next tick
func (r *Resolver) resolveSVM(ctx context.Context, event *store.Event, chainID string) {
	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to extract outbound IDs for SVM resolve")
		return
	}

	client, err := r.chains.GetClient(chainID)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Str("chain_id", chainID).
			Msg("failed to get chain client for SVM resolve")
		return
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Str("chain_id", chainID).
			Msg("failed to get tx builder for SVM resolve")
		return
	}

	executed, err := builder.IsAlreadyExecuted(ctx, txID)
	if err != nil {
		// RPC error — stay BROADCASTED, retry next tick
		r.logger.Debug().Err(err).Str("event_id", event.EventID).Str("tx_id", txID).
			Msg("SVM PDA check failed, will retry next tick")
		return
	}

	if executed {
		if err := r.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusCompleted}); err != nil {
			r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to mark SVM event COMPLETED")
			return
		}
		r.logger.Info().Str("event_id", event.EventID).Str("tx_id", txID).Str("chain_id", chainID).
			Msg("SVM ExecutedTx PDA found, marked COMPLETED")
		return
	}

	// PDA not found — tx was not executed on destination chain, no gas consumed
	_ = r.voteFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "0", "tx not executed on destination chain")
}
