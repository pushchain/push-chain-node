package txresolver

import (
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// resolveSVM marks Solana transactions as COMPLETED immediately.
// On Solana, a tx either succeeds or reverts, but in both cases we send a tx that
// increments the nonce. This means Solana always emits an on-chain event for both
// success and failure outcomes. Voting (success or failure) is therefore handled
// entirely by destination chain event listening (inbound watcher), so the resolver's
// only job is to move the event out of BROADCASTED.
func (r *Resolver) resolveSVM(event *store.Event, chainID string) {
	_ = r.eventStore.Update(event.EventID, map[string]any{"status": eventstore.StatusCompleted})
	r.logger.Info().Str("event_id", event.EventID).Str("chain_id", chainID).Msg("broadcasted event marked COMPLETED (non-EVM)")
}
