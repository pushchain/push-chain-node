package txresolver

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// txCheckResult represents the outcome of verifying a tx on chain with not-found retry handling.
type txCheckResult int

const (
	txCheckRetry        txCheckResult = iota // tx not found or not enough confirmations, retry later
	txCheckMaxRetries                        // tx not found after max retries
	txCheckReverted                          // tx found, confirmed, status=0
	txCheckSuccess                           // tx found, confirmed, status=1
)

// checkEVMTx verifies a tx on chain and handles the not-found retry counter.
// Returns the check result, block height, and raw tx hash for further processing.
func (r *Resolver) checkEVMTx(ctx context.Context, event *store.Event, chainID, rawTxHash string) (txCheckResult, uint64) {
	found, blockHeight, confirmations, status, err := r.verifyTxOnChain(ctx, chainID, rawTxHash)
	if err != nil {
		r.logger.Debug().Err(err).Str("event_id", event.EventID).Msg("tx verification error")
		return txCheckRetry, 0
	}

	if !found {
		r.notFoundCounts[event.EventID]++
		count := r.notFoundCounts[event.EventID]
		r.logger.Debug().
			Str("event_id", event.EventID).Str("tx_hash", rawTxHash).
			Int("not_found_count", count).Msg("tx not found on chain, will retry")

		if count >= maxNotFoundRetries {
			delete(r.notFoundCounts, event.EventID)
			return txCheckMaxRetries, 0
		}
		return txCheckRetry, 0
	}

	delete(r.notFoundCounts, event.EventID)

	requiredConfs := r.chains.GetStandardConfirmations(chainID)
	if confirmations < requiredConfs {
		return txCheckRetry, 0
	}

	if status == 0 {
		return txCheckReverted, blockHeight
	}

	return txCheckSuccess, blockHeight
}

// resolveOutboundEVM checks the on-chain receipt for an outbound EVM tx.
// Success vote is done by destination chain event listener, not here.
func (r *Resolver) resolveOutboundEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string) {
	txID, utxID, err := extractOutboundIDs(event)
	if err != nil {
		r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to extract outbound IDs")
		return
	}

	result, blockHeight := r.checkEVMTx(ctx, event, chainID, rawTxHash)

	switch result {
	case txCheckRetry:
		return

	case txCheckMaxRetries:
		_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, "", 0, "0",
			"tx not found on destination chain after max retries")

	case txCheckReverted:
		gasFeeUsed := "0"
		if builder, err := r.getBuilder(chainID); err == nil {
			if fee, err := builder.GetGasFeeUsed(ctx, rawTxHash); err == nil {
				gasFeeUsed = fee
			}
		}
		_ = r.voteOutboundFailureAndMarkReverted(ctx, event, txID, utxID, rawTxHash, blockHeight, gasFeeUsed,
			"tx execution reverted on destination chain")

	case txCheckSuccess:
		// Success vote done by destination chain event listener
		if err := r.eventStore.Update(event.EventID, map[string]any{"status": store.StatusCompleted}); err != nil {
			r.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to mark event COMPLETED")
			return
		}
		r.logger.Info().
			Str("event_id", event.EventID).Str("tx_hash", rawTxHash).
			Msg("outbound EVM tx marked COMPLETED")
	}
}

// resolveFundMigrationEVM checks the on-chain receipt for a fund migration EVM tx.
// Votes success/failure explicitly since there is no gateway event listener for native transfers.
func (r *Resolver) resolveFundMigrationEVM(ctx context.Context, event *store.Event, chainID, rawTxHash string, migrationID uint64) {
	result, _ := r.checkEVMTx(ctx, event, chainID, rawTxHash)

	switch result {
	case txCheckRetry:
		return
	case txCheckMaxRetries:
		r.voteFundMigrationAndMark(ctx, event, migrationID, "", false)
	case txCheckReverted:
		r.voteFundMigrationAndMark(ctx, event, migrationID, rawTxHash, false)
	case txCheckSuccess:
		r.voteFundMigrationAndMark(ctx, event, migrationID, rawTxHash, true)
	}
}
