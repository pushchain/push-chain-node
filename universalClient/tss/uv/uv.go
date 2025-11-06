package uv

import (
	"context"
	"sort"
	"time"

	"github.com/rs/zerolog"
)

// Manager handles Universal Validator fetching, caching, and coordinator selection
type Manager struct {
	ctx    context.Context
	logger zerolog.Logger

	// Cached validators
	validators []*UniversalValidator

	// Configuration
	refreshInterval      time.Duration
	coordinatorRangeSize int64
	myValidatorAddress   string
}

// New creates a new UV manager instance
func New(ctx context.Context, logger zerolog.Logger, myValidatorAddress string, coordinatorRangeSize int64) *Manager {
	return &Manager{
		ctx:                  ctx,
		logger:               logger.With().Str("component", "tss_uv").Logger(),
		myValidatorAddress:   myValidatorAddress,
		coordinatorRangeSize: coordinatorRangeSize,
		refreshInterval:      30 * time.Second, // Default refresh every 30 seconds
	}
}

// Start begins the background refresh loop
func (m *Manager) Start() error {
	// Initial fetch
	if err := m.Refresh(); err != nil {
		m.logger.Warn().Err(err).Msg("failed to fetch validators on startup")
	}

	// Start background refresh loop
	go m.refreshLoop()

	m.logger.Info().Msg("UV manager started")
	return nil
}

// refreshLoop continuously refreshes UV set at configured interval
func (m *Manager) refreshLoop() {
	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			m.logger.Info().Msg("UV refresh loop stopped")
			return
		case <-ticker.C:
			if err := m.Refresh(); err != nil {
				m.logger.Error().Err(err).Msg("failed to refresh validators")
			}
		}
	}
}

// GetUVs returns the current cached list of Universal Validators
func (m *Manager) GetUVs() []*UniversalValidator {
	// TODO: Add mutex for thread-safe access
	return m.validators
}

// Refresh fetches the latest UV set from chain and updates cache
func (m *Manager) Refresh() error {
	// TODO: Fetch validators from Push Chain
	// 1. Query on-chain Universal Validator registry
	// 2. Parse response into []*tss.UniversalValidator
	// 3. Update m.validators cache

	m.logger.Debug().Msg("refreshing UV set")
	return nil
}

// GetCoordinator returns the coordinator Universal Validator for a given block number
func (m *Manager) GetCoordinator(blockNum int64) (*UniversalValidator, error) {
	validators := m.GetUVs()
	return m.getCoordinator(blockNum, validators)
}

// IsCoordinator checks if this node is the coordinator for the given block
func (m *Manager) IsCoordinator(blockNum int64) (bool, error) {
	coordinator, err := m.GetCoordinator(blockNum)
	if err != nil {
		return false, err
	}
	return m.myValidatorAddress == coordinator.ValidatorAddress, nil
}

// getCoordinator returns the coordinator Universal Validator for a given block number
// Coordinator selection uses ACTIVE + PENDING_JOIN validators
func (m *Manager) getCoordinator(
	blockNum int64,
	allValidators []*UniversalValidator,
) (*UniversalValidator, error) {
	if blockNum < 0 {
		return nil, ErrInvalidBlockNumber
	}

	// Filter to ACTIVE + PENDING_JOIN validators for coordinator selection
	eligibleValidators := m.filterCoordinatorEligible(allValidators)
	if len(eligibleValidators) == 0 {
		return nil, ErrNoEligibleValidators
	}

	// Sort by validator_address for deterministic ordering
	sort.Slice(eligibleValidators, func(i, j int) bool {
		return eligibleValidators[i].ValidatorAddress < eligibleValidators[j].ValidatorAddress
	})

	// Calculate epoch (which range/period this block belongs to)
	epoch := blockNum / m.coordinatorRangeSize

	// Use epoch modulo to select coordinator index
	coordinatorIdx := int(epoch % int64(len(eligibleValidators)))

	return eligibleValidators[coordinatorIdx], nil
}

// filterCoordinatorEligible filters validators eligible for coordinator selection
// Coordinator selection uses ACTIVE + PENDING_JOIN validators
func (m *Manager) filterCoordinatorEligible(
	allValidators []*UniversalValidator,
) []*UniversalValidator {
	eligible := make([]*UniversalValidator, 0, len(allValidators))
	for _, uv := range allValidators {
		if uv.Status == UVStatusActive || uv.Status == UVStatusPendingJoin {
			eligible = append(eligible, uv)
		}
	}
	return eligible
}
