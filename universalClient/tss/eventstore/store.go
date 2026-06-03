package eventstore

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
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
//	s.Update(id, map[string]any{"status": store.StatusInProgress})
//	s.Update(id, map[string]any{"status": store.StatusConfirmed, "block_height": newHeight})
//	s.Update(id, map[string]any{"broadcasted_tx_hash": txHash})
func (s *Store) Update(eventID string, fields map[string]any) error {
	result := s.db.Model(&store.Event{}).
		Where("event_id = ?", eventID).
		Updates(fields)
	if result.Error != nil {
		return fmt.Errorf("failed to update event %s: %w", eventID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("event %s not found", eventID)
	}
	return nil
}

// PersistSignature merges signing data (signature, hash, nonce, optional fund
// migration amount) onto an event's event_data and flips its status to SIGNED.
//
// Conditional on current status ∈ {CONFIRMED, IN_PROGRESS} — if the row has
// already advanced (SIGNED/BROADCASTED/COMPLETED/REVERTED), the write is a
// no-op. This prevents late writers (a second signature_broadcast arriving
// after the broadcaster already moved the row to BROADCASTED) from clobbering
// downstream progress.
//
// Returns (persisted, error). persisted=false when the status guard skipped
// the write; caller can log and move on.
func (s *Store) PersistSignature(
	eventID string,
	eventData []byte,
	signature []byte,
	signingHash []byte,
	nonce uint64,
	fundMigrationAmount *big.Int,
) (bool, error) {
	signingData := map[string]any{
		"signature":    hex.EncodeToString(signature),
		"signing_hash": hex.EncodeToString(signingHash),
		"nonce":        nonce,
	}
	if fundMigrationAmount != nil && fundMigrationAmount.Sign() > 0 {
		signingData["tss_fund_migration_amount"] = fundMigrationAmount
	}

	var raw map[string]any
	if err := json.Unmarshal(eventData, &raw); err != nil {
		return false, fmt.Errorf("parse event data for signing_data injection: %w", err)
	}
	raw["signing_data"] = signingData
	newEventData, err := json.Marshal(raw)
	if err != nil {
		return false, fmt.Errorf("marshal event data with signing_data: %w", err)
	}

	result := s.db.Model(&store.Event{}).
		Where("event_id = ? AND status IN ?", eventID,
			[]string{store.StatusConfirmed, store.StatusInProgress}).
		Updates(map[string]any{
			"event_data": newEventData,
			"status":     store.StatusSigned,
		})
	if result.Error != nil {
		return false, fmt.Errorf("persist signature for %s: %w", eventID, result.Error)
	}
	return result.RowsAffected > 0, nil
}

// CountInProgress returns the number of events with status IN_PROGRESS.
// Used by the coordinator to cap how many new events to fetch.
func (s *Store) CountInProgress() (int64, error) {
	var count int64
	if err := s.db.Model(&store.Event{}).Where("status = ?", store.StatusInProgress).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count IN_PROGRESS events: %w", err)
	}
	return count, nil
}

// ResetInProgressEventsToConfirmed resets all IN_PROGRESS events to CONFIRMED status.
// Called on node startup to recover from crashes mid-session.
func (s *Store) ResetInProgressEventsToConfirmed() (int64, error) {
	result := s.db.Model(&store.Event{}).
		Where("status = ?", store.StatusInProgress).
		Update("status", store.StatusConfirmed)
	if result.Error != nil {
		return 0, fmt.Errorf("failed to reset IN_PROGRESS events to CONFIRMED: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// GetNonExpiredConfirmedEvents returns confirmed events ready to be processed.
// Events must be at least minBlockConfirmation blocks old and not past expiry.
// expiry_block_height = 0 means "no client-side expiry" and matches always.
func (s *Store) GetNonExpiredConfirmedEvents(currentBlock, minBlockConfirmation uint64, limit int) ([]store.Event, error) {
	var minBlock uint64
	if currentBlock > minBlockConfirmation {
		minBlock = currentBlock - minBlockConfirmation
	}

	query := s.db.Where("status = ? AND block_height <= ? AND (expiry_block_height = 0 OR expiry_block_height > ?)",
		store.StatusConfirmed, minBlock, currentBlock).
		Order("block_height ASC, created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var events []store.Event
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to query confirmed events: %w", err)
	}
	return events, nil
}

// GetInFlightSignEvents returns SIGN events that are currently IN_PROGRESS or SIGNED.
// BROADCASTED events are excluded because they are already submitted to chain;
// the pending nonce RPC accounts for them in the mempool, and including them here
// would make the coordinator wait for the resolver unnecessarily.
func (s *Store) GetInFlightSignEvents() ([]store.Event, error) {
	var events []store.Event
	if err := s.db.Where("type IN (?, ?) AND status IN (?, ?)",
		store.EventTypeSignOutbound, store.EventTypeSignFundMigrate, store.StatusInProgress, store.StatusSigned).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to query in-flight sign events: %w", err)
	}
	return events, nil
}

// GetSignedSignEvents returns SIGN events with status SIGNED (ready to be broadcast).
func (s *Store) GetSignedSignEvents(limit int) ([]store.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	var events []store.Event
	if err := s.db.Where("type IN (?, ?) AND status = ?", store.EventTypeSignOutbound, store.EventTypeSignFundMigrate, store.StatusSigned).
		Order("block_height ASC, created_at ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to query signed sign events: %w", err)
	}
	return events, nil
}

// GetBroadcastedSignEvents returns SIGN events with status BROADCASTED (for receipt check).
func (s *Store) GetBroadcastedSignEvents(limit int) ([]store.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	var events []store.Event
	if err := s.db.Where("type IN (?, ?) AND status = ? AND broadcasted_tx_hash != ?", store.EventTypeSignOutbound, store.EventTypeSignFundMigrate, store.StatusBroadcasted, "").
		Order("block_height ASC, created_at ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to query broadcasted sign events: %w", err)
	}
	return events, nil
}

// DeleteExpiredEvents hard-deletes events past their ExpiryBlockHeight.
// Events with ExpiryBlockHeight = 0 (no client-side expiry, e.g., sign events)
// are not touched. Push chain re-supplies any still-pending event via the
// event listener — local deletion is safe.
func (s *Store) DeleteExpiredEvents(currentBlock uint64) (int64, error) {
	result := s.db.Unscoped().
		Where("expiry_block_height > 0 AND expiry_block_height <= ?", currentBlock).
		Delete(&store.Event{})
	if result.Error != nil {
		return 0, fmt.Errorf("delete expired events: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// DeleteOldUnsignedEvents hard-deletes unsigned events (status CONFIRMED or
// IN_PROGRESS) whose CreatedAt is before cutoff. Events past SIGNED are
// preserved because they carry local commitments (signing_data,
// broadcasted_tx_hash) that must not be lost. If an event we drop is still
// pending on push chain, the push chain pending-tx parser will re-populate
// it on its next poll.
func (s *Store) DeleteOldUnsignedEvents(cutoff time.Time) (int64, error) {
	result := s.db.Unscoped().
		Where("created_at < ? AND status IN ?", cutoff, []string{store.StatusConfirmed, store.StatusInProgress}).
		Delete(&store.Event{})
	if result.Error != nil {
		return 0, fmt.Errorf("delete old unsigned events: %w", result.Error)
	}
	return result.RowsAffected, nil
}
