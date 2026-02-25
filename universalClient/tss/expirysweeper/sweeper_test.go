package expirysweeper

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

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
		if ev.Type == statusSign {
			require.NoError(t, s.voteFailureAndMarkReverted(ctx, &ev, "event expired before TSS could start"))
		} else {
			require.NoError(t, s.eventStore.Update(ev.EventID, map[string]any{"status": eventstore.StatusReverted}))
		}
	}
}

func TestSweep(t *testing.T) {
	t.Run("expired CONFIRMED SIGN marked REVERTED (pushSigner nil skips vote)", func(t *testing.T) {
		sweeper, evtStore, db := setupTestSweeper(t)

		db.Create(&store.Event{EventID: "expired-sign", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "CONFIRMED", Type: "SIGN", EventData: signEventData(t, "tx-1", "utx-1")})

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
			Status: "CONFIRMED", Type: "SIGN", EventData: signEventData(t, "tx-1", "utx-1")})
		// Non-expired CONFIRMED — unchanged
		db.Create(&store.Event{EventID: "valid-1", BlockHeight: 50, ExpiryBlockHeight: 200,
			Status: "CONFIRMED", Type: "KEYGEN"})
		// Expired non-CONFIRMED — unchanged
		db.Create(&store.Event{EventID: "ip-expired", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "IN_PROGRESS", Type: "KEYGEN"})
		db.Create(&store.Event{EventID: "signed-expired", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "SIGNED", Type: "SIGN", EventData: signEventData(t, "tx-2", "utx-2")})
		db.Create(&store.Event{EventID: "broadcasted-expired", BlockHeight: 50, ExpiryBlockHeight: 90,
			Status: "BROADCASTED", Type: "SIGN", EventData: signEventData(t, "tx-3", "utx-3")})

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
