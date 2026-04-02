package expirysweeper

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"

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

func setupTestSweeper(t *testing.T) (*Sweeper, *eventstore.Store, *gorm.DB) {
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())
	sweeper := &Sweeper{
		eventStore: evtStore,
		pushSigner: nil, // nil — vote skipped, status update still happens
		logger:     zerolog.Nop(),
	}
	return sweeper, evtStore, db
}

// signEventData returns minimal valid JSON for a SIGN (outbound) event.
func signEventData(t *testing.T, txID, utxID string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"tx_id":             txID,
		"utx_id":            utxID,
		"destination_chain": "ethereum",
	})
	require.NoError(t, err)
	return data
}

// runSweepBatch drives the sweep batch logic directly, bypassing pushCore.GetLatestBlock.
// This mirrors what sweep() does after fetching currentBlock.
func runSweepBatch(t *testing.T, s *Sweeper, currentBlock uint64) {
	t.Helper()
	ctx := context.Background()
	events, err := s.eventStore.GetExpiredConfirmedEvents(currentBlock, sweepBatchSize)
	require.NoError(t, err)
	for _, event := range events {
		ev := event
		if ev.Type == store.EventTypeSignOutbound {
			require.NoError(t, s.voteOutboundFailureAndMarkReverted(ctx, &ev, "event expired before TSS could start"))
		} else if ev.Type == store.EventTypeSignFundMigrate {
			require.NoError(t, s.voteFundMigrationFailureAndMarkReverted(ctx, &ev, "event expired before TSS could start"))
		} else {
			require.NoError(t, s.eventStore.Update(ev.EventID, map[string]any{"status": store.StatusReverted}))
		}
	}
}

func TestSweep(t *testing.T) {
	t.Run("expired CONFIRMED SIGN marked REVERTED (pushSigner nil skips vote)", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		db.Create(&store.Event{EventID: "expired-sign", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: "SIGN_OUTBOUND", EventData: signEventData(t, "tx-1", "utx-1")})

		runSweepBatch(t, sweeper, 100)

		e, _ := evtStore.GetEvent("expired-sign")
		assert.Equal(t, "REVERTED", e.Status)
	})

	t.Run("expired CONFIRMED key event marked REVERTED directly", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		db.Create(&store.Event{EventID: "expired-keygen", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: "KEYGEN"})

		runSweepBatch(t, sweeper, 100)

		e, _ := evtStore.GetEvent("expired-keygen")
		assert.Equal(t, "REVERTED", e.Status)
	})

	t.Run("expired CONFIRMED events become REVERTED, others untouched", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		// Expired CONFIRMED — should be swept
		db.Create(&store.Event{EventID: "expired-keygen", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: "KEYGEN"})
		db.Create(&store.Event{EventID: "expired-sign", BlockHeight: 60, ExpiryBlockHeight: 100,
			Status: "CONFIRMED", Type: "SIGN_OUTBOUND", EventData: signEventData(t, "tx-1", "utx-1")})
		// Non-expired CONFIRMED — unchanged
		db.Create(&store.Event{EventID: "valid-1", BlockHeight: 50, ExpiryBlockHeight: 200,
			Status: "CONFIRMED", Type: "KEYGEN"})
		// Expired non-CONFIRMED — unchanged
		db.Create(&store.Event{EventID: "ip-expired", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "IN_PROGRESS", Type: "KEYGEN"})
		db.Create(&store.Event{EventID: "signed-expired", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "SIGNED", Type: "SIGN_OUTBOUND", EventData: signEventData(t, "tx-2", "utx-2")})
		db.Create(&store.Event{EventID: "broadcasted-expired", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "BROADCASTED", Type: "SIGN_OUTBOUND", EventData: signEventData(t, "tx-3", "utx-3")})

		runSweepBatch(t, sweeper, 100)

		// Expired CONFIRMED → REVERTED
		e1, _ := evtStore.GetEvent("expired-keygen")
		assert.Equal(t, "REVERTED", e1.Status)
		e2, _ := evtStore.GetEvent("expired-sign")
		assert.Equal(t, "REVERTED", e2.Status)

		// Non-expired CONFIRMED → unchanged
		v1, _ := evtStore.GetEvent("valid-1")
		assert.Equal(t, "CONFIRMED", v1.Status)

		// Non-CONFIRMED expired → unchanged
		ip, _ := evtStore.GetEvent("ip-expired")
		assert.Equal(t, "IN_PROGRESS", ip.Status)
		sig, _ := evtStore.GetEvent("signed-expired")
		assert.Equal(t, "SIGNED", sig.Status)
		bc, _ := evtStore.GetEvent("broadcasted-expired")
		assert.Equal(t, "BROADCASTED", bc.Status)
	})

	t.Run("no expired events is a no-op", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		db.Create(&store.Event{EventID: "valid-1", BlockHeight: 50, ExpiryBlockHeight: 200,
			Status: "CONFIRMED", Type: "KEYGEN"})

		runSweepBatch(t, sweeper, 100)

		v1, _ := evtStore.GetEvent("valid-1")
		assert.Equal(t, "CONFIRMED", v1.Status)
	})
}

func TestNewSweeper(t *testing.T) {
	t.Run("default check interval", func(t *testing.T) {
		s := NewSweeper(Config{
			Logger: zerolog.Nop(),
		})
		assert.Equal(t, defaultCheckInterval, s.checkInterval)
	})

	t.Run("custom check interval", func(t *testing.T) {
		s := NewSweeper(Config{
			CheckInterval: 5 * time.Second,
			Logger:        zerolog.Nop(),
		})
		assert.Equal(t, 5*time.Second, s.checkInterval)
	})

	t.Run("all fields set", func(t *testing.T) {
		db := setupTestDB(t)
		evtStore := eventstore.NewStore(db, zerolog.Nop())
		s := NewSweeper(Config{
			EventStore:    evtStore,
			CheckInterval: 10 * time.Second,
			Logger:        zerolog.Nop(),
		})
		assert.Equal(t, 10*time.Second, s.checkInterval)
		assert.NotNil(t, s.eventStore)
		assert.Nil(t, s.pushSigner)
		assert.Nil(t, s.pushCore)
	})
}

func fundMigrationEventData(t *testing.T, migrationID uint64, chain string) []byte {
	t.Helper()
	data, err := json.Marshal(utsstypes.FundMigrationInitiatedEventData{
		MigrationID: migrationID,
		Chain:       chain,
	})
	require.NoError(t, err)
	return data
}

func TestSweep_FundMigration(t *testing.T) {
	t.Run("expired CONFIRMED SIGN_FUND_MIGRATE marked REVERTED (pushSigner nil skips vote)", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		db.Create(&store.Event{
			EventID: "expired-fm", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: store.EventTypeSignFundMigrate,
			EventData: fundMigrationEventData(t, 1, "eip155:1"),
		})

		ctx := context.Background()
		events, err := sweeper.eventStore.GetExpiredConfirmedEvents(100, sweepBatchSize)
		require.NoError(t, err)
		require.Len(t, events, 1)

		ev := events[0]
		require.NoError(t, sweeper.voteFundMigrationFailureAndMarkReverted(ctx, &ev, "event expired"))

		e, _ := evtStore.GetEvent("expired-fm")
		assert.Equal(t, "REVERTED", e.Status)
	})

	t.Run("fund migration with invalid event data returns error", func(t *testing.T) {
		sweeper, _, _ := setupTestSweeper(t)
		ctx := context.Background()

		event := &store.Event{
			EventID:   "bad-fm",
			EventData: []byte("not json"),
		}
		err := sweeper.voteFundMigrationFailureAndMarkReverted(ctx, event, "test error")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
	})
}

func TestSweep_VoteOutboundFailureInvalidJSON(t *testing.T) {
	sweeper, _, _ := setupTestSweeper(t)
	ctx := context.Background()

	event := &store.Event{
		EventID:   "bad-sign",
		EventData: []byte("not json"),
	}
	err := sweeper.voteOutboundFailureAndMarkReverted(ctx, event, "test error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestSweep_FundMigrateViaRunSweepBatch(t *testing.T) {
	t.Run("fund migrate event swept through runSweepBatch", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		db.Create(&store.Event{
			EventID: "fm-batch", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: store.EventTypeSignFundMigrate,
			EventData: fundMigrationEventData(t, 42, "eip155:1"),
		})

		runSweepBatch(t, sweeper, 100)

		e, err := evtStore.GetEvent("fm-batch")
		require.NoError(t, err)
		assert.Equal(t, "REVERTED", e.Status)
	})

	t.Run("mixed outbound and fund migrate events all swept", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		db.Create(&store.Event{
			EventID: "sign-1", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: store.EventTypeSignOutbound,
			EventData: signEventData(t, "tx-1", "utx-1"),
		})
		db.Create(&store.Event{
			EventID: "fm-1", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: store.EventTypeSignFundMigrate,
			EventData: fundMigrationEventData(t, 10, "eip155:137"),
		})
		db.Create(&store.Event{
			EventID: "keygen-1", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: "KEYGEN",
		})

		runSweepBatch(t, sweeper, 100)

		for _, id := range []string{"sign-1", "fm-1", "keygen-1"} {
			e, err := evtStore.GetEvent(id)
			require.NoError(t, err)
			assert.Equal(t, "REVERTED", e.Status, "event %s should be REVERTED", id)
		}
	})
}

func TestStart_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())

	// Use a long check interval so the ticker never fires before we cancel.
	sweeper := NewSweeper(Config{
		EventStore:    evtStore,
		CheckInterval: 10 * time.Second,
		Logger:        zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		// Directly call run (blocking) so we can detect when it returns.
		sweeper.run(ctx)
		close(done)
	}()

	// Cancel immediately; the goroutine should exit via ctx.Done().
	cancel()

	select {
	case <-done:
		// run returned cleanly — pass.
	case <-time.After(2 * time.Second):
		t.Fatal("sweeper.run did not stop after context cancellation")
	}
}

func TestStart_SpawnsGoroutine(t *testing.T) {
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())

	sweeper := NewSweeper(Config{
		EventStore:    evtStore,
		CheckInterval: 10 * time.Second,
		Logger:        zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	sweeper.Start(ctx)

	// Cancel and give the goroutine time to exit.
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestSweep_EmptyPushCore_ReturnsOnError(t *testing.T) {
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())

	// Create a pushcore.Client with no endpoints — GetLatestBlock will return an error.
	emptyCore := &pushcore.Client{}

	sweeper := NewSweeper(Config{
		EventStore:    evtStore,
		PushCore:      emptyCore,
		CheckInterval: 10 * time.Second,
		Logger:        zerolog.Nop(),
	})

	// Insert an expired event to confirm it is NOT swept (because GetLatestBlock fails first).
	db.Create(&store.Event{
		EventID: "should-not-sweep", BlockHeight: 50, ExpiryBlockHeight: 90,
		Status: "CONFIRMED", Type: "KEYGEN",
	})

	sweeper.sweep(context.Background())

	// Event should remain CONFIRMED because sweep returned early on GetLatestBlock error.
	e, err := evtStore.GetEvent("should-not-sweep")
	require.NoError(t, err)
	assert.Equal(t, "CONFIRMED", e.Status)
}

func TestVoteOutboundFailureAndMarkReverted_UpdateFailure(t *testing.T) {
	// Simulate eventStore.Update failure by closing the DB before calling the function.
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())

	sweeper := &Sweeper{
		eventStore: evtStore,
		pushSigner: nil, // vote skipped, but Update still called
		logger:     zerolog.Nop(),
	}

	// Insert an event so Update can find it
	db.Create(&store.Event{
		EventID:   "update-fail",
		Status:    "CONFIRMED",
		Type:      store.EventTypeSignOutbound,
		EventData: signEventData(t, "tx-uf", "utx-uf"),
	})

	// Close the underlying SQL connection to force Update to fail
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.Close()

	err = sweeper.voteOutboundFailureAndMarkReverted(context.Background(),
		&store.Event{
			EventID:   "update-fail",
			EventData: signEventData(t, "tx-uf", "utx-uf"),
		},
		"event expired",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mark event")
}

func TestVoteFundMigrationFailureAndMarkReverted_UpdateFailure(t *testing.T) {
	db := setupTestDB(t)
	evtStore := eventstore.NewStore(db, zerolog.Nop())

	sweeper := &Sweeper{
		eventStore: evtStore,
		pushSigner: nil,
		logger:     zerolog.Nop(),
	}

	db.Create(&store.Event{
		EventID:   "fm-update-fail",
		Status:    "CONFIRMED",
		Type:      store.EventTypeSignFundMigrate,
		EventData: fundMigrationEventData(t, 7, "eip155:1"),
	})

	// Close DB to force Update failure
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.Close()

	err = sweeper.voteFundMigrationFailureAndMarkReverted(context.Background(),
		&store.Event{
			EventID:   "fm-update-fail",
			EventData: fundMigrationEventData(t, 7, "eip155:1"),
		},
		"event expired",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mark event")
}
