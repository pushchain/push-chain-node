package eventstore

import (
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// Event statuses for TSS operations
const (
	// StatusPending - Event is waiting to be processed (TSS signing not started)
	StatusPending = "PENDING"

	// StatusInProgress - TSS signing is in progress
	StatusInProgress = "IN_PROGRESS"

	// StatusBroadcasted - Transaction sent to external chain (for sign events)
	StatusBroadcasted = "BROADCASTED"

	// StatusCompleted - Successfully completed (key events: vote sent, sign events: confirmed)
	StatusCompleted = "COMPLETED"

	// StatusReverted - Event reverted
	StatusReverted = "REVERTED"
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

// GetPendingEvents returns all pending events that are ready to be processed.
// Events are ready if they are at least `minBlockConfirmation` blocks behind the current block and not expired.
func (s *Store) GetPendingEvents(currentBlock uint64, minBlockConfirmation uint64) ([]store.PCEvent, error) {
	var events []store.PCEvent

	// Only get events that are old enough (at least minBlockConfirmation blocks behind)
	minBlock := currentBlock - minBlockConfirmation
	if currentBlock < minBlockConfirmation {
		minBlock = 0
	}

	// Get pending events that are not expired
	if err := s.db.Where("status = ? AND block_height <= ? AND expiry_block_height > ?",
		StatusPending, minBlock, currentBlock).
		Order("block_height ASC, created_at ASC").
		Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query pending events")
	}

	return events, nil
}

// GetExpiredPendingEvents returns pending events that have expired (expiry_block_height <= currentBlock).
func (s *Store) GetExpiredPendingEvents(currentBlock uint64) ([]store.PCEvent, error) {
	var events []store.PCEvent

	if err := s.db.Where("status = ? AND expiry_block_height <= ?", StatusPending, currentBlock).
		Order("block_height ASC, created_at ASC").
		Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query expired pending events")
	}

	return events, nil
}

// GetEvent retrieves an event by ID.
func (s *Store) GetEvent(eventID string) (*store.PCEvent, error) {
	var event store.PCEvent
	if err := s.db.Where("event_id = ?", eventID).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// UpdateStatus updates the status of an event.
func (s *Store) UpdateStatus(eventID, status, errorMsg string) error {
	update := map[string]any{"status": status}
	if errorMsg != "" {
		update["error_msg"] = errorMsg
	}
	result := s.db.Model(&store.PCEvent{}).
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

// UpdateStatusAndBlockHeight updates the status and block height of an event.
func (s *Store) UpdateStatusAndBlockHeight(eventID, status string, blockHeight uint64) error {
	update := map[string]any{
		"status":       status,
		"block_height": blockHeight,
	}
	result := s.db.Model(&store.PCEvent{}).
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

// GetEventsByStatus returns all events with the given status.
func (s *Store) GetEventsByStatus(status string, limit int) ([]store.PCEvent, error) {
	var events []store.PCEvent
	query := s.db.Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&events).Error; err != nil {
		return nil, errors.Wrapf(err, "failed to query events with status %s", status)
	}
	return events, nil
}

// ClearTerminalEvents deletes completed and reverted events.
func (s *Store) ClearTerminalEvents() (int64, error) {
	result := s.db.Where("status IN ?", []string{StatusCompleted, StatusReverted}).Delete(&store.PCEvent{})
	if result.Error != nil {
		return 0, errors.Wrap(result.Error, "failed to clear expired and completed events")
	}
	s.logger.Info().
		Int64("deleted_count", result.RowsAffected).
		Msg("cleared expired and completed events")
	return result.RowsAffected, nil
}

// ResetInProgressEventsToPending resets all IN_PROGRESS events to PENDING status.
// This should be called on node startup to handle cases where the node crashed
// while events were in progress, causing sessions to be lost from memory.
func (s *Store) ResetInProgressEventsToPending() (int64, error) {
	result := s.db.Model(&store.PCEvent{}).
		Where("status = ?", StatusInProgress).
		Update("status", StatusPending)
	if result.Error != nil {
		return 0, errors.Wrap(result.Error, "failed to reset IN_PROGRESS events to PENDING")
	}
	if result.RowsAffected > 0 {
		s.logger.Info().
			Int64("reset_count", result.RowsAffected).
			Msg("reset IN_PROGRESS events to PENDING on node startup")
	}
	return result.RowsAffected, nil
}

// CreateEvent stores a new PCEvent. Returns error if event already exists.
func (s *Store) CreateEvent(event *store.PCEvent) error {
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

// UpdateTxHash updates the TxHash field for an event (used after broadcasting).
func (s *Store) UpdateTxHash(eventID, txHash string) error {
	result := s.db.Model(&store.PCEvent{}).
		Where("event_id = ?", eventID).
		Update("tx_hash", txHash)
	if result.Error != nil {
		return errors.Wrapf(result.Error, "failed to update tx_hash for event %s", eventID)
	}
	if result.RowsAffected == 0 {
		return errors.Errorf("event %s not found", eventID)
	}
	return nil
}
