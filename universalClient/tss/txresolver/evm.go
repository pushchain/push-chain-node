package txresolver

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// resolveEVM checks the on-chain receipt and moves the event to COMPLETED or REVERTED.
//
// EVM resolution flow:
//  1. Look up the tx receipt by hash on the destination chain.
//  2. If NOT FOUND for maxNotFoundRetries consecutive polls (~5 min): vote failure and REVERT.
//     This covers cases where the tx was dropped from the mempool (gas spike, nonce replaced).
//  3. If FOUND but not enough confirmations yet: wait (retry next tick).
//  4. If FOUND with enough confirmations and receipt status == 0 (reverted): vote failure and REVERT.
//  5. If FOUND with enough confirmations and receipt status == 1 (success): mark COMPLETED, success vote will be done by destination chain event listening.
//
// The failure vote triggers a refund of user funds on Push chain.
func (r *Resolver) resolveEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string) {
	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to extract outbound IDs")
		return
	}
	found, confirmations, status, err := r.verifyTxOnChain(ctx, chainID, rawTxHash)
	if err != nil {
		r.logger.Debug().Err(err).Str("event_id", event.EventID).Msg("tx verification error")
		return
	}
	if !found {
		r.notFoundCounts[event.EventID]++
		count := r.notFoundCounts[event.EventID]
		r.logger.Debug().
			Str("event_id", event.EventID).Str("tx_hash", rawTxHash).
			Int("not_found_count", count).Msg("tx not found on chain, will retry")

		if count >= maxNotFoundRetries {
			delete(r.notFoundCounts, event.EventID)
			_ = r.voteFailureAndMarkReverted(ctx, event, txID, utxID, rawTxHash, "tx not found on destination chain after max retries")
		}
		return
	}

	// Tx found — clear any not-found tracking
	delete(r.notFoundCounts, event.EventID)

	requiredConfs := r.chains.GetStandardConfirmations(chainID)
	if confirmations < requiredConfs {
		return // not enough confirmations yet, retry next tick
	}

	// Enough confirmations: finalize based on status
	if status == 0 {
		_ = r.voteFailureAndMarkReverted(ctx, event, txID, utxID, rawTxHash, "tx execution reverted on destination chain")
		return
	}

	// status == 1 (success)
	if err := r.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusCompleted}); err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to mark event COMPLETED")
		return
	}
	r.logger.Info().
		Str("event_id", event.EventID).Str("tx_hash", rawTxHash).
		Uint64("confirmations", confirmations).Msg("broadcasted EVM tx marked COMPLETED")
}

func (r *Resolver) verifyTxOnChain(ctx context.Context, chainID, txHash string) (bool, uint64, uint8, error) {
	client, err := r.chains.GetClient(chainID)
	if err != nil {
		return false, 0, 0, err
	}
	builder, err := client.GetTxBuilder()
	if err != nil {
		return false, 0, 0, err
	}
	return builder.VerifyBroadcastedTx(ctx, txHash)
}
