package expirysweeper

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&store.Event{}))
	return db
}

func setupTestSweeper(t *testing.T, maxAge time.Duration) (*Sweeper, *eventstore.Store, *gorm.DB) {
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())
	if maxAge == 0 {
		maxAge = defaultMaxEventAge
	}
	sweeper := &Sweeper{
		eventStore:    evtStore,
		pushCore:      &pushcore.Client{}, // empty client — RPC calls return errors (not panic)
		checkInterval: defaultCheckInterval,
		maxEventAge:   maxAge,
		logger:        zerolog.Nop(),
	}
	return sweeper, evtStore, db
}

func TestNewSweeper(t *testing.T) {
	t.Run("zero values get defaults", func(t *testing.T) {
		s := NewSweeper(Config{
			EventStore: eventstore.NewStore(setupTestDB(t), zerolog.Nop()),
			PushCore:   &pushcore.Client{},
			Logger:     zerolog.Nop(),
		})
		assert.Equal(t, defaultCheckInterval, s.checkInterval)
		assert.Equal(t, defaultMaxEventAge, s.maxEventAge)
	})

	t.Run("custom values are respected", func(t *testing.T) {
		s := NewSweeper(Config{
			EventStore:    eventstore.NewStore(setupTestDB(t), zerolog.Nop()),
			PushCore:      &pushcore.Client{},
			CheckInterval: 5 * time.Second,
			MaxEventAge:   10 * time.Minute,
			Logger:        zerolog.Nop(),
		})
		assert.Equal(t, 5*time.Second, s.checkInterval)
		assert.Equal(t, 10*time.Minute, s.maxEventAge)
	})
}

func TestDeleteExpiredEvents(t *testing.T) {
	t.Run("KEY event past ExpiryBlockHeight is deleted", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		require.NoError(t, db.Create(&store.Event{
			EventID:           "keygen-expired",
			BlockHeight:       50,
			ExpiryBlockHeight: 90,
			Type:              store.EventTypeKeygen,
			Status:            store.StatusConfirmed,
		}).Error)

		n, err := evtStore.DeleteExpiredEvents(100)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)

		_, err = evtStore.GetEvent("keygen-expired")
		require.Error(t, err, "deleted event must not be retrievable")
	})

	t.Run("sign event with ExpiryBlockHeight=0 is preserved", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		require.NoError(t, db.Create(&store.Event{
			EventID:           "sign-noexp",
			BlockHeight:       50,
			ExpiryBlockHeight: 0,
			Type:              store.EventTypeSignOutbound,
			Status:            store.StatusConfirmed,
		}).Error)

		n, err := evtStore.DeleteExpiredEvents(1_000_000)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n, "expiry_block_height=0 must skip the block-based deletion path")

		got, err := evtStore.GetEvent("sign-noexp")
		require.NoError(t, err)
		assert.Equal(t, "sign-noexp", got.EventID)
	})

	t.Run("KEY event before ExpiryBlockHeight is preserved", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		require.NoError(t, db.Create(&store.Event{
			EventID:           "keygen-future",
			BlockHeight:       50,
			ExpiryBlockHeight: 200,
			Type:              store.EventTypeKeygen,
			Status:            store.StatusConfirmed,
		}).Error)

		n, err := evtStore.DeleteExpiredEvents(100)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)

		got, err := evtStore.GetEvent("keygen-future")
		require.NoError(t, err)
		assert.Equal(t, "keygen-future", got.EventID)
	})
}

func TestDeleteOldUnsignedEvents(t *testing.T) {
	backdate := func(t *testing.T, db *gorm.DB, eventID string, age time.Duration) {
		t.Helper()
		require.NoError(t, db.Model(&store.Event{}).Where("event_id = ?", eventID).
			Update("created_at", time.Now().Add(-age)).Error)
	}

	t.Run("old CONFIRMED event is deleted", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		require.NoError(t, db.Create(&store.Event{
			EventID: "old-confirmed", BlockHeight: 50,
			Type: store.EventTypeSignOutbound, Status: store.StatusConfirmed,
		}).Error)
		backdate(t, db, "old-confirmed", 2*time.Hour)

		n, err := evtStore.DeleteOldUnsignedEvents(time.Now().Add(-1 * time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)

		_, err = evtStore.GetEvent("old-confirmed")
		require.Error(t, err)
	})

	t.Run("old IN_PROGRESS event is deleted", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		require.NoError(t, db.Create(&store.Event{
			EventID: "old-inprog", BlockHeight: 50,
			Type: store.EventTypeSignOutbound, Status: store.StatusInProgress,
		}).Error)
		backdate(t, db, "old-inprog", 2*time.Hour)

		n, err := evtStore.DeleteOldUnsignedEvents(time.Now().Add(-1 * time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)

		_, err = evtStore.GetEvent("old-inprog")
		require.Error(t, err)
	})

	t.Run("old SIGNED event is preserved (carries local commitment)", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		require.NoError(t, db.Create(&store.Event{
			EventID: "old-signed", BlockHeight: 50,
			Type: store.EventTypeSignOutbound, Status: store.StatusSigned,
		}).Error)
		backdate(t, db, "old-signed", 2*time.Hour)

		n, err := evtStore.DeleteOldUnsignedEvents(time.Now().Add(-1 * time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)

		got, err := evtStore.GetEvent("old-signed")
		require.NoError(t, err)
		assert.Equal(t, "old-signed", got.EventID)
	})

	t.Run("old terminal-state events are preserved", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		for _, status := range []string{store.StatusBroadcasted, store.StatusCompleted, store.StatusReverted} {
			require.NoError(t, db.Create(&store.Event{
				EventID: "old-" + status, BlockHeight: 50,
				Type: store.EventTypeSignOutbound, Status: status,
			}).Error)
			backdate(t, db, "old-"+status, 2*time.Hour)
		}

		n, err := evtStore.DeleteOldUnsignedEvents(time.Now().Add(-1 * time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)

		for _, status := range []string{store.StatusBroadcasted, store.StatusCompleted, store.StatusReverted} {
			_, err := evtStore.GetEvent("old-" + status)
			assert.NoError(t, err, "status %s must be preserved", status)
		}
	})

	t.Run("recent CONFIRMED event is preserved", func(t *testing.T) {
		_, evtStore, db := setupTestSweeper(t, 0)
		require.NoError(t, db.Create(&store.Event{
			EventID: "recent", BlockHeight: 50,
			Type: store.EventTypeSignOutbound, Status: store.StatusConfirmed,
		}).Error)
		// CreatedAt is set to now() by GORM.

		n, err := evtStore.DeleteOldUnsignedEvents(time.Now().Add(-1 * time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)

		got, err := evtStore.GetEvent("recent")
		require.NoError(t, err)
		assert.Equal(t, "recent", got.EventID)
	})
}

func TestStart_ContextCancellation(t *testing.T) {
	sweeper, _, _ := setupTestSweeper(t, 0)
	sweeper.checkInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	sweeper.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()
	time.Sleep(30 * time.Millisecond)
	// no panic, no hang — pass
}
