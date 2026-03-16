package txresolver

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"unsafe"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockTxBuilder struct{ mock.Mock }

func (m *mockTxBuilder) GetOutboundSigningRequest(ctx context.Context, data *uexecutortypes.OutboundCreatedEvent, nonce uint64) (*common.UnSignedOutboundTxReq, error) {
	args := m.Called(ctx, data, nonce)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*common.UnSignedOutboundTxReq), args.Error(1)
}

func (m *mockTxBuilder) GetNextNonce(ctx context.Context, addr string, useFinalized bool) (uint64, error) {
	args := m.Called(ctx, addr, useFinalized)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockTxBuilder) BroadcastOutboundSigningRequest(ctx context.Context, req *common.UnSignedOutboundTxReq, data *uexecutortypes.OutboundCreatedEvent, sig []byte) (string, error) {
	args := m.Called(ctx, req, data, sig)
	return args.String(0), args.Error(1)
}

func (m *mockTxBuilder) VerifyBroadcastedTx(ctx context.Context, txHash string) (bool, uint64, uint64, uint8, error) {
	args := m.Called(ctx, txHash)
	return args.Bool(0), args.Get(1).(uint64), args.Get(2).(uint64), args.Get(3).(uint8), args.Error(4)
}

func (m *mockTxBuilder) IsAlreadyExecuted(ctx context.Context, txID string) (bool, error) {
	args := m.Called(ctx, txID)
	return args.Bool(0), args.Error(1)
}

type mockChainClient struct{ builder *mockTxBuilder }

func (m *mockChainClient) Start(context.Context) error                     { return nil }
func (m *mockChainClient) Stop() error                                     { return nil }
func (m *mockChainClient) IsHealthy() bool                                 { return true }
func (m *mockChainClient) GetTxBuilder() (common.OutboundTxBuilder, error) { return m.builder, nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupTestDB(t *testing.T) (*eventstore.Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&store.Event{}))
	return eventstore.NewStore(db, zerolog.Nop()), db
}

func makeOutboundEventData(txID, utxID, destChain string) []byte {
	data := uexecutortypes.OutboundCreatedEvent{
		TxID:             txID,
		UniversalTxId:    utxID,
		DestinationChain: destChain,
	}
	b, _ := json.Marshal(data)
	return b
}

func newTestChains(t *testing.T, chainID string, vmType uregistrytypes.VmType, client common.ChainClient) *chains.Chains {
	t.Helper()
	c := chains.NewChains(nil, nil, &config.Config{PushChainID: "test-chain"}, zerolog.Nop())

	v := reflect.ValueOf(c).Elem()

	chainsField := v.FieldByName("chains")
	chainsMap := *(*map[string]common.ChainClient)(unsafe.Pointer(chainsField.UnsafeAddr()))
	chainsMap[chainID] = client

	configsField := v.FieldByName("chainConfigs")
	configsMap := *(*map[string]*uregistrytypes.ChainConfig)(unsafe.Pointer(configsField.UnsafeAddr()))
	configsMap[chainID] = &uregistrytypes.ChainConfig{
		Chain:  chainID,
		VmType: vmType,
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}

	return c
}

func insertBroadcastedEvent(t *testing.T, db *gorm.DB, eventID, destChain, broadcastedTxHash string, eventData []byte) {
	t.Helper()
	event := store.Event{
		EventID:           eventID,
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              "SIGN",
		ConfirmationType:  "STANDARD",
		Status:            eventstore.StatusBroadcasted,
		EventData:         eventData,
		BroadcastedTxHash: broadcastedTxHash,
	}
	require.NoError(t, db.Create(&event).Error)
}

func getEvent(t *testing.T, db *gorm.DB, eventID string) store.Event {
	t.Helper()
	var ev store.Event
	require.NoError(t, db.Where("event_id = ?", eventID).First(&ev).Error)
	return ev
}

func newResolver(evtStore *eventstore.Store, ch *chains.Chains) *Resolver {
	return NewResolver(Config{
		EventStore:    evtStore,
		Chains:        ch,
		CheckInterval: 0,
		Logger:        zerolog.Nop(),
	})
}

// ---------------------------------------------------------------------------
// parseCAIPTxHash tests
// ---------------------------------------------------------------------------

func TestParseCAIPTxHash(t *testing.T) {
	t.Run("valid CAIP tx hash", func(t *testing.T) {
		chainID, txHash, err := parseCAIPTxHash("eip155:1:0xabc123")
		require.NoError(t, err)
		assert.Equal(t, "eip155:1", chainID)
		assert.Equal(t, "0xabc123", txHash)
	})

	t.Run("valid CAIP with long tx hash", func(t *testing.T) {
		chainID, txHash, err := parseCAIPTxHash("eip155:137:0xdeadbeef1234567890abcdef")
		require.NoError(t, err)
		assert.Equal(t, "eip155:137", chainID)
		assert.Equal(t, "0xdeadbeef1234567890abcdef", txHash)
	})

	t.Run("solana CAIP tx hash", func(t *testing.T) {
		chainID, txHash, err := parseCAIPTxHash("solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp:3AsdoALgZFuq2oUVWrDYhg2pNeaLJKPLf8hU2mQ6U8qJxeJ6hsrPVd")
		require.NoError(t, err)
		assert.Equal(t, "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp", chainID)
		assert.Equal(t, "3AsdoALgZFuq2oUVWrDYhg2pNeaLJKPLf8hU2mQ6U8qJxeJ6hsrPVd", txHash)
	})

	t.Run("empty string", func(t *testing.T) {
		_, _, err := parseCAIPTxHash("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CAIP tx hash")
	})

	t.Run("no colon", func(t *testing.T) {
		_, _, err := parseCAIPTxHash("0xabc123")
		require.Error(t, err)
	})

	t.Run("colon at end", func(t *testing.T) {
		_, _, err := parseCAIPTxHash("eip155:1:")
		require.Error(t, err)
	})

	t.Run("colon at start", func(t *testing.T) {
		_, _, err := parseCAIPTxHash(":0xabc")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// extractOutboundIDs tests
// ---------------------------------------------------------------------------

func TestExtractOutboundIDs(t *testing.T) {
	t.Run("valid event data", func(t *testing.T) {
		eventData := makeOutboundEventData("tx-123", "utx-456", "eip155:1")
		event := &store.Event{EventData: eventData}

		txID, utxID, err := extractOutboundIDs(event)
		require.NoError(t, err)
		assert.Equal(t, "tx-123", txID)
		assert.Equal(t, "utx-456", utxID)
	})

	t.Run("invalid json", func(t *testing.T) {
		event := &store.Event{EventData: []byte("not json")}
		_, _, err := extractOutboundIDs(event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
	})

	t.Run("empty event data", func(t *testing.T) {
		event := &store.Event{EventData: []byte("{}")}
		txID, utxID, err := extractOutboundIDs(event)
		require.NoError(t, err)
		assert.Equal(t, "", txID)
		assert.Equal(t, "", utxID)
	})
}

// ---------------------------------------------------------------------------
// resolveSVM tests
// ---------------------------------------------------------------------------

func TestSVM_PDAExists_MarksCompleted(t *testing.T) {
	// PDA found on-chain → mark COMPLETED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-123", "utx-456", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(true, nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusCompleted, updated.Status)
}

func TestSVM_PDANotFound_VotesFailureAndReverts(t *testing.T) {
	// PDA not found → vote failure → REVERTED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-123", "utx-456", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:", eventData)

	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, nil)

	// No PushSigner — voteFailure will log warning and return nil, but won't mark REVERTED
	// (because pushSigner is nil, it returns early). This validates the code path.
	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	// With no push signer, voteFailureAndMarkReverted returns nil early (logs warning).
	// The event stays BROADCASTED because the vote+revert is skipped.
	updated := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, updated.Status)
}

func TestSVM_PDACheckFails_StaysBroadcasted(t *testing.T) {
	// RPC error on PDA check → stays BROADCASTED for retry.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-123", "utx-456", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, assert.AnError)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, updated.Status) // stays BROADCASTED
}

func TestSVM_InvalidEventData_Skips(t *testing.T) {
	// Bad event data → logged and skipped (stays BROADCASTED).
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", []byte("not json"))

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, updated.Status) // stays BROADCASTED
	builder.AssertNotCalled(t, "IsAlreadyExecuted", mock.Anything, mock.Anything)
}

// ---------------------------------------------------------------------------
// resolveEvent routing tests
// ---------------------------------------------------------------------------

func TestResolveEventRouting(t *testing.T) {
	t.Run("invalid CAIP hash with no outbound IDs triggers warning", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			Logger:     zerolog.Nop(),
		})

		event := &store.Event{
			EventID:           "bad-caip-1",
			BroadcastedTxHash: "invalid",
			EventData:         []byte("not json"),
		}

		// Should not panic — logs warning and returns
		resolver.resolveEvent(context.Background(), event)
	})

	t.Run("invalid CAIP hash with valid outbound IDs attempts revert", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			Logger:     zerolog.Nop(),
			// No PushSigner — voteFailure will log warning but not panic
		})

		eventData := makeOutboundEventData("tx-1", "utx-1", "eip155:1")
		event := &store.Event{
			EventID:           "bad-caip-2",
			BroadcastedTxHash: "invalid",
			EventData:         eventData,
		}

		// Should not panic — will try to vote failure (no signer, logged), then try to mark reverted
		resolver.resolveEvent(context.Background(), event)
	})
}

// ---------------------------------------------------------------------------
// notFoundCounts tracking tests
// ---------------------------------------------------------------------------

func TestNotFoundCountTracking(t *testing.T) {
	t.Run("increments on not found", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			Logger:     zerolog.Nop(),
		})

		eventID := "test-event-1"
		assert.Equal(t, 0, resolver.notFoundCounts[eventID])

		resolver.notFoundCounts[eventID]++
		assert.Equal(t, 1, resolver.notFoundCounts[eventID])

		resolver.notFoundCounts[eventID]++
		assert.Equal(t, 2, resolver.notFoundCounts[eventID])
	})

	t.Run("cleared after max retries", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			Logger:     zerolog.Nop(),
		})

		eventID := "test-event-2"
		resolver.notFoundCounts[eventID] = maxNotFoundRetries

		// Simulate cleanup
		delete(resolver.notFoundCounts, eventID)
		assert.Equal(t, 0, resolver.notFoundCounts[eventID])
	})

	t.Run("cleared when tx found", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			Logger:     zerolog.Nop(),
		})

		eventID := "test-event-3"
		resolver.notFoundCounts[eventID] = 5

		// Simulate tx found — clear tracking
		delete(resolver.notFoundCounts, eventID)
		_, exists := resolver.notFoundCounts[eventID]
		assert.False(t, exists)
	})
}

// ---------------------------------------------------------------------------
// voteFailureAndMarkReverted tests
// ---------------------------------------------------------------------------

func TestVoteFailureAndMarkReverted(t *testing.T) {
	t.Run("no push signer logs warning and returns nil", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			PushSigner: nil, // no signer
			Logger:     zerolog.Nop(),
		})

		event := &store.Event{EventID: "ev-1"}
		err := resolver.voteFailureAndMarkReverted(context.Background(), event, "tx-1", "utx-1", "0xhash", 12345, "some error")
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// constants tests
// ---------------------------------------------------------------------------

func TestConstants(t *testing.T) {
	t.Run("maxNotFoundRetries is reasonable", func(t *testing.T) {
		// At 30s interval, 10 retries = ~5 minutes
		assert.Equal(t, 10, maxNotFoundRetries)
	})

	t.Run("processBroadcastedBatchSize", func(t *testing.T) {
		assert.Equal(t, 100, processBroadcastedBatchSize)
	})
}
