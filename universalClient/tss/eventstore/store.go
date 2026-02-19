package eventstore

import (
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
)

// Event statuses for TSS operations.
//
// Lifecycle: CONFIRMED → IN_PROGRESS → SIGNED → BROADCASTED → COMPLETED
//
//	                                  ↘ REVERTED (on expiry, receipt failed, or key vote failed)
const (
	StatusConfirmed   = "CONFIRMED"   // Event confirmed on Push chain, ready for processing
	StatusInProgress  = "IN_PROGRESS" // TSS signing is in progress
	StatusSigned      = "SIGNED"      // TSS signing done, tx not yet broadcast (sign events only)
	StatusBroadcasted = "BROADCASTED" // Transaction sent to external chain (sign events only)
	StatusReverted    = "REVERTED"    // Reverted (failure vote sent for sign events, or key vote failed)
	StatusCompleted   = "COMPLETED"   // Successfully completed
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

// GetEvent retrieves an event by ID.
func (s *Store) GetEvent(eventID string) (*store.Event, error) {
	var event store.Event
	if err := s.db.Where("event_id = ?", eventID).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// Update applies field updates to an event by ID.
// Returns an error if the event is not found.
//
// Example usage:
//
//	s.Update(id, map[string]any{"status": StatusInProgress})
//	s.Update(id, map[string]any{"status": StatusConfirmed, "block_height": newHeight})
//	s.Update(id, map[string]any{"broadcasted_tx_hash": txHash})
func (s *Store) Update(eventID string, fields map[string]any) error {
	result := s.db.Model(&store.Event{}).
		Where("event_id = ?", eventID).
		Updates(fields)
	if result.Error != nil {
		return errors.Wrapf(result.Error, "failed to update event %s", eventID)
	}
	if result.RowsAffected == 0 {
		return errors.Errorf("event %s not found", eventID)
	}
	return nil
}

// CountInProgress returns the number of events with status IN_PROGRESS.
// Used by the coordinator to cap how many new events to fetch.
func (s *Store) CountInProgress() (int64, error) {
	var count int64
	if err := s.db.Model(&store.Event{}).Where("status = ?", StatusInProgress).Count(&count).Error; err != nil {
		return 0, errors.Wrap(err, "failed to count IN_PROGRESS events")
	}
	return count, nil
}

// ResetInProgressEventsToConfirmed resets all IN_PROGRESS events to CONFIRMED status.
// Called on node startup to recover from crashes mid-session.
func (s *Store) ResetInProgressEventsToConfirmed() (int64, error) {
	result := s.db.Model(&store.Event{}).
		Where("status = ?", StatusInProgress).
		Update("status", StatusConfirmed)
	if result.Error != nil {
		return 0, errors.Wrap(result.Error, "failed to reset IN_PROGRESS events to CONFIRMED")
	}
	return result.RowsAffected, nil
}

// GetNonExpiredConfirmedEvents returns confirmed events ready to be processed.
// Events must be at least minBlockConfirmation blocks old and not past expiry.
func (s *Store) GetNonExpiredConfirmedEvents(currentBlock, minBlockConfirmation uint64, limit int) ([]store.Event, error) {
	var minBlock uint64
	if currentBlock > minBlockConfirmation {
		minBlock = currentBlock - minBlockConfirmation
	}

	query := s.db.Where("status = ? AND block_height <= ? AND expiry_block_height > ?",
		StatusConfirmed, minBlock, currentBlock).
		Order("block_height ASC, created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var events []store.Event
	if err := query.Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query confirmed events")
	}
	return events, nil
}

// GetInFlightSignEvents returns SIGN events that are currently IN_PROGRESS or SIGNED.
// BROADCASTED events are excluded because they are already submitted to chain;
// the pending nonce RPC accounts for them in the mempool, and including them here
// would make the coordinator wait for the resolver unnecessarily.
func (s *Store) GetInFlightSignEvents() ([]store.Event, error) {
	var events []store.Event
	if err := s.db.Where("type = ? AND status IN (?, ?)",
		"SIGN", StatusInProgress, StatusSigned).
		Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query in-flight sign events")
	}
	return events, nil
}

// GetSignedSignEvents returns SIGN events with status SIGNED (ready to be broadcast).
func (s *Store) GetSignedSignEvents(limit int) ([]store.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	var events []store.Event
	if err := s.db.Where("type = ? AND status = ?", "SIGN", StatusSigned).
		Order("block_height ASC, created_at ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query signed sign events")
	}
	return events, nil
}

// GetBroadcastedSignEvents returns SIGN events with status BROADCASTED (for receipt check).
func (s *Store) GetBroadcastedSignEvents(limit int) ([]store.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	var events []store.Event
	if err := s.db.Where("type = ? AND status = ? AND broadcasted_tx_hash != ?", "SIGN", StatusBroadcasted, "").
		Order("block_height ASC, created_at ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query broadcasted sign events")
	}
	return events, nil
}

// GetExpiredConfirmedEvents returns CONFIRMED events past their expiry block.
func (s *Store) GetExpiredConfirmedEvents(currentBlock uint64, limit int) ([]store.Event, error) {
	query := s.db.Where("status = ? AND expiry_block_height <= ?",
		StatusConfirmed, currentBlock).
		Order("block_height ASC, created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var events []store.Event
	if err := query.Find(&events).Error; err != nil {
		return nil, errors.Wrap(err, "failed to query expired confirmed events")
	}
	return events, nil
}
