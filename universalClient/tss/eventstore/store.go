package eventstore

import (
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

const (
	StatusPending    = "PENDING"
	StatusInProgress = "IN_PROGRESS"
	StatusSuccess    = "SUCCESS"
	StatusExpired    = "EXPIRED"
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
// Events are ready if they are at least `minBlockConfirmation` blocks behind the current block.
func (s *Store) GetPendingEvents(currentBlock uint64, minBlockConfirmation uint64) ([]store.TSSEvent, error) {
	var events []store.TSSEvent

	// Only get events that are old enough (at least minBlockConfirmation blocks behind)
	minBlock := currentBlock - minBlockConfirmation
	if currentBlock < minBlockConfirmation {
		minBlock = 0
	}

	if err := s.db.Where("status = ? AND block_number <= ?", StatusPending, minBlock).
		Order("block_number ASC, created_at ASC").
		Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query pending events")
	}

	// Filter out expired events
	var validEvents []store.TSSEvent
	for _, event := range events {
		if event.ExpiryHeight > 0 && currentBlock > event.ExpiryHeight {
			// Mark as expired
			if err := s.UpdateStatus(event.EventID, StatusExpired, ""); err != nil {
				s.logger.Warn().Err(err).Str("event_id", event.EventID).Msg("failed to mark event as expired")
			}
			continue
		}
		validEvents = append(validEvents, event)
	}

	return validEvents, nil
}

// GetEvent retrieves an event by ID.
func (s *Store) GetEvent(eventID string) (*store.TSSEvent, error) {
	var event store.TSSEvent
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
	result := s.db.Model(&store.TSSEvent{}).
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
func (s *Store) GetEventsByStatus(status string, limit int) ([]store.TSSEvent, error) {
	var events []store.TSSEvent
	query := s.db.Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&events).Error; err != nil {
		return nil, errors.Wrapf(err, "failed to query events with status %s", status)
	}
	return events, nil
}

// ClearExpiredAndSuccessfulEvents deletes both expired and successful events.
func (s *Store) ClearExpiredAndSuccessfulEvents() (int64, error) {
	result := s.db.Where("status IN ?", []string{StatusExpired, StatusSuccess}).Delete(&store.TSSEvent{})
	if result.Error != nil {
		return 0, errors.Wrap(result.Error, "failed to clear expired and successful events")
	}
	s.logger.Info().
		Int64("deleted_count", result.RowsAffected).
		Msg("cleared expired and successful events")
	return result.RowsAffected, nil
}
