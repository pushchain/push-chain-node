package txresolver

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

// resolveEVM checks the on-chain receipt and moves the event to COMPLETED or REVERTED.
//
// EVM resolution flow:
//  1. Look up the tx receipt by hash on the destination chain.
//  2. If NOT FOUND for maxNotFoundRetries consecutive polls (~5 min): vote failure and REVERT.
//     This covers cases where the tx was dropped from the mempool (gas spike, nonce replaced).
//  3. If FOUND but not enough confirmations yet: wait (retry next tick).
//  4. If FOUND with enough confirmations and receipt status == 0 (reverted): vote failure and REVERT
//     with the receipt's block height and tx hash.
//  5. If FOUND with enough confirmations and receipt status == 1 (success): mark COMPLETED,
//     success vote will be done by destination chain event listening.
//
// The failure vote triggers a refund of user funds on Push chain.
//
// Observation semantics for the user:
//   - txHash + blockHeight → tx landed on chain (success or revert)
//   - no txHash + no blockHeight → protocol issue (tx dropped, invalid hash, etc.)
func (r *Resolver) resolveEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string) {
	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to extract outbound IDs")
		return
	}
	found, blockHeight, confirmations, status, err := r.verifyTxOnChain(ctx, chainID, rawTxHash)
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
			// Protocol issue: tx dropped/not found — no txHash, no height, no gas used
			_ = r.voteFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "0", "tx not found on destination chain after max retries")
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
		// Destination chain revert — fetch gas used and attach receipt info
		gasFeeUsed := "0"
		if builder, err := r.getBuilder(chainID); err == nil {
			if fee, err := builder.GetGasFeeUsed(ctx, rawTxHash); err == nil {
				gasFeeUsed = fee
			}
		}
		_ = r.voteFailureAndMarkReverted(ctx, event, txID, utxID, rawTxHash, blockHeight, gasFeeUsed, "tx execution reverted on destination chain")
		return
	}

	// status == 1 (success)
	if err := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusCompleted}); err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to mark event COMPLETED")
		return
	}
	r.logger.Info().
		Str("event_id", event.EventID).Str("tx_hash", rawTxHash).
		Uint64("confirmations", confirmations).Msg("broadcasted EVM tx marked COMPLETED")
}

func (r *Resolver) verifyTxOnChain(ctx context.Context, chainID, txHash string) (bool, uint64, uint64, uint8, error) {
	builder, err := r.getBuilder(chainID)
	if err != nil {
		return false, 0, 0, 0, err
	}
	return builder.VerifyBroadcastedTx(ctx, txHash)
}

func (r *Resolver) getBuilder(chainID string) (common.OutboundTxBuilder, error) {
	client, err := r.chains.GetClient(chainID)
	if err != nil {
		return nil, err
	}
	return client.GetTxBuilder()
}
