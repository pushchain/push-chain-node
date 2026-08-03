package pushwatcher

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/uread"
	"github.com/rs/zerolog"
)

const readProcessBatchSize = 1000

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

// ReadProcessor consumes READ_REQUEST events from the push chain DB, executes
// each request on its destination chain via the resolved handler, and votes
// the result. Transient failures (destination not served, RPC errors, vote
// failure) keep the event CONFIRMED for retry; corrupt events flip to
// REVERTED. Expiry is core's job: expired requests leave the pending query.
type ReadProcessor struct {
	voter      readVoter
	resolver   ChainResolver
	chainStore *common.ChainStore
	cfg        Config
	logger     zerolog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewReadProcessor creates a new read processor.
func NewReadProcessor(
	voter readVoter,
	resolver ChainResolver,
	database *db.DB,
	pollInterval time.Duration,
	logger zerolog.Logger,
) (*ReadProcessor, error) {
	if database == nil {
		return nil, ErrNilDatabase
	}

	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}

	return &ReadProcessor{
		voter:      voter,
		resolver:   resolver,
		chainStore: common.NewChainStore(database),
		cfg:        Config{PollInterval: pollInterval},
		logger:     logger.With().Str("component", "push_read_processor").Logger(),
	}, nil
}

// Start begins processing read request events.
func (p *ReadProcessor) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return ErrAlreadyRunning
	}

	childCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.running = true

	p.logger.Debug().
		Dur("poll_interval", p.cfg.PollInterval).
		Msg("starting read processor")

	p.wg.Add(1)
	go p.run(childCtx)

	return nil
}

// Stop gracefully stops the processor.
func (p *ReadProcessor) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return ErrNotRunning
	}

	p.cancel()
	p.wg.Wait()
	p.running = false

	return nil
}

func (p *ReadProcessor) run(ctx context.Context) {
	defer p.wg.Done()

	p.processConfirmedReads(ctx)

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processConfirmedReads(ctx)
		}
	}
}

// processConfirmedReads executes and votes stored read request events.
func (p *ReadProcessor) processConfirmedReads(ctx context.Context) {
	events, err := p.chainStore.GetConfirmedEvents(readProcessBatchSize)
	if err != nil {
		p.logger.Error().Err(err).Msg("failed to query confirmed events")
		return
	}

	for _, event := range events {
		if event.Type != store.EventTypeReadRequest {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := p.processOne(ctx, &event); err != nil {
			p.logger.Error().
				Err(err).
				Str("event_id", event.EventID).
				Msg("failed to process read request event")
		}
	}
}

func (p *ReadProcessor) processOne(ctx context.Context, event *store.Event) error {
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

func (p *ReadProcessor) markReverted(eventID string) {
	if _, err := p.chainStore.UpdateEventStatus(eventID, store.StatusConfirmed, store.StatusReverted); err != nil {
		p.logger.Error().Err(err).Str("event_id", eventID).Msg("failed to mark read request reverted")
	}
}
