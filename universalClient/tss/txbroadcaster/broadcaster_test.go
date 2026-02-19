package txbroadcaster

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"unsafe"

	"github.com/rs/zerolog"
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

func (m *mockTxBuilder) GetOutboundSigningRequest(ctx context.Context, data *uexecutortypes.OutboundCreatedEvent, gasPrice *big.Int, nonce uint64) (*common.UnSignedOutboundTxReq, error) {
	args := m.Called(ctx, data, gasPrice, nonce)
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

func (m *mockTxBuilder) VerifyBroadcastedTx(ctx context.Context, txHash string) (bool, uint64, uint8, error) {
	args := m.Called(ctx, txHash)
	return args.Bool(0), args.Get(1).(uint64), args.Get(2).(uint8), args.Error(3)
}

type mockChainClient struct{ builder *mockTxBuilder }

func (m *mockChainClient) Start(context.Context) error                          { return nil }
func (m *mockChainClient) Stop() error                                          { return nil }
func (m *mockChainClient) IsHealthy() bool                                      { return true }
func (m *mockChainClient) GetTxBuilder() (common.OutboundTxBuilder, error)      { return m.builder, nil }

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

func newTestChains(t *testing.T, chainID string, vmType uregistrytypes.VmType, client common.ChainClient) *chains.Chains {
	t.Helper()
	c := chains.NewChains(nil, nil, &config.Config{PushChainID: "test-chain"}, zerolog.Nop())

	// Inject into unexported maps via reflect+unsafe.
	v := reflect.ValueOf(c).Elem()

	chainsField := v.FieldByName("chains")
	chainsMap := *(*map[string]common.ChainClient)(unsafe.Pointer(chainsField.UnsafeAddr()))
	chainsMap[chainID] = client

	configsField := v.FieldByName("chainConfigs")
	configsMap := *(*map[string]*uregistrytypes.ChainConfig)(unsafe.Pointer(configsField.UnsafeAddr()))
	configsMap[chainID] = &uregistrytypes.ChainConfig{Chain: chainID, VmType: vmType}

	return c
}

func makeSignedEventData(t *testing.T, destChain string, nonce uint64) []byte {
	t.Helper()
	sig := hex.EncodeToString(make([]byte, 64))
	hash := hex.EncodeToString(make([]byte, 32))
	data := SignedEventData{
		OutboundCreatedEvent: uexecutortypes.OutboundCreatedEvent{
			TxID:             "tx-123",
			UniversalTxId:    "utx-456",
			DestinationChain: destChain,
			Recipient:        "0xRecipient",
			Amount:           "1000000",
		},
		SigningData: &SigningData{
			Signature:   sig,
			SigningHash: hash,
			Nonce:       nonce,
			GasPrice:    "1000000000",
		},
	}
	b, err := json.Marshal(data)
	require.NoError(t, err)
	return b
}

func insertSignedEvent(t *testing.T, db *gorm.DB, eventID, destChain string, nonce uint64) {
	t.Helper()
	event := store.Event{
		EventID:           eventID,
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              "SIGN",
		ConfirmationType:  "STANDARD",
		Status:            eventstore.StatusSigned,
		EventData:         makeSignedEventData(t, destChain, nonce),
	}
	require.NoError(t, db.Create(&event).Error)
}

func getEvent(t *testing.T, db *gorm.DB, eventID string) store.Event {
	t.Helper()
	var ev store.Event
	require.NoError(t, db.Where("event_id = ?", eventID).First(&ev).Error)
	return ev
}

func newBroadcaster(evtStore *eventstore.Store, ch *chains.Chains, tssAddr string) *Broadcaster {
	getTSSAddr := func(ctx context.Context) (string, error) { return tssAddr, nil }
	return NewBroadcaster(Config{
		EventStore:    evtStore,
		Chains:        ch,
		CheckInterval: 0, // uses default, doesn't matter for direct calls
		Logger:        zerolog.Nop(),
		GetTSSAddress: getTSSAddr,
	})
}

// ---------------------------------------------------------------------------
// EVM Tests
// ---------------------------------------------------------------------------

func TestEVM_NonceAlreadyConsumed_MarksBroadcasted(t *testing.T) {
	// Event nonce (5) < finalized nonce (10) → mark BROADCASTED without broadcasting.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).Return(uint64(10), nil)
	// BroadcastOutboundSigningRequest should NOT be called.

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
	require.Equal(t, "eip155:1:", ev.BroadcastedTxHash) // empty txHash
	builder.AssertNotCalled(t, "BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestEVM_BroadcastSuccess_MarksBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 10)

	// Finalized nonce == event nonce → proceed to broadcast.
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).Return(uint64(10), nil)
	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xabc123", nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
	require.Equal(t, "eip155:1:0xabc123", ev.BroadcastedTxHash)
}

func TestEVM_BroadcastFails_NonceConsumedOnRecheck_MarksBroadcasted(t *testing.T) {
	// Broadcast fails, but re-checking nonce shows it was consumed (race with another node).
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	// First nonce check: nonce not yet consumed.
	// Second nonce check (after broadcast error): nonce consumed.
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).
		Return(uint64(5), nil).Once()
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).
		Return(uint64(6), nil).Once()
	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xfailed", fmt.Errorf("some RPC error"))

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
}

func TestEVM_BroadcastFails_NonceNotConsumed_StaysSigned(t *testing.T) {
	// Broadcast fails, nonce still not consumed → keep SIGNED for retry.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	// Both nonce checks return the same value → nonce not consumed.
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).Return(uint64(5), nil)
	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("connection refused"))

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusSigned, ev.Status) // stays SIGNED
}

func TestEVM_PreCheckNonceFails_StillBroadcasts(t *testing.T) {
	// Pre-check GetNextNonce fails → should still attempt broadcast.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	// Pre-check fails.
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).
		Return(uint64(0), fmt.Errorf("RPC down")).Once()
	// Broadcast succeeds.
	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xabc", nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
	require.Equal(t, "eip155:1:0xabc", ev.BroadcastedTxHash)
}

func TestEVM_GetTSSAddressNil_UsesEmptyAddress(t *testing.T) {
	// getTSSAddress is nil → empty string passed to GetNextNonce.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	// Expect empty address since GetTSSAddress is nil.
	builder.On("GetNextNonce", mock.Anything, "", true).Return(uint64(10), nil)

	b := NewBroadcaster(Config{
		EventStore:    evtStore,
		Chains:        ch,
		Logger:        zerolog.Nop(),
		GetTSSAddress: nil, // explicitly nil
	})
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
	builder.AssertCalled(t, "GetNextNonce", mock.Anything, "", true)
}

// ---------------------------------------------------------------------------
// SVM Tests
// ---------------------------------------------------------------------------

func TestSVM_NonceAlreadyConsumed_MarksBroadcasted(t *testing.T) {
	// Event nonce (3) < on-chain nonce (5) → mark BROADCASTED without broadcasting.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 3)

	builder.On("GetNextNonce", mock.Anything, "", false).Return(uint64(5), nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
	require.Equal(t, "solana:mainnet:", ev.BroadcastedTxHash)
	builder.AssertNotCalled(t, "BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestSVM_NonceNotReady_StaysSigned(t *testing.T) {
	// Event nonce (5) > on-chain nonce (3) → skip, waiting for earlier nonce.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 5)

	builder.On("GetNextNonce", mock.Anything, "", false).Return(uint64(3), nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusSigned, ev.Status) // stays SIGNED
	builder.AssertNotCalled(t, "BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestSVM_NonceMatch_BroadcastSuccess(t *testing.T) {
	// Event nonce == on-chain nonce → broadcast succeeds → BROADCASTED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 7)

	builder.On("GetNextNonce", mock.Anything, "", false).Return(uint64(7), nil)
	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("solTxSig123", nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
	require.Equal(t, "solana:mainnet:solTxSig123", ev.BroadcastedTxHash)
}

func TestSVM_BroadcastFails_NonceConsumedOnRecheck_MarksBroadcasted(t *testing.T) {
	// Broadcast fails, but re-check shows nonce consumed → BROADCASTED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 7)

	// First call: nonce matches. Second call (re-check): nonce advanced.
	builder.On("GetNextNonce", mock.Anything, "", false).
		Return(uint64(7), nil).Once()
	builder.On("GetNextNonce", mock.Anything, "", false).
		Return(uint64(8), nil).Once()
	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("tx simulation failed"))

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusBroadcasted, ev.Status)
}

func TestSVM_BroadcastFails_NonceNotConsumed_StaysSigned(t *testing.T) {
	// Broadcast fails, nonce unchanged → keep SIGNED for retry.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 7)

	builder.On("GetNextNonce", mock.Anything, "", false).Return(uint64(7), nil)
	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("RPC timeout"))

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusSigned, ev.Status) // stays SIGNED
}

func TestSVM_GetNonceFails_StaysSigned(t *testing.T) {
	// GetNextNonce fails entirely → skip, stay SIGNED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 7)

	builder.On("GetNextNonce", mock.Anything, "", false).Return(uint64(0), fmt.Errorf("RPC down"))

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, eventstore.StatusSigned, ev.Status)
	builder.AssertNotCalled(t, "BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// ---------------------------------------------------------------------------
// processSigned Tests
// ---------------------------------------------------------------------------

func TestProcessSigned_NoEvents_DoesNothing(t *testing.T) {
	evtStore, _ := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background()) // no panic, no calls

	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestProcessSigned_NilChains_DoesNothing(t *testing.T) {
	evtStore, _ := setupTestDB(t)
	b := newBroadcaster(evtStore, nil, "")
	b.processSigned(context.Background()) // should not panic
}

func TestProcessSigned_MultipleEvents(t *testing.T) {
	// Two SIGNED events — both should be processed.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)
	insertSignedEvent(t, db, "ev-2", "eip155:1", 6)

	// Both nonces already consumed.
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).Return(uint64(10), nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev1 := getEvent(t, db, "ev-1")
	ev2 := getEvent(t, db, "ev-2")
	require.Equal(t, eventstore.StatusBroadcasted, ev1.Status)
	require.Equal(t, eventstore.StatusBroadcasted, ev2.Status)
}

// ---------------------------------------------------------------------------
// markBroadcasted Tests
// ---------------------------------------------------------------------------

func TestMarkBroadcasted_FormatsCAIPTxHash(t *testing.T) {
	evtStore, db := setupTestDB(t)
	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	b := newBroadcaster(evtStore, nil, "")
	ev := getEvent(t, db, "ev-1")
	b.markBroadcasted(&ev, "eip155:1", "0xdeadbeef")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, "eip155:1:0xdeadbeef", updated.BroadcastedTxHash)
	require.Equal(t, eventstore.StatusBroadcasted, updated.Status)
}

func TestMarkBroadcasted_EmptyTxHash(t *testing.T) {
	evtStore, db := setupTestDB(t)
	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 3)

	b := newBroadcaster(evtStore, nil, "")
	ev := getEvent(t, db, "ev-1")
	b.markBroadcasted(&ev, "solana:mainnet", "")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, "solana:mainnet:", updated.BroadcastedTxHash)
}
