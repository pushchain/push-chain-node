package pushwatcher

import (
	"context"
	"encoding/json"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/uread"
	"github.com/rs/zerolog"
)

// ChainResolver resolves a CAIP-2 chain ID to its chain client.
// Satisfied by externalchains.Chains.
type ChainResolver interface {
	GetClient(chainID string) (common.ChainClient, error)
}

// readVoter submits a read observation vote to Push Chain.
// Satisfied by *pushsigner.Signer.
type readVoter interface {
	VoteReadResult(ctx context.Context, requestID string, result *uread.ReadResult) (string, error)
}

// ReadEventProcessor handles READ_REQUEST events: it executes each request on
// its destination chain via the resolved read handler and votes the result.
// Transient failures (destination not served, RPC errors, vote failure) keep
// the event CONFIRMED for retry; corrupt events flip to REVERTED. Expiry is
// core's job: expired requests leave the pending query.
type ReadEventProcessor struct {
	voter      readVoter
	resolver   ChainResolver
	chainStore *common.ChainStore
	logger     zerolog.Logger
}

// NewReadEventProcessor creates the handler for READ_REQUEST events.
func NewReadEventProcessor(
	voter readVoter,
	resolver ChainResolver,
	database *db.DB,
	logger zerolog.Logger,
) (*ReadEventProcessor, error) {
	if database == nil {
		return nil, ErrNilDatabase
	}

	return &ReadEventProcessor{
		voter:      voter,
		resolver:   resolver,
		chainStore: common.NewChainStore(database),
		logger:     logger.With().Str("component", "push_read_event_processor").Logger(),
	}, nil
}

// HandleEvent implements EventHandler for READ_REQUEST events.
func (p *ReadEventProcessor) HandleEvent(ctx context.Context, event *store.Event) error {
	if p.isExpired(event) {
		p.logger.Info().Str("event_id", event.EventID).Msg("read request expired; marking reverted")
		p.markReverted(event.EventID)
		return nil
	}

	var req uread.ReadRequest
	if err := json.Unmarshal(event.EventData, &req); err != nil {
		p.markReverted(event.EventID)
		return err
	}

	log := p.logger.With().Str("request_id", req.RequestID).Logger()

	destClient, err := p.resolver.GetClient(req.DestinationChain)
	if err != nil {
		// destination not served by this validator yet; retry next tick
		log.Debug().Err(err).Str("destination_chain", req.DestinationChain).Msg("destination chain not served")
		return nil
	}

	handler, err := destClient.GetReadRequestHandler()
	if err != nil {
		// destination client not ready to serve reads yet; retry next tick
		log.Debug().Err(err).Str("destination_chain", req.DestinationChain).Msg("read handler not available")
		return nil
	}

	result, err := handler.ExecuteRead(ctx, &req)
	if err != nil {
		log.Debug().Err(err).Str("destination_chain", req.DestinationChain).Msg("read execution failed; will retry")
		return nil
	}

	voteTxHash, err := p.voter.VoteReadResult(ctx, req.RequestID, result)
	if err != nil {
		// TODO(core): ErrVoteReadNotAvailable falls through here until MsgVoteReadResult lands.
		log.Warn().Err(err).Msg("failed to vote read result; will retry")
		return nil
	}

	rowsAffected, err := p.chainStore.UpdateStatusAndVoteTxHash(event.EventID, store.StatusConfirmed, store.StatusCompleted, voteTxHash)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return nil
	}

	log.Info().
		Str("vote_tx_hash", voteTxHash).
		Int32("status", int32(result.Status)).
		Uint64("observed_height", result.ObservedBlockHeight).
		Msg("read request voted")

	return nil
}

// isExpired reports whether the request's expiry Push chain height has been
// reached, using the chain height persisted by the event listener.
func (p *ReadEventProcessor) isExpired(event *store.Event) bool {
	if event.ExpiryBlockHeight == 0 {
		return false
	}
	pushHeight, err := p.chainStore.GetChainHeight()
	if err != nil {
		return false
	}
	return pushHeight >= event.ExpiryBlockHeight
}

func (p *ReadEventProcessor) markReverted(eventID string) {
	if _, err := p.chainStore.UpdateEventStatus(eventID, store.StatusConfirmed, store.StatusReverted); err != nil {
		p.logger.Error().Err(err).Str("event_id", eventID).Msg("failed to mark read request reverted")
	}
}
