package keyshare

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// Quorum change and key refresh are rare, and a retained share is only a
// concern over the long run, so sweeping daily is ample.
const defaultCheckInterval = 24 * time.Hour

// PushCoreClient is the subset of pushcore.Client the sweeper depends on.
// Defined as an interface so tests can inject a mock. *pushcore.Client satisfies it.
type PushCoreClient interface {
	GetCurrentKey(ctx context.Context) (*utsstypes.TssKey, error)
	GetKeyByID(ctx context.Context, keyID string) (*utsstypes.TssKey, error)
	GetPendingTssEvents(ctx context.Context) ([]*utsstypes.TssEvent, error)
	GetPendingFundMigrations(ctx context.Context) ([]*utsstypes.FundMigration, error)
}

// KeyshareStore is the subset of keyshare.Manager the sweeper depends on.
type KeyshareStore interface {
	List() ([]string, error)
	Delete(id string) error
}

// Config holds configuration for the keyshare sweeper.
type Config struct {
	Keyshares     KeyshareStore
	PushCore      PushCoreClient
	CheckInterval time.Duration
	Logger        zerolog.Logger
}

// Sweeper deletes local keyshares that chain state proves are redundant.
//
// A keyshare is deleted only when every one of these holds:
//   - it is not the current key ID;
//   - its TSS pubkey equals the current key's pubkey, i.e. a quorum change or
//     key refresh superseded it while preserving the vault key;
//   - no TSS process is pending, so no in-flight session can still load it;
//   - no pending fund migration references it.
//
// Shares whose pubkey differs from the current one are kept: they belong to a
// rotated-away key that fund migration still needs to sweep its vault. Retiring
// those is an explicit operator action, since a chain that was never migrated is
// indistinguishable from one with nothing to migrate.
//
// Every chain-state lookup fails closed: on error the sweep is skipped and
// retried next tick rather than deleting on incomplete information. Pubkeys are
// resolved per held share rather than from the full key history, which grows
// unbounded and would need paging.
type Sweeper struct {
	keyshares     KeyshareStore
	pushCore      PushCoreClient
	checkInterval time.Duration
	logger        zerolog.Logger
	startOnce     sync.Once
}

// NewSweeper creates a new keyshare sweeper.
func NewSweeper(cfg Config) *Sweeper {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = defaultCheckInterval
	}
	return &Sweeper{
		keyshares:     cfg.Keyshares,
		pushCore:      cfg.PushCore,
		checkInterval: interval,
		logger:        cfg.Logger.With().Str("component", "keyshare_sweeper").Logger(),
	}
}

// Start begins the background sweep loop. Repeat calls are no-ops, so a
// restarted node cannot end up with two sweepers deleting concurrently.
func (s *Sweeper) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		go s.run(ctx)
	})
}

func (s *Sweeper) run(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// Sweep on start: with a long interval, a node restarted more often than
	// that would otherwise never sweep.
	s.sweep(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *Sweeper) sweep(ctx context.Context) {
	if s.keyshares == nil || s.pushCore == nil {
		return
	}

	localIDs, err := s.keyshares.List()
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to list keyshares, skipping sweep")
		return
	}
	if len(localIDs) <= 1 {
		return
	}

	current, err := s.pushCore.GetCurrentKey(ctx)
	if err != nil {
		s.logger.Debug().Err(err).Msg("failed to get current TSS key, skipping sweep")
		return
	}
	if current == nil || current.KeyId == "" || current.TssPubkey == "" {
		return
	}

	// An in-flight session may still load a predecessor share; a missing share
	// is silently reinterpreted as "new party" during quorum change, so wait.
	pendingProcesses, err := s.pushCore.GetPendingTssEvents(ctx)
	if err != nil {
		s.logger.Debug().Err(err).Msg("failed to get pending TSS events, skipping sweep")
		return
	}
	if len(pendingProcesses) > 0 {
		s.logger.Debug().Int("pending", len(pendingProcesses)).Msg("TSS process pending, skipping sweep")
		return
	}

	migrations, err := s.pushCore.GetPendingFundMigrations(ctx)
	if err != nil {
		s.logger.Debug().Err(err).Msg("failed to get pending fund migrations, skipping sweep")
		return
	}
	migrating := make(map[string]bool, len(migrations))
	for _, m := range migrations {
		if m != nil {
			migrating[m.OldKeyId] = true
		}
	}

	deleted := 0
	for _, id := range localIDs {
		if id == current.KeyId || migrating[id] {
			continue
		}
		// Look up only the shares we hold; the on-chain key history is unbounded.
		// Any lookup failure (unknown ID or transport error) keeps the share.
		key, err := s.pushCore.GetKeyByID(ctx, id)
		if err != nil || key == nil {
			s.logger.Debug().Err(err).Str("key_id", id).Msg("cannot resolve keyshare on chain, keeping")
			continue
		}
		if key.TssPubkey != current.TssPubkey {
			continue
		}
		if err := s.keyshares.Delete(id); err != nil {
			s.logger.Warn().Err(err).Str("key_id", id).Msg("failed to delete superseded keyshare")
			continue
		}
		deleted++
		s.logger.Info().Str("key_id", id).Str("current_key_id", current.KeyId).
			Msg("deleted superseded keyshare")
	}

	if deleted > 0 {
		s.logger.Info().Int("deleted", deleted).Msg("keyshare sweep complete")
	}
}
