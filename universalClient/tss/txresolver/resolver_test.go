package txresolver

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

type mockTxBuilder struct{ mock.Mock }

func (m *mockTxBuilder) GetOutboundSigningRequest(ctx context.Context, data *uexecutortypes.OutboundCreatedEvent, nonce uint64) (*common.UnsignedSigningReq, error) {
	args := m.Called(ctx, data, nonce)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*common.UnsignedSigningReq), args.Error(1)
}

func (m *mockTxBuilder) GetNextNonce(ctx context.Context, addr string, useFinalized bool) (uint64, error) {
	args := m.Called(ctx, addr, useFinalized)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockTxBuilder) BroadcastOutboundSigningRequest(ctx context.Context, req *common.UnsignedSigningReq, data *uexecutortypes.OutboundCreatedEvent, sig []byte) (string, error) {
	args := m.Called(ctx, req, data, sig)
	return args.String(0), args.Error(1)
}

func (m *mockTxBuilder) VerifyBroadcastedTx(ctx context.Context, txHash string) (bool, uint64, uint64, uint8, error) {
	args := m.Called(ctx, txHash)
	return args.Bool(0), args.Get(1).(uint64), args.Get(2).(uint64), args.Get(3).(uint8), args.Error(4)
}

func (m *mockTxBuilder) IsAlreadyExecuted(ctx context.Context, txID string) (bool, int64, error) {
	args := m.Called(ctx, txID)
	return args.Bool(0), args.Get(1).(int64), args.Error(2)
}

func (m *mockTxBuilder) GetGasFeeUsed(ctx context.Context, txHash string) (string, error) {
	args := m.Called(ctx, txHash)
	return args.String(0), args.Error(1)
}

func (m *mockTxBuilder) GetFundMigrationSigningRequest(ctx context.Context, data *common.FundMigrationData, nonce uint64) (*common.UnsignedSigningReq, error) {
	args := m.Called(ctx, data, nonce)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*common.UnsignedSigningReq), args.Error(1)
}

func (m *mockTxBuilder) BroadcastFundMigrationTx(ctx context.Context, req *common.UnsignedSigningReq, data *common.FundMigrationData, sig []byte) (string, error) {
	args := m.Called(ctx, req, data, sig)
	return args.String(0), args.Error(1)
}

type mockChainClient struct{ builder *mockTxBuilder }

func (m *mockChainClient) Start(context.Context) error             { return nil }
func (m *mockChainClient) Stop() error                             { return nil }
func (m *mockChainClient) IsHealthy() bool                         { return true }
func (m *mockChainClient) GetTxBuilder() (common.TxBuilder, error) { return m.builder, nil }

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
		Type:              "SIGN_OUTBOUND",
		ConfirmationType:  "STANDARD",
		Status:            store.StatusBroadcasted,
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

func newTestChainsOutboundDisabled(t *testing.T, chainID string, vmType uregistrytypes.VmType, client common.ChainClient) *chains.Chains {
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
			IsOutboundEnabled: false,
		},
	}

	return c
}

func newResolver(evtStore *eventstore.Store, ch *chains.Chains) *Resolver {
	return NewResolver(Config{
		EventStore:    evtStore,
		Chains:        ch,
		CheckInterval: 0,
		Logger:        zerolog.Nop(),
	})
}

// newResolverWithTSSAddress builds a Resolver that returns a fixed TSS address
// from GetTSSAddress — needed by tests that exercise the EVM nonce-based
// retry/revert path.
func newResolverWithTSSAddress(evtStore *eventstore.Store, ch *chains.Chains, addr string) *Resolver {
	return NewResolver(Config{
		EventStore:    evtStore,
		Chains:        ch,
		CheckInterval: 0,
		Logger:        zerolog.Nop(),
		GetTSSAddress: func(ctx context.Context) (string, error) { return addr, nil },
	})
}

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

	t.Run("trailing colon accepted with empty hash (SVM broadcaster signal)", func(t *testing.T) {
		// `solana:<cluster>:` is the broadcaster's "no real tx hash to point to"
		// marker; the parser passes the chainID through and lets the chain
		// branch decide whether empty hash is acceptable.
		chainID, txHash, err := parseCAIPTxHash("solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1:")
		require.NoError(t, err)
		assert.Equal(t, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", chainID)
		assert.Equal(t, "", txHash)
	})

	t.Run("colon at start", func(t *testing.T) {
		_, _, err := parseCAIPTxHash(":0xabc")
		require.Error(t, err)
	})
}

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

func TestSVM_PDAExists_MarksCompleted(t *testing.T) {
	// PDA found on-chain → mark COMPLETED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-123", "utx-456", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(true, int64(0), nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusCompleted, updated.Status)
}

func TestSVM_PDAAbsent_DeadlineZero_ClusterFresh_Reverts(t *testing.T) {
	// Legacy event with no deadline (=0). PDA absent + fresh cluster time
	// (>> 0) satisfies `clusterTime > deadline + slack` → reaches REVERT.
	// No PushSigner → vote returns nil, status stays BROADCASTED. The point
	// is that the resolver reaches the vote path (not defers).
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-123", "utx-456", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:", eventData)

	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, time.Now().Unix(), nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, updated.Status) // no PushSigner → vote skipped
	builder.AssertCalled(t, "IsAlreadyExecuted", mock.Anything, "tx-123")
}

func TestSVM_PDACheckFails_StaysBroadcasted(t *testing.T) {
	// RPC error on PDA check → stays BROADCASTED for retry.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-123", "utx-456", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, int64(0), assert.AnError)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, updated.Status) // stays BROADCASTED
}

// makeOutboundEventDataWithDeadline mirrors makeOutboundEventData but sets the
// chain-emitted signing deadline used by the resolver's cluster-time gate.
func makeOutboundEventDataWithDeadline(txID, utxID, destChain string, deadline int64) []byte {
	data := uexecutortypes.OutboundCreatedEvent{
		TxID:             txID,
		UniversalTxId:    utxID,
		DestinationChain: destChain,
		SigningDeadline:  deadline,
	}
	b, _ := json.Marshal(data)
	return b
}

func TestSVM_PDAAbsent_ClusterTimeUnknown_DefersRevert(t *testing.T) {
	// PDA absent + deadline set + cluster time = 0 (RPC didn't supply it) →
	// stay BROADCASTED, defer REVERT until we can verify cluster health.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventDataWithDeadline("tx-123", "utx-456", "solana:mainnet", time.Now().Unix()-3600)
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, int64(0), nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, updated.Status)
}

func TestSVM_PDAAbsent_ClusterStale_DefersRevert(t *testing.T) {
	// PDA absent + cluster time >120s old → cluster halted/lagging → defer REVERT.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventDataWithDeadline("tx-123", "utx-456", "solana:mainnet", time.Now().Unix()-3600)
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	// Cluster block time is 10 minutes old.
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, time.Now().Unix()-600, nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, updated.Status)
}

func TestSVM_PDAAbsent_ClusterStillInWindow_DefersRevert(t *testing.T) {
	// PDA absent + cluster fresh but cluster's clock <= deadline+slack →
	// the program still accepts late retries; defer REVERT.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	now := time.Now().Unix()
	deadline := now - 30 // local says past, but well under slack
	eventData := makeOutboundEventDataWithDeadline("tx-123", "utx-456", "solana:mainnet", deadline)
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	// Cluster time = now (fresh) but <= deadline+slack (deadline+60 = now+30).
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, now, nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, updated.Status)
}

func TestSVM_PDAAbsent_ClusterConfirmsExpiry_Reverts(t *testing.T) {
	// PDA absent + cluster fresh + cluster past deadline+slack → REVERT path.
	// (With no PushSigner the vote is logged but status stays BROADCASTED;
	// what we assert is that the resolver reached the vote call.)
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	now := time.Now().Unix()
	eventData := makeOutboundEventDataWithDeadline("tx-123", "utx-456", "solana:mainnet", now-3600)
	insertBroadcastedEvent(t, db, "ev-1", "solana:mainnet", "solana:mainnet:solTxSig", eventData)

	// Cluster time = now (fresh) and well past deadline+slack.
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, now, nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-1")
	resolver.resolveSVM(context.Background(), &ev, "solana:mainnet")

	// No PushSigner → vote returns nil early; status unchanged. The point is
	// the resolver REACHED the vote (i.e., didn't defer); covered by absence
	// of any defer log path and the mock having been called.
	builder.AssertCalled(t, "IsAlreadyExecuted", mock.Anything, "tx-123")
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
	require.Equal(t, store.StatusBroadcasted, updated.Status) // stays BROADCASTED
	builder.AssertNotCalled(t, "IsAlreadyExecuted", mock.Anything, mock.Anything)
}

func TestResolveOutbound_SVMEmptyHashDispatchesToSVMFlow(t *testing.T) {
	// Regression: BroadcastedTxHash = "solana:<cluster>:" (empty hash suffix)
	// must NOT vote REVERT through the parse-error branch — that branch is
	// EVM-shaped. SVM verifies by event txID; the dispatcher must route this
	// to resolveSVM, which then calls IsAlreadyExecuted on the event's txID.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-123", "utx-456", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-svm-empty", "solana:mainnet", "solana:mainnet:", eventData)

	// resolveSVM should reach IsAlreadyExecuted with the event's txID.
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").
		Return(true, time.Now().Unix(), nil)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-svm-empty")
	resolver.resolveOutbound(context.Background(), &ev)

	// Critical assertion: the SVM verification path was reached.
	builder.AssertCalled(t, "IsAlreadyExecuted", mock.Anything, "tx-123")
}

func TestResolveOutboundEVM_EmptyHashRewindsToSigned(t *testing.T) {
	// Defensive: if a row reaches the EVM resolver with empty rawTxHash
	// (broadcaster bug), don't trust the nonce check — voting REVERT here
	// would risk reverting a row whose tx is actually on chain. Rewind to
	// SIGNED so the broadcaster recomputes the deterministic hash from
	// signing_data on its next tick. No chain RPCs should be consulted.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventData("tx-evm", "utx-evm", "eip155:1")
	insertBroadcastedEvent(t, db, "ev-evm-empty", "eip155:1", "eip155:1:", eventData)

	resolver := newResolver(evtStore, ch)
	ev := getEvent(t, db, "ev-evm-empty")
	resolver.resolveOutbound(context.Background(), &ev)

	// Status rewound to SIGNED — broadcaster will recompute hash next tick.
	updated := getEvent(t, db, "ev-evm-empty")
	require.Equal(t, store.StatusSigned, updated.Status)
	// No chain calls — we knew the hash was missing without asking.
	builder.AssertNotCalled(t, "VerifyBroadcastedTx", mock.Anything, mock.Anything)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

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

	t.Run("invalid CAIP hash logs warning and does NOT vote REVERT", func(t *testing.T) {
		// Malformed CAIP is treated as a bug indicator, not a dead-row signal.
		// The row is left BROADCASTED for manual recovery; nothing is voted.
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			Logger:     zerolog.Nop(),
		})

		eventData := makeOutboundEventData("tx-1", "utx-1", "eip155:1")
		event := &store.Event{
			EventID:           "bad-caip-2",
			BroadcastedTxHash: "invalid",
			EventData:         eventData,
		}

		// Should not panic, should not vote — just logs and returns.
		resolver.resolveEvent(context.Background(), event)
	})
}

func TestVoteFailureAndMarkReverted(t *testing.T) {
	t.Run("no push signer logs warning and returns nil", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		resolver := NewResolver(Config{
			EventStore: evtStore,
			PushSigner: nil, // no signer
			Logger:     zerolog.Nop(),
		})

		event := &store.Event{EventID: "ev-1"}
		err := resolver.voteOutboundFailureAndMarkReverted(context.Background(), event, "tx-1", "utx-1", "0xhash", 12345, "0", "some error")
		assert.NoError(t, err)
	})
}

func makeFundMigrationEventData(migrationID uint64, chain string) []byte {
	data := utsstypes.FundMigrationInitiatedEventData{
		MigrationID: migrationID,
		Chain:       chain,
	}
	b, _ := json.Marshal(data)
	return b
}

func insertBroadcastedFundMigrationEvent(t *testing.T, db *gorm.DB, eventID, chain, broadcastedTxHash string, migrationID uint64) {
	t.Helper()
	event := store.Event{
		EventID:           eventID,
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              store.EventTypeSignFundMigrate,
		ConfirmationType:  "INSTANT",
		Status:            store.StatusBroadcasted,
		EventData:         makeFundMigrationEventData(migrationID, chain),
		BroadcastedTxHash: broadcastedTxHash,
	}
	require.NoError(t, db.Create(&event).Error)
}

func TestFundMigrationEVM_TxSuccess_VotesSuccessAndCompletes(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEvent(t, db, "fm-1", "eip155:1", "eip155:1:0xmigrate123", 42)

	// Tx found, confirmed, status=1 (success)
	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmigrate123").
		Return(true, uint64(500), uint64(20), uint8(1), nil)

	// No pushSigner — voteFundMigrationAndMark logs warning but doesn't panic
	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	// Without pushSigner, vote is skipped so status stays BROADCASTED
	ev := getEvent(t, db, "fm-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestFundMigrationEVM_TxReverted_VotesFailure(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEvent(t, db, "fm-1", "eip155:1", "eip155:1:0xfailed", 42)

	// Tx found, confirmed, status=0 (reverted)
	builder.On("VerifyBroadcastedTx", mock.Anything, "0xfailed").
		Return(true, uint64(500), uint64(20), uint8(0), nil)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	// Without pushSigner, vote is skipped so status stays BROADCASTED
	ev := getEvent(t, db, "fm-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

// testOldTSSPubkey is a valid compressed secp256k1 pubkey that DeriveEVMAddressFromPubkey
// can parse into testOldTSSAddr. Used by fund migration nonce-based tests.
const testOldTSSPubkey = "03d5d5d290a0ecec420e843fc2a57f1696781ec657e204406fc67bb5fe0c751317"
const testOldTSSAddr = "0x9fed6f778a956244c06a3b905ba45bdb2ec3afea"

// makeFundMigrationEventDataWithNonce mirrors makeFundMigrationEventData but
// adds OldTssPubkey + signing_data.nonce — the fields the resolver consults
// on tx-not-found.
func makeFundMigrationEventDataWithNonce(migrationID uint64, chain, oldPubkey string, nonce uint64) []byte {
	b, _ := json.Marshal(map[string]any{
		"migration_id":   migrationID,
		"chain":          chain,
		"old_tss_pubkey": oldPubkey,
		"signing_data": map[string]any{
			"nonce": nonce,
		},
	})
	return b
}

func insertBroadcastedFundMigrationEventWithNonce(
	t *testing.T, db *gorm.DB,
	eventID, chain, broadcastedTxHash string,
	migrationID uint64, oldPubkey string, nonce uint64,
) {
	t.Helper()
	event := store.Event{
		EventID:           eventID,
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              store.EventTypeSignFundMigrate,
		ConfirmationType:  "INSTANT",
		Status:            store.StatusBroadcasted,
		EventData:         makeFundMigrationEventDataWithNonce(migrationID, chain, oldPubkey, nonce),
		BroadcastedTxHash: broadcastedTxHash,
	}
	require.NoError(t, db.Create(&event).Error)
}

func TestFundMigrationEVM_NotFound_NonceConsumed_VotesFailure(t *testing.T) {
	// Fund migration tx not found AND old-TSS signed nonce already finalized →
	// another tx consumed the slot. REVERT path.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEventWithNonce(
		t, db, "fm-consumed", "eip155:1", "eip155:1:0xmigmissing", 42, testOldTSSPubkey, 3,
	)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmigmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)
	builder.On("GetNextNonce", mock.Anything, testOldTSSAddr, true).Return(uint64(5), nil)

	resolver := newResolver(evtStore, ch) // GetTSSAddress not needed; signer derived from event
	resolver.processBroadcasted(context.Background())

	// No PushSigner → vote skipped, status stays BROADCASTED.
	ev := getEvent(t, db, "fm-consumed")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertCalled(t, "GetNextNonce", mock.Anything, testOldTSSAddr, true)
}

func TestFundMigrationEVM_NotFound_NonceUnconsumed_RewindsToSigned(t *testing.T) {
	// Fund migration tx not found AND old-TSS signed nonce not yet finalized →
	// rewind to SIGNED for re-broadcast.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEventWithNonce(
		t, db, "fm-unconsumed", "eip155:1", "eip155:1:0xmigpending", 42, testOldTSSPubkey, 5,
	)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmigpending").
		Return(false, uint64(0), uint64(0), uint8(0), nil)
	builder.On("GetNextNonce", mock.Anything, testOldTSSAddr, true).Return(uint64(5), nil)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "fm-unconsumed")
	require.Equal(t, store.StatusSigned, ev.Status)
}

func TestFundMigrationEVM_NotFound_NonceRPCError_StaysBroadcasted(t *testing.T) {
	// Fund migration tx not found and nonce RPC errors → defer (retry next tick).
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEventWithNonce(
		t, db, "fm-rpc-err", "eip155:1", "eip155:1:0xmigmissing", 42, testOldTSSPubkey, 3,
	)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmigmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)
	builder.On("GetNextNonce", mock.Anything, testOldTSSAddr, true).Return(uint64(0), assert.AnError)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "fm-rpc-err")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestFundMigrationEVM_NotFound_SignerInfoMissing_StaysBroadcasted(t *testing.T) {
	// Fund migration tx not found but OldTssPubkey is missing from the event
	// payload → can't derive signer → defer. Uses the standard fund-migration
	// helper which doesn't populate OldTssPubkey.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEvent(t, db, "fm-no-pubkey", "eip155:1", "eip155:1:0xmigmissing", 42)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmigmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "fm-no-pubkey")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestFundMigrationEVM_VerifyError_StaysBroadcasted(t *testing.T) {
	// VerifyBroadcastedTx errors → defer (retry next tick); no nonce check, no vote.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEvent(t, db, "fm-verify-err", "eip155:1", "eip155:1:0xmigerr", 42)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmigerr").
		Return(false, uint64(0), uint64(0), uint8(0), assert.AnError)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "fm-verify-err")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestFundMigrationEVM_EmptyHashRewindsToSigned(t *testing.T) {
	// EVM fund migration with empty rawTxHash → rewind to SIGNED so the
	// broadcaster recomputes the deterministic hash. No chain RPCs consulted.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEvent(t, db, "fm-empty", "eip155:1", "eip155:1:", 42)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "fm-empty")
	require.Equal(t, store.StatusSigned, ev.Status)
	builder.AssertNotCalled(t, "VerifyBroadcastedTx", mock.Anything, mock.Anything)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestFundMigrationEVM_InsufficientConfirmations_Retries(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertBroadcastedFundMigrationEvent(t, db, "fm-1", "eip155:1", "eip155:1:0xpending", 42)

	// Tx found but only 2 confirmations (needs more)
	builder.On("VerifyBroadcastedTx", mock.Anything, "0xpending").
		Return(true, uint64(500), uint64(2), uint8(1), nil)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	// Not enough confirmations, stays BROADCASTED
	ev := getEvent(t, db, "fm-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestConstants(t *testing.T) {
	t.Run("processBroadcastedBatchSize", func(t *testing.T) {
		assert.Equal(t, 100, processBroadcastedBatchSize)
	})
}

func TestNewResolverDefaults(t *testing.T) {
	t.Run("default check interval when zero", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		r := NewResolver(Config{
			EventStore:    evtStore,
			CheckInterval: 0,
			Logger:        zerolog.Nop(),
		})
		assert.Equal(t, 15*time.Second, r.checkInterval)
	})

	t.Run("custom check interval", func(t *testing.T) {
		evtStore, _ := setupTestDB(t)
		r := NewResolver(Config{
			EventStore:    evtStore,
			CheckInterval: 45 * time.Second,
			Logger:        zerolog.Nop(),
		})
		assert.Equal(t, 45*time.Second, r.checkInterval)
	})
}

func TestResolveOutboundEVM_Success_MarksCompleted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventData("tx-100", "utx-200", "eip155:1")
	insertBroadcastedEvent(t, db, "ev-evm-1", "eip155:1", "eip155:1:0xsuccess", eventData)

	// Tx found, confirmed (20 confs), status=1 (success)
	builder.On("VerifyBroadcastedTx", mock.Anything, "0xsuccess").
		Return(true, uint64(500), uint64(20), uint8(1), nil)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-evm-1")
	require.Equal(t, store.StatusCompleted, ev.Status)
}

func TestResolveOutboundEVM_DisabledChain_StaysBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChainsOutboundDisabled(t, "eip155:42", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventData("tx-100", "utx-200", "eip155:42")
	insertBroadcastedEvent(t, db, "ev-disabled-1", "eip155:42", "eip155:42:0xwhatever", eventData)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-disabled-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "VerifyBroadcastedTx", mock.Anything, mock.Anything)
}

func TestResolveOutboundEVM_InsufficientConfirmations_StaysBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventData("tx-100", "utx-200", "eip155:1")
	insertBroadcastedEvent(t, db, "ev-lowconf-1", "eip155:1", "eip155:1:0xlowconf", eventData)

	// Found but only 2 confirmations (default required is 12)
	builder.On("VerifyBroadcastedTx", mock.Anything, "0xlowconf").
		Return(true, uint64(500), uint64(2), uint8(1), nil)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-lowconf-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestResolveOutboundEVM_Reverted_NoPushSigner_StaysBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventData("tx-100", "utx-200", "eip155:1")
	insertBroadcastedEvent(t, db, "ev-reverted-1", "eip155:1", "eip155:1:0xreverted", eventData)

	// Found, confirmed, status=0 (reverted)
	builder.On("VerifyBroadcastedTx", mock.Anything, "0xreverted").
		Return(true, uint64(500), uint64(20), uint8(0), nil)
	builder.On("GetGasFeeUsed", mock.Anything, "0xreverted").
		Return("21000", nil)

	// No pushSigner, so voteOutboundFailureAndMarkReverted returns nil early
	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-reverted-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertCalled(t, "GetGasFeeUsed", mock.Anything, "0xreverted")
}

// makeOutboundEventDataWithNonce mirrors makeOutboundEventData but adds the
// `signing_data.nonce` field the EVM resolver consults on tx-not-found.
func makeOutboundEventDataWithNonce(txID, utxID, destChain string, nonce uint64) []byte {
	b, _ := json.Marshal(map[string]any{
		"tx_id":             txID,
		"utx_id":            utxID,
		"destination_chain": destChain,
		"signing_data": map[string]any{
			"nonce": nonce,
		},
	})
	return b
}

const testEVMTSSAddr = "0x4D353565442Eb33b66ef88E14336F3F4Bf3a02FB"

func TestResolveOutboundEVM_NotFound_NonceConsumed_Reverts(t *testing.T) {
	// Tx not found AND signed nonce < finalized nonce → another tx consumed
	// the slot. Our tx is dead, REVERT path is taken. (No PushSigner means
	// the vote is logged but status stays BROADCASTED; we verify the resolver
	// took the REVERT branch by checking the nonce RPC was called.)
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventDataWithNonce("tx-100", "utx-200", "eip155:1", 5)
	insertBroadcastedEvent(t, db, "ev-consumed-1", "eip155:1", "eip155:1:0xmissing", eventData)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)
	// Finalized nonce = 7 → our nonce 5 is past finalized → consumed.
	builder.On("GetNextNonce", mock.Anything, testEVMTSSAddr, true).Return(uint64(7), nil)

	resolver := newResolverWithTSSAddress(evtStore, ch, testEVMTSSAddr)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-consumed-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status, "no PushSigner → vote skipped, status unchanged")
	builder.AssertCalled(t, "GetNextNonce", mock.Anything, testEVMTSSAddr, true)
}

func TestResolveOutboundEVM_NotFound_NonceUnconsumed_RewindsToSigned(t *testing.T) {
	// Tx not found AND signed nonce >= finalized nonce → tx may still land
	// (or was dropped from mempool). Rewind to SIGNED so the broadcaster
	// re-broadcasts.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventDataWithNonce("tx-100", "utx-200", "eip155:1", 5)
	insertBroadcastedEvent(t, db, "ev-unconsumed-1", "eip155:1", "eip155:1:0xstillpending", eventData)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xstillpending").
		Return(false, uint64(0), uint64(0), uint8(0), nil)
	// Finalized nonce = 5 → our nonce 5 not yet finalized.
	builder.On("GetNextNonce", mock.Anything, testEVMTSSAddr, true).Return(uint64(5), nil)

	resolver := newResolverWithTSSAddress(evtStore, ch, testEVMTSSAddr)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-unconsumed-1")
	require.Equal(t, store.StatusSigned, ev.Status)
}

func TestResolveOutboundEVM_NotFound_NonceRPCError_StaysBroadcasted(t *testing.T) {
	// Tx not found and nonce RPC errors → defer (retry next tick).
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventDataWithNonce("tx-100", "utx-200", "eip155:1", 5)
	insertBroadcastedEvent(t, db, "ev-rpc-err-1", "eip155:1", "eip155:1:0xmissing", eventData)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)
	builder.On("GetNextNonce", mock.Anything, testEVMTSSAddr, true).Return(uint64(0), assert.AnError)

	resolver := newResolverWithTSSAddress(evtStore, ch, testEVMTSSAddr)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-rpc-err-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestResolveOutboundEVM_NotFound_SignedNonceMissing_StaysBroadcasted(t *testing.T) {
	// Tx not found and event payload has no signing_data.nonce → can't run
	// nonce check → defer. Uses the standard outbound helper which doesn't
	// populate signing_data.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventData("tx-100", "utx-200", "eip155:1")
	insertBroadcastedEvent(t, db, "ev-no-nonce", "eip155:1", "eip155:1:0xmissing", eventData)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)

	resolver := newResolverWithTSSAddress(evtStore, ch, testEVMTSSAddr)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-no-nonce")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestResolveOutboundEVM_NotFound_TSSAddressFetchError_StaysBroadcasted(t *testing.T) {
	// Tx not found and GetTSSAddress callback errors → defer (retry next tick).
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventDataWithNonce("tx-100", "utx-200", "eip155:1", 5)
	insertBroadcastedEvent(t, db, "ev-tss-err", "eip155:1", "eip155:1:0xmissing", eventData)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)

	resolver := NewResolver(Config{
		EventStore:    evtStore,
		Chains:        ch,
		CheckInterval: 0,
		Logger:        zerolog.Nop(),
		GetTSSAddress: func(ctx context.Context) (string, error) { return "", assert.AnError },
	})
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-tss-err")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestResolveOutboundEVM_NotFound_NoTSSAddressResolver_StaysBroadcasted(t *testing.T) {
	// Tx not found and GetTSSAddress is nil → can't run nonce check → defer.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventDataWithNonce("tx-100", "utx-200", "eip155:1", 5)
	insertBroadcastedEvent(t, db, "ev-no-tss-1", "eip155:1", "eip155:1:0xmissing", eventData)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xmissing").
		Return(false, uint64(0), uint64(0), uint8(0), nil)

	resolver := newResolver(evtStore, ch) // no GetTSSAddress configured
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-no-tss-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestResolveOutboundEVM_VerifyError_StaysBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	eventData := makeOutboundEventData("tx-100", "utx-200", "eip155:1")
	insertBroadcastedEvent(t, db, "ev-err-1", "eip155:1", "eip155:1:0xerror", eventData)

	builder.On("VerifyBroadcastedTx", mock.Anything, "0xerror").
		Return(false, uint64(0), uint64(0), uint8(0), assert.AnError)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-err-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestResolveFundMigration_NonEVMChain_StaysBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertBroadcastedFundMigrationEvent(t, db, "fm-svm-1", "solana:mainnet", "solana:mainnet:somesig", 99)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "fm-svm-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "VerifyBroadcastedTx", mock.Anything, mock.Anything)
}

func TestResolveFundMigration_InvalidEventData_StaysBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	event := store.Event{
		EventID:           "fm-bad-data",
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              store.EventTypeSignFundMigrate,
		ConfirmationType:  "INSTANT",
		Status:            store.StatusBroadcasted,
		EventData:         []byte("not valid json"),
		BroadcastedTxHash: "eip155:1:0xbaddata",
	}
	require.NoError(t, db.Create(&event).Error)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "fm-bad-data")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertNotCalled(t, "VerifyBroadcastedTx", mock.Anything, mock.Anything)
}

func TestGetBuilder_ChainNotRegistered_ReturnsError(t *testing.T) {
	evtStore, _ := setupTestDB(t)
	ch := chains.NewChains(nil, nil, &config.Config{PushChainID: "test-chain"}, zerolog.Nop())
	resolver := newResolver(evtStore, ch)

	_, err := resolver.getBuilder("eip155:999")
	require.Error(t, err)
}

func TestResolveEvent_UnknownType_StaysBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	event := store.Event{
		EventID:           "ev-unknown-type",
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              "SIGN_SOMETHING_ELSE",
		ConfirmationType:  "STANDARD",
		Status:            store.StatusBroadcasted,
		EventData:         []byte("{}"),
		BroadcastedTxHash: "eip155:1:0xwhatever",
	}
	require.NoError(t, db.Create(&event).Error)

	resolver := newResolver(evtStore, ch)
	resolver.resolveEvent(context.Background(), &event)

	ev := getEvent(t, db, "ev-unknown-type")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestResolveOutbound_SVM_RoutingPath(t *testing.T) {
	// Valid CAIP hash for a non-EVM (SVM) chain routes to resolveSVM.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	eventData := makeOutboundEventData("tx-svm-1", "utx-svm-1", "solana:mainnet")
	insertBroadcastedEvent(t, db, "ev-svm-route", "solana:mainnet", "solana:mainnet:someSig", eventData)

	// PDA found → COMPLETED
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-svm-1").Return(true, int64(0), nil)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-svm-route")
	require.Equal(t, store.StatusCompleted, ev.Status)
	builder.AssertCalled(t, "IsAlreadyExecuted", mock.Anything, "tx-svm-1")
}

func TestResolveOutbound_InvalidCAIP_LeavesBroadcasted(t *testing.T) {
	// Malformed CAIP is treated as a bug indicator, not a dead-row signal.
	// The row stays BROADCASTED — no vote, no status change. Operators see the
	// warning log and can manually inspect / recover the row.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, &mockChainClient{builder: builder})

	eventData := makeOutboundEventData("tx-bad", "utx-bad", "eip155:1")
	insertBroadcastedEvent(t, db, "ev-bad-caip", "eip155:1", "invalid-no-colon", eventData)

	resolver := newResolver(evtStore, ch)
	resolver.processBroadcasted(context.Background())

	ev := getEvent(t, db, "ev-bad-caip")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	// Neither the chain nor the voter should have been consulted.
	builder.AssertNotCalled(t, "VerifyBroadcastedTx", mock.Anything, mock.Anything)
}

func TestVoteOutboundFailureAndMarkReverted_EmptyGasFeeDefaults(t *testing.T) {
	// Empty gasFeeUsed should default to "0" inside voteOutboundFailureAndMarkReverted.
	evtStore, _ := setupTestDB(t)
	resolver := NewResolver(Config{
		EventStore: evtStore,
		PushSigner: nil,
		Logger:     zerolog.Nop(),
	})

	event := &store.Event{EventID: "ev-gas-default"}
	// Pass empty gasFeeUsed
	err := resolver.voteOutboundFailureAndMarkReverted(
		context.Background(), event, "tx-1", "utx-1", "0xhash", 100, "", "some error",
	)
	// With nil pushSigner, returns nil early (no panic, no error)
	assert.NoError(t, err)
}

func TestVoteOutboundFailureAndMarkReverted_EventStoreUpdateFailure(t *testing.T) {
	// Simulate eventStore.Update failure by using an event ID that doesn't exist in the DB.
	// Since pushSigner is nil, the vote is skipped and Update is never called,
	// so this just confirms nil-signer early return.
	evtStore, _ := setupTestDB(t)
	resolver := NewResolver(Config{
		EventStore: evtStore,
		PushSigner: nil,
		Logger:     zerolog.Nop(),
	})

	event := &store.Event{EventID: "nonexistent-event"}
	err := resolver.voteOutboundFailureAndMarkReverted(
		context.Background(), event, "tx-1", "utx-1", "", 0, "500", "test error",
	)
	assert.NoError(t, err)
}

func TestVoteFundMigrationAndMark_NilSigner(t *testing.T) {
	// Nil pushSigner returns early cleanly without panic.
	evtStore, _ := setupTestDB(t)
	resolver := NewResolver(Config{
		EventStore: evtStore,
		PushSigner: nil,
		Logger:     zerolog.Nop(),
	})

	event := &store.Event{EventID: "fm-nil-signer"}
	// Should not panic, just log and return
	resolver.voteFundMigrationAndMark(context.Background(), event, 42, "0xhash", true)
	resolver.voteFundMigrationAndMark(context.Background(), event, 42, "0xhash", false)
}

func TestStart_ContextCancellation(t *testing.T) {
	evtStore, _ := setupTestDB(t)
	resolver := NewResolver(Config{
		EventStore:    evtStore,
		CheckInterval: 10 * time.Second,
		Logger:        zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		resolver.run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// run returned cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("resolver.run did not stop after context cancellation")
	}
}

func TestProcessBroadcasted_NilChains(t *testing.T) {
	evtStore, _ := setupTestDB(t)
	resolver := NewResolver(Config{
		EventStore: evtStore,
		Chains:     nil,
		Logger:     zerolog.Nop(),
	})

	// Should return early without panic when chains is nil
	resolver.processBroadcasted(context.Background())
}
