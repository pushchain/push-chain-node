package common

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

// ChainStore provides database operations for chain state and events
type ChainStore struct {
	database *db.DB
}

// NewChainStore creates a new chain store
func NewChainStore(database *db.DB) *ChainStore {
	return &ChainStore{
		database: database,
	}
}

// GetChainHeight returns the last processed block height for the chain
// Creates a new entry with height 0 if it doesn't exist
func (cs *ChainStore) GetChainHeight() (uint64, error) {
	if cs.database == nil {
		return 0, fmt.Errorf("database is nil")
	}

	var state store.State
	result := cs.database.Client().First(&state)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// Create new entry with height 0
			state = store.State{
				BlockHeight: 0,
			}
			if err := cs.database.Client().Create(&state).Error; err != nil {
				return 0, fmt.Errorf("failed to create chain state: %w", err)
			}
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get chain height: %w", result.Error)
	}

	return state.BlockHeight, nil
}

// UpdateChainHeight updates the last processed block height for the chain
// Creates a new entry if it doesn't exist
func (cs *ChainStore) UpdateChainHeight(blockHeight uint64) error {
	if cs.database == nil {
		return fmt.Errorf("database is nil")
	}

	var state store.State
	result := cs.database.Client().First(&state)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// Create new entry
			state = store.State{
				BlockHeight: blockHeight,
			}
			if err := cs.database.Client().Create(&state).Error; err != nil {
				return fmt.Errorf("failed to create chain state: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to query chain state: %w", result.Error)
	}

	// Update existing record only if new block is higher
	if blockHeight > state.BlockHeight {
		state.BlockHeight = blockHeight
		if err := cs.database.Client().Save(&state).Error; err != nil {
			return fmt.Errorf("failed to update chain height: %w", err)
		}
	}

	return nil
}

// GetPendingEvents fetches pending events ordered by creation time
func (cs *ChainStore) GetPendingEvents(limit int) ([]store.Event, error) {
	if cs.database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var events []store.Event
	if err := cs.database.Client().
		Where("status = ?", "PENDING").
		Order("created_at ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to query pending events: %w", err)
	}

	return events, nil
}

// GetConfirmedEvents fetches confirmed events ordered by creation time
func (cs *ChainStore) GetConfirmedEvents(limit int) ([]store.Event, error) {
	if cs.database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var events []store.Event
	if err := cs.database.Client().
		Where("status = ?", "CONFIRMED").
		Order("created_at ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to query confirmed events: %w", err)
	}

	return events, nil
}

// UpdateEventStatus updates the status of an event by event ID
func (cs *ChainStore) UpdateEventStatus(eventID string, oldStatus, newStatus string) (int64, error) {
	if cs.database == nil {
		return 0, fmt.Errorf("database is nil")
	}

	res := cs.database.Client().
		Model(&store.Event{}).
		Where("event_id = ? AND status = ?", eventID, oldStatus).
		Update("status", newStatus)

	if res.Error != nil {
		return 0, fmt.Errorf("failed to update event status: %w", res.Error)
	}

	return res.RowsAffected, nil
}

// UpdateVoteTxHash updates the vote_tx_hash field for an event
func (cs *ChainStore) UpdateVoteTxHash(eventID string, voteTxHash string) error {
	if cs.database == nil {
		return fmt.Errorf("database is nil")
	}

	result := cs.database.Client().
		Model(&store.Event{}).
		Where("event_id = ?", eventID).
		Update("vote_tx_hash", voteTxHash)

	if result.Error != nil {
		return fmt.Errorf("failed to update vote_tx_hash: %w", result.Error)
	}

	return nil
}

// DeleteCompletedEvents deletes completed events updated before the given time
func (cs *ChainStore) DeleteCompletedEvents(updatedBefore interface{}) (int64, error) {
	if cs.database == nil {
		return 0, fmt.Errorf("database is nil")
	}

	res := cs.database.Client().
		Where("status = ? AND updated_at < ?", "COMPLETED", updatedBefore).
		Delete(&store.Event{})

	if res.Error != nil {
		return 0, fmt.Errorf("failed to delete events: %w", res.Error)
	}

	return res.RowsAffected, nil
}

// GetExpiredEvents returns events that have expired (expiry_block_height <= currentBlock)
// and are still in a non-terminal state (PENDING, CONFIRMED, BROADCASTED, IN_PROGRESS)
func (cs *ChainStore) GetExpiredEvents(currentBlock uint64, limit int) ([]store.Event, error) {
	if cs.database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var events []store.Event
	if err := cs.database.Client().
		Where("status IN ? AND expiry_block_height > 0 AND expiry_block_height <= ?",
			[]string{"PENDING", "CONFIRMED", "BROADCASTED", "IN_PROGRESS"}, currentBlock).
		Order("created_at ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to query expired events: %w", err)
	}

	return events, nil
}

// DeleteTerminalEvents deletes events in terminal states (COMPLETED, REVERTED, EXPIRED)
// that were updated before the given time
func (cs *ChainStore) DeleteTerminalEvents(updatedBefore interface{}) (int64, error) {
	if cs.database == nil {
		return 0, fmt.Errorf("database is nil")
	}

	res := cs.database.Client().
		Where("status IN ? AND updated_at < ?",
			[]string{"COMPLETED", "REVERTED", "EXPIRED"}, updatedBefore).
		Delete(&store.Event{})

	if res.Error != nil {
		return 0, fmt.Errorf("failed to delete terminal events: %w", res.Error)
	}

	return res.RowsAffected, nil
}

// InsertEventIfNotExists inserts an event if it doesn't already exist (by EventID)
// Returns (true, nil) if a new event was inserted, (false, nil) if it already existed,
// or (false, error) if insertion failed
func (cs *ChainStore) InsertEventIfNotExists(event *store.Event) (bool, error) {
	if cs.database == nil {
		return false, fmt.Errorf("database is nil")
	}

	// Check for existing event
	var existing store.Event
	err := cs.database.Client().Where("event_id = ?", event.EventID).First(&existing).Error
	if err == nil {
		// Event already exists
		return false, nil
	}
	if err != gorm.ErrRecordNotFound {
		return false, fmt.Errorf("failed to check existing event: %w", err)
	}

	// Store new event
	if err := cs.database.Client().Create(event).Error; err != nil {
		return false, fmt.Errorf("failed to create event: %w", err)
	}

	return true, nil
}
