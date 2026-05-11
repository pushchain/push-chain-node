package svm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
)

func TestNewEventConfirmer(t *testing.T) {
	t.Run("creates event confirmer with valid params", func(t *testing.T) {
		logger := zerolog.Nop()
		chainID := "solana:mainnet"

		confirmer := NewEventConfirmer(nil, nil, chainID, 5, 5, 12, logger)

		require.NotNil(t, confirmer)
		assert.Equal(t, chainID, confirmer.chainID)
		assert.Equal(t, 5, confirmer.pollIntervalSeconds)
		assert.Equal(t, uint64(5), confirmer.fastConfirmations)
		assert.Equal(t, uint64(12), confirmer.standardConfirmations)
		assert.NotNil(t, confirmer.chainStore)
		assert.NotNil(t, confirmer.stopCh)
	})

	t.Run("creates event confirmer with different confirmation counts", func(t *testing.T) {
		logger := zerolog.Nop()

		testCases := []struct {
			fast     uint64
			standard uint64
		}{
			{1, 6},
			{5, 12},
			{10, 20},
			{0, 1},
		}

		for _, tc := range testCases {
			confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, tc.fast, tc.standard, logger)
			assert.Equal(t, tc.fast, confirmer.fastConfirmations)
			assert.Equal(t, tc.standard, confirmer.standardConfirmations)
		}
	})

	t.Run("nil rpc client is accepted", func(t *testing.T) {
		logger := zerolog.Nop()
		confirmer := NewEventConfirmer(nil, nil, "solana:test", 5, 5, 12, logger)
		require.NotNil(t, confirmer)
		assert.Nil(t, confirmer.rpcClient)
	})

	t.Run("with in-memory database", func(t *testing.T) {
		logger := zerolog.Nop()
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		confirmer := NewEventConfirmer(nil, database, "solana:test", 10, 3, 15, logger)
		require.NotNil(t, confirmer)
		assert.Equal(t, 10, confirmer.pollIntervalSeconds)
		assert.Equal(t, uint64(3), confirmer.fastConfirmations)
		assert.Equal(t, uint64(15), confirmer.standardConfirmations)
	})

	t.Run("zero confirmations stored as-is", func(t *testing.T) {
		logger := zerolog.Nop()
		confirmer := NewEventConfirmer(nil, nil, "solana:test", 5, 0, 0, logger)
		require.NotNil(t, confirmer)
		assert.Equal(t, uint64(0), confirmer.fastConfirmations)
		assert.Equal(t, uint64(0), confirmer.standardConfirmations)
	})
}

func TestEventConfirmerGetTxSignatureFromEventID(t *testing.T) {
	logger := zerolog.Nop()
	confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 12, logger)

	t.Run("extracts signature from standard format", func(t *testing.T) {
		eventID := "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW:0"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW", sig)
	})

	t.Run("extracts signature with log index", func(t *testing.T) {
		eventID := "abc123def456:5"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "abc123def456", sig)
	})

	t.Run("handles event ID without colon", func(t *testing.T) {
		eventID := "abc123def456"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "abc123def456", sig)
	})

	t.Run("returns empty string for empty event ID", func(t *testing.T) {
		sig := confirmer.getTxSignatureFromEventID("")
		assert.Empty(t, sig)
	})

	t.Run("handles multiple colons", func(t *testing.T) {
		eventID := "sig:123:456:789"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, "sig", sig)
	})

	t.Run("colon at start returns empty", func(t *testing.T) {
		sig := confirmer.getTxSignatureFromEventID(":42")
		assert.Equal(t, "", sig)
	})
}

func TestEventConfirmerGetRequiredConfirmations(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("FAST confirmation type with custom value", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 12, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(5), confirmations)
	})

	t.Run("FAST confirmation type with zero uses default", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 0, 12, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(5), confirmations) // Default is 5
	})

	t.Run("STANDARD confirmation type with custom value", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 20, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(20), confirmations)
	})

	t.Run("STANDARD confirmation type with zero uses default", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 0, logger)
		confirmations := confirmer.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(12), confirmations) // Default is 12
	})

	t.Run("unknown type defaults to standard configured", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 25, logger)
		confirmations := confirmer.getRequiredConfirmations("UNKNOWN")
		assert.Equal(t, uint64(25), confirmations)
	})

	t.Run("unknown type with zero falls back to default 12", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 0, 0, logger)
		confirmations := confirmer.getRequiredConfirmations("UNKNOWN")
		assert.Equal(t, uint64(12), confirmations)
	})

	t.Run("empty type defaults to standard", func(t *testing.T) {
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 15, logger)
		confirmations := confirmer.getRequiredConfirmations("")
		assert.Equal(t, uint64(15), confirmations)
	})
}

func TestEventConfirmerStop(t *testing.T) {
	t.Run("stop without start does not panic", func(t *testing.T) {
		logger := zerolog.Nop()
		confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 12, logger)

		assert.NotPanics(t, func() {
			confirmer.Stop()
		})
	})
}

func TestEventConfirmerStruct(t *testing.T) {
	t.Run("struct has expected fields", func(t *testing.T) {
		ec := &EventConfirmer{}
		assert.Nil(t, ec.rpcClient)
		assert.Nil(t, ec.chainStore)
		assert.Empty(t, ec.chainID)
		assert.Equal(t, 0, ec.pollIntervalSeconds)
		assert.Equal(t, uint64(0), ec.fastConfirmations)
		assert.Equal(t, uint64(0), ec.standardConfirmations)
		assert.Nil(t, ec.stopCh)
	})
}

func TestEventConfirmer_StartStop_ContextCancel(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	// rpcClient is nil so processPendingEvents will error, but the goroutine
	// should still shut down cleanly via context cancellation.
	ec := NewEventConfirmer(nil, database, "solana:test", 1, 5, 12, logger)

	ctx, cancel := context.WithCancel(context.Background())
	err = ec.Start(ctx)
	require.NoError(t, err)

	// Cancel context and verify shutdown completes
	cancel()

	done := make(chan struct{})
	go func() {
		ec.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("event confirmer did not stop after context cancellation")
	}
}

func TestEventConfirmer_StartStop_StopChannel(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	ec := NewEventConfirmer(nil, database, "solana:test", 1, 5, 12, logger)

	ctx := context.Background()
	err = ec.Start(ctx)
	require.NoError(t, err)

	// Stop via the Stop() method
	done := make(chan struct{})
	go func() {
		ec.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("event confirmer did not stop after Stop() call")
	}
}

func TestEventConfirmerGetTxSignatureFromEventID_MoreEdgeCases(t *testing.T) {
	logger := zerolog.Nop()
	confirmer := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 12, logger)

	t.Run("trailing colon returns signature", func(t *testing.T) {
		sig := confirmer.getTxSignatureFromEventID("abc123:")
		assert.Equal(t, "abc123", sig)
	})

	t.Run("colon only returns empty", func(t *testing.T) {
		sig := confirmer.getTxSignatureFromEventID(":")
		assert.Equal(t, "", sig)
	})

	t.Run("whitespace-only returns as-is", func(t *testing.T) {
		sig := confirmer.getTxSignatureFromEventID("   ")
		assert.Equal(t, "   ", sig)
	})

	t.Run("full 88-char base58 signature with log index", func(t *testing.T) {
		fullSig := "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"
		eventID := fullSig + ":42"
		sig := confirmer.getTxSignatureFromEventID(eventID)
		assert.Equal(t, fullSig, sig)
	})

	t.Run("numeric-only event ID returns as-is", func(t *testing.T) {
		sig := confirmer.getTxSignatureFromEventID("123456789")
		assert.Equal(t, "123456789", sig)
	})

	t.Run("special characters in signature part", func(t *testing.T) {
		sig := confirmer.getTxSignatureFromEventID("abc+def/ghi=:0")
		assert.Equal(t, "abc+def/ghi=", sig)
	})
}

func TestEventConfirmerGetRequiredConfirmations_MoreEdgeCases(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("large fast confirmations", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 1000000, 12, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(1000000), result)
	})

	t.Run("large standard confirmations", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 500000, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(500000), result)
	})

	t.Run("fast 1 confirmation", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 1, 12, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(1), result)
	})

	t.Run("standard 1 confirmation", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 1, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(1), result)
	})

	t.Run("unknown type with large standard returns large value", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 5, 300, logger)
		result := ec.getRequiredConfirmations("SUPER_SAFE")
		assert.Equal(t, uint64(300), result)
	})

	t.Run("all types consistent when same values", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 10, 10, logger)
		fast := ec.getRequiredConfirmations(store.ConfirmationFast)
		standard := ec.getRequiredConfirmations(store.ConfirmationStandard)
		unknown := ec.getRequiredConfirmations("OTHER")
		assert.Equal(t, uint64(10), fast)
		assert.Equal(t, uint64(10), standard)
		assert.Equal(t, uint64(10), unknown)
	})

	t.Run("zero fast falls back to default 5", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 0, 20, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationFast)
		assert.Equal(t, uint64(5), result) // default 5
	})

	t.Run("zero standard falls back to default 12", func(t *testing.T) {
		ec := NewEventConfirmer(nil, nil, "solana:mainnet", 5, 10, 0, logger)
		result := ec.getRequiredConfirmations(store.ConfirmationStandard)
		assert.Equal(t, uint64(12), result) // default 12
	})
}

// A pending event whose tx response has meta.err set must transition to
// REVERTED, never to CONFIRMED. Solana preserves meta.logMessages even on
// failed txs, so without this branch a Program-data line from a failed tx
// could otherwise reach confirmation and vote paths.
func TestProcessPendingEvents_FailedMetaErrMarkedReverted(t *testing.T) {
	// 64 base58 '1' chars decode to all-zero bytes — a syntactically valid
	// Solana signature that the test confirmer can parse.
	sigStr := strings.Repeat("1", 64)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		bodyStr := string(body)

		switch {
		case strings.Contains(bodyStr, `"getHealth"`):
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"ok"}`))
		case strings.Contains(bodyStr, `"getSlot"`):
			// Slot well beyond the event's recorded slot to satisfy
			// the confirmation-depth check if execution had been successful.
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":1000}`))
		case strings.Contains(bodyStr, `"getTransaction"`):
			// meta.err non-null indicates a failed transaction. The exact
			// shape mirrors a real Solana RPC failure response.
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{` +
				`"slot":100,` +
				`"meta":{` +
				`"err":{"InstructionError":[0,"Custom: 1"]},` +
				`"fee":5000,` +
				`"preBalances":[],` +
				`"postBalances":[],` +
				`"logMessages":["Program log: would have been a Program data: line"],` +
				`"status":{"Err":{"InstructionError":[0,"Custom: 1"]}}` +
				`},` +
				`"transaction":["AQ==","base64"]` +
				`}}`))
		default:
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`))
		}
	}))
	defer server.Close()

	logger := zerolog.Nop()
	// Pass empty expectedGenesisHash to skip the getGenesisHash probe.
	rpcClient, err := NewRPCClient([]string{server.URL}, "", logger)
	require.NoError(t, err)
	defer rpcClient.Close()

	memDB, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	defer memDB.Close()

	ec := NewEventConfirmer(rpcClient, memDB, "solana:mainnet", 5, 5, 12, logger)
	cs := common.NewChainStore(memDB)

	pending := &store.Event{
		EventID:          sigStr + ":0",
		BlockHeight:      100,
		Type:             store.EventTypeInbound,
		ConfirmationType: store.ConfirmationStandard,
		Status:           store.StatusPending,
		EventData:        []byte(`{}`),
	}
	inserted, err := cs.InsertEventIfNotExists(pending)
	require.NoError(t, err)
	require.True(t, inserted)

	require.NoError(t, ec.processPendingEvents(context.Background()))

	var got store.Event
	require.NoError(t, memDB.Client().Where("event_id = ?", pending.EventID).First(&got).Error)
	assert.Equal(t, store.StatusReverted, got.Status, "failed-meta tx must transition to REVERTED, not CONFIRMED")
}

func TestEventConfirmer_StartStop_ZeroPollInterval(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	// Zero poll interval should default to 5s internally in checkAndConfirmEvents
	ec := NewEventConfirmer(nil, database, "solana:test", 0, 5, 12, logger)

	ctx, cancel := context.WithCancel(context.Background())
	err = ec.Start(ctx)
	require.NoError(t, err)

	cancel()

	done := make(chan struct{})
	go func() {
		ec.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("event confirmer did not stop after context cancellation with zero poll interval")
	}
}
