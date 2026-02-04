package eventstore

import (
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// Event statuses for TSS operations
const (
	StatusConfirmed = "CONFIRMED"

	// StatusInProgress - TSS signing is in progress
	StatusInProgress = "IN_PROGRESS"

	// StatusBroadcasted - Transaction sent to external chain (for sign events)
	StatusBroadcasted = "BROADCASTED"

	// StatusCompleted - Successfully completed (key events: vote sent, sign events: confirmed)
	StatusCompleted = "COMPLETED"

	// StatusReverted - Event reverted
	StatusReverted = "REVERTED"

	// StatusExpired - Event expired (for key events)
	StatusExpired = "EXPIRED"
)

// Store provides database access for TSS events.
type Store struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewStore creates a new event store.
func NewStore(db *gorm.DB, logger zerolog.Logger) *Store {
	return &Store{
		db:     db,
		logger: logger.With().Str("component", "event_store").Logger(),
	}
}

// GetConfirmedEvents returns confirmed events that are ready to be processed.
// Events are confirmed if they are at least `minBlockConfirmation` blocks behind the current block and not expired.
// If limit > 0, at most limit events are returned; otherwise all matching events are returned.
func (s *Store) GetConfirmedEvents(currentBlock uint64, minBlockConfirmation uint64, limit int) ([]store.Event, error) {
	var events []store.Event

	// Only get events that are old enough (at least minBlockConfirmation blocks behind)
	minBlock := currentBlock - minBlockConfirmation
	if currentBlock < minBlockConfirmation {
		minBlock = 0
	}

	query := s.db.Where("status = ? AND block_height <= ? AND expiry_block_height > ?",
		StatusConfirmed, minBlock, currentBlock).
		Order("block_height ASC, created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query confirmed events")
	}

	return events, nil
}

// CountInProgress returns the number of events with status IN_PROGRESS (TSS or broadcast in flight).
// Used by the coordinator to cap how many new events to fetch.
func (s *Store) CountInProgress() (int64, error) {
	var count int64
	if err := s.db.Model(&store.Event{}).Where("status = ?", StatusInProgress).Count(&count).Error; err != nil {
		return 0, errors.Wrap(err, "failed to count IN_PROGRESS events")
	}
	return count, nil
}

// GetEvent retrieves an event by ID.
func (s *Store) GetEvent(eventID string) (*store.Event, error) {
	var event store.Event
	if err := s.db.Where("event_id = ?", eventID).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// UpdateStatus updates the status of an event.
// Note: errorMsg is logged but not stored (Event model doesn't have error_msg field).
func (s *Store) UpdateStatus(eventID, status, errorMsg string) error {
	if errorMsg != "" {
		s.logger.Warn().
			Str("event_id", eventID).
			Str("status", status).
			Str("error", errorMsg).
			Msg("updating event status with error")
	}
	result := s.db.Model(&store.Event{}).
		Where("event_id = ?", eventID).
		Update("status", status)
	if result.Error != nil {
		return errors.Wrapf(result.Error, "failed to update event %s", eventID)
	}
	if result.RowsAffected == 0 {
		return errors.Errorf("event %s not found", eventID)
	}
	return nil
}

// UpdateStatusAndBlockHeight updates the status and block height of an event.
func (s *Store) UpdateStatusAndBlockHeight(eventID, status string, blockHeight uint64) error {
	update := map[string]any{
		"status":       status,
		"block_height": blockHeight,
	}
	result := s.db.Model(&store.Event{}).
		Where("event_id = ?", eventID).
		Updates(update)
	if result.Error != nil {
		return errors.Wrapf(result.Error, "failed to update event %s", eventID)
	}
	if result.RowsAffected == 0 {
		return errors.Errorf("event %s not found", eventID)
	}
	return nil
}

// ResetInProgressEventsToConfirmed resets all IN_PROGRESS events to CONFIRMED status.
// This should be called on node startup to handle cases where the node crashed
// while events were in progress, causing sessions to be lost from memory.
func (s *Store) ResetInProgressEventsToConfirmed() (int64, error) {
	result := s.db.Model(&store.Event{}).
		Where("status = ?", StatusInProgress).
		Update("status", StatusConfirmed)
	if result.Error != nil {
		return 0, errors.Wrap(result.Error, "failed to reset IN_PROGRESS events to CONFIRMED")
	}
	if result.RowsAffected > 0 {
		s.logger.Info().
			Int64("reset_count", result.RowsAffected).
			Msg("reset IN_PROGRESS events to CONFIRMED on node startup")
	}
	return result.RowsAffected, nil
}

// CreateEvent stores a new PCEvent. Returns error if event already exists.
func (s *Store) CreateEvent(event *store.Event) error {
	if err := s.db.Create(event).Error; err != nil {
		return errors.Wrapf(err, "failed to create event %s", event.EventID)
	}
	s.logger.Info().
		Str("event_id", event.EventID).
		Str("type", event.Type).
		Uint64("block_height", event.BlockHeight).
		Msg("stored new TSS event")
	return nil
}

// UpdateBroadcastedTxHash updates the BroadcastedTxHash field for an event (used after broadcasting).
func (s *Store) UpdateBroadcastedTxHash(eventID, txHash string) error {
	result := s.db.Model(&store.Event{}).
		Where("event_id = ?", eventID).
		Update("broadcasted_tx_hash", txHash)
	if result.Error != nil {
		return errors.Wrapf(result.Error, "failed to update broadcasted_tx_hash for event %s", eventID)
	}
	if result.RowsAffected == 0 {
		return errors.Errorf("event %s not found", eventID)
	}
	return nil
}
