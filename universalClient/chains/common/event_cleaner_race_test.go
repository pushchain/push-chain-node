package common

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Start and Stop race against the cleanup goroutine. Run under -race.
func TestEventCleaner_StartStopUnderRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		database := newTestCleanerDB(t, nil)
		cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
		// Fast enough that the goroutine is inside performCleanup while Stop runs.
		cleaner.cleanupInterval = time.Millisecond

		require.NoError(t, cleaner.Start(context.Background()))
		cleaner.Stop()
	}
}

// Concurrent Stop calls must not double close the channel or return before the
// goroutine has exited.
func TestEventCleaner_ConcurrentStop(t *testing.T) {
	database := newTestCleanerDB(t, nil)
	cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
	cleaner.cleanupInterval = time.Millisecond

	require.NoError(t, cleaner.Start(context.Background()))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cleaner.Stop()
		}()
	}
	wg.Wait()

	assert.False(t, cleaner.running)
}

// Concurrent Start calls must leave exactly one goroutine running.
func TestEventCleaner_ConcurrentStart(t *testing.T) {
	database := newTestCleanerDB(t, nil)
	cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
	cleaner.cleanupInterval = time.Millisecond

	var mu sync.Mutex
	started := 0

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cleaner.Start(context.Background()); err == nil {
				mu.Lock()
				started++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, started, "more than one cleanup goroutine was started")
	cleaner.Stop()
}

// Stop must not return while a cleanup is still in flight, otherwise the caller
// can close the chain database underneath an in-flight query.
//
// Held open with a write transaction so the goroutine is genuinely blocked
// inside performCleanup while Stop is called. Without that, the goroutine exits
// so fast that a Stop which does not wait looks identical to one that does.
func TestEventCleaner_StopWaitsForInFlightCleanup(t *testing.T) {
	database := newTestCleanerDB(t, nil)
	cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
	cleaner.cleanupInterval = time.Millisecond

	// Start first: the initial cleanup is synchronous and would block on the lock.
	require.NoError(t, cleaner.Start(context.Background()))

	// Take the write lock so the next ticked cleanup blocks on DELETE.
	tx := database.Client().Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Exec(
		"CREATE TABLE IF NOT EXISTS lock_probe (id INTEGER PRIMARY KEY)").Error)
	require.NoError(t, tx.Exec("INSERT INTO lock_probe (id) VALUES (1)").Error)

	time.Sleep(50 * time.Millisecond) // let a tick land and block

	stopped := make(chan struct{})
	go func() {
		cleaner.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		tx.Rollback()
		t.Fatal("Stop returned while a cleanup was still in flight")
	case <-time.After(200 * time.Millisecond):
	}

	tx.Rollback() // release the lock; the cleanup can now finish

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after the cleanup finished")
	}
}

// Cancelling the context stops the goroutine, and a later Stop is still safe.
func TestEventCleaner_ContextCancelThenStop(t *testing.T) {
	database := newTestCleanerDB(t, nil)
	cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
	cleaner.cleanupInterval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, cleaner.Start(ctx))
	cancel()
	time.Sleep(20 * time.Millisecond)

	cleaner.Stop() // must not hang or panic
	assert.False(t, cleaner.running)
}

// Restart after Stop gets a fresh channel rather than reusing the closed one.
func TestEventCleaner_RestartAfterStop(t *testing.T) {
	database := newTestCleanerDB(t, nil)
	cleaner := NewEventCleaner(database, intPtr(3600), intPtr(0), "test-chain", zerolog.Nop())
	cleaner.cleanupInterval = time.Millisecond

	require.NoError(t, cleaner.Start(context.Background()))
	cleaner.Stop()

	require.NoError(t, cleaner.Start(context.Background()), "restart was refused")
	cleaner.Stop()
}
