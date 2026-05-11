package txbroadcaster

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/rs/zerolog"
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

func (m *mockTxBuilder) IsAlreadyExecuted(ctx context.Context, txID string) (bool, error) {
	args := m.Called(ctx, txID)
	return args.Bool(0), args.Error(1)
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

func (m *mockChainClient) Start(context.Context) error                     { return nil }
func (m *mockChainClient) Stop() error                                     { return nil }
func (m *mockChainClient) IsHealthy() bool                                 { return true }
func (m *mockChainClient) GetTxBuilder() (common.TxBuilder, error) { return m.builder, nil }

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

func makeSignedOutboundData(t *testing.T, destChain string, nonce uint64) []byte {
	t.Helper()
	sig := hex.EncodeToString(make([]byte, 64))
	hash := hex.EncodeToString(make([]byte, 32))
	data := SignedOutboundData{
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
		Type:              "SIGN_OUTBOUND",
		ConfirmationType:  "STANDARD",
		Status:            store.StatusSigned,
		EventData:         makeSignedOutboundData(t, destChain, nonce),
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

func TestEVM_BroadcastError_NonceConsumed_MarksBroadcasted(t *testing.T) {
	// Broadcast fails with txHash, finalized nonce shows consumed → BROADCASTED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xabc", fmt.Errorf("already known"))
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).Return(uint64(10), nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	require.Equal(t, "eip155:1:0xabc", ev.BroadcastedTxHash)
}

func TestEVM_BroadcastSuccess_MarksBroadcasted(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 10)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xabc123", nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	require.Equal(t, "eip155:1:0xabc123", ev.BroadcastedTxHash)
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestEVM_BroadcastFails_NonceConsumedOnRecheck_MarksBroadcasted(t *testing.T) {
	// Broadcast fails, but nonce check shows it was consumed (race with another node).
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xfailed", fmt.Errorf("some RPC error"))
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).Return(uint64(6), nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestEVM_BroadcastFails_NonceNotConsumed_StaysSigned(t *testing.T) {
	// Broadcast fails with no txHash (assembly failure) → stay SIGNED for retry.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("connection refused"))

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusSigned, ev.Status) // stays SIGNED
	builder.AssertNotCalled(t, "GetNextNonce", mock.Anything, mock.Anything, mock.Anything)
}

func TestEVM_BroadcastFails_WithTxHash_NonceNotConsumed_StaysSigned(t *testing.T) {
	// Broadcast fails with txHash, but nonce not consumed → stay SIGNED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xabc", fmt.Errorf("gas too low"))
	builder.On("GetNextNonce", mock.Anything, "0xTSS", true).Return(uint64(5), nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusSigned, ev.Status) // stays SIGNED
}

func TestEVM_GetTSSAddressNil_UsesEmptyAddress(t *testing.T) {
	// getTSSAddress is nil → empty string passed to GetNextNonce on broadcast error.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xabc", fmt.Errorf("already known"))
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
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertCalled(t, "GetNextNonce", mock.Anything, "", true)
}

func TestSVM_BroadcastSuccess_MarksBroadcasted(t *testing.T) {
	// Broadcast succeeds → BROADCASTED with tx hash.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 0)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("solTxSig123", nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	require.Equal(t, "solana:mainnet:solTxSig123", ev.BroadcastedTxHash)
}

func TestSVM_BroadcastFails_PDAExists_MarksBroadcasted(t *testing.T) {
	// Broadcast fails, but ExecutedTx PDA exists → another relayer processed it → BROADCASTED.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 0)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("tx simulation failed: account already exists"))
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(true, nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	require.Equal(t, "solana:mainnet:", ev.BroadcastedTxHash) // empty tx hash
}

func TestSVM_BroadcastFails_PDANotFound_MarksBroadcasted(t *testing.T) {
	// Broadcast fails, PDA not found → permanent failure (bad payload) → BROADCASTED for resolver to REVERT.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 0)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("simulation failed: invalid instruction"))
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	require.Equal(t, "solana:mainnet:", ev.BroadcastedTxHash) // empty tx hash
}

func TestSVM_BroadcastFails_PDACheckFails_StaysSigned(t *testing.T) {
	// Broadcast fails, PDA check also fails (RPC truly down) → stays SIGNED for retry.
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "solana:mainnet", uregistrytypes.VmType_SVM, client)

	insertSignedEvent(t, db, "ev-1", "solana:mainnet", 0)

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("RPC timeout"))
	builder.On("IsAlreadyExecuted", mock.Anything, "tx-123").Return(false, fmt.Errorf("RPC down"))

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "ev-1")
	require.Equal(t, store.StatusSigned, ev.Status) // stays SIGNED
}

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

	builder.On("BroadcastOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xabc", nil)

	b := newBroadcaster(evtStore, ch, "0xTSS")
	b.processSigned(context.Background())

	ev1 := getEvent(t, db, "ev-1")
	ev2 := getEvent(t, db, "ev-2")
	require.Equal(t, store.StatusBroadcasted, ev1.Status)
	require.Equal(t, store.StatusBroadcasted, ev2.Status)
}

func TestMarkBroadcasted_FormatsCAIPTxHash(t *testing.T) {
	evtStore, db := setupTestDB(t)
	insertSignedEvent(t, db, "ev-1", "eip155:1", 5)

	b := newBroadcaster(evtStore, nil, "")
	ev := getEvent(t, db, "ev-1")
	b.markBroadcasted(&ev, "eip155:1", "0xdeadbeef")

	updated := getEvent(t, db, "ev-1")
	require.Equal(t, "eip155:1:0xdeadbeef", updated.BroadcastedTxHash)
	require.Equal(t, store.StatusBroadcasted, updated.Status)
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

// testOldTSSPubkey is a valid compressed secp256k1 pubkey for testing.
// Derived address: coordinator.DeriveEVMAddressFromPubkey will succeed with this.
const testOldTSSPubkey = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
const testNewTSSPubkey = "02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5"

func makeSignedFundMigrationData(t *testing.T, chainID string, nonce uint64) []byte {
	t.Helper()
	return makeSignedFundMigrationDataWithTransfer(t, chainID, nonce, nil)
}

func makeSignedFundMigrationDataWithTransfer(t *testing.T, chainID string, nonce uint64, transferAmount *big.Int) []byte {
	t.Helper()
	sig := hex.EncodeToString(make([]byte, 65))
	hash := hex.EncodeToString(make([]byte, 32))
	data := SignedFundMigrationData{
		FundMigrationInitiatedEventData: utsstypes.FundMigrationInitiatedEventData{
			MigrationID:      1,
			OldKeyID:         "old-key",
			OldTssPubkey:     testOldTSSPubkey,
			CurrentKeyID:     "new-key",
			CurrentTssPubkey: testNewTSSPubkey,
			Chain:            chainID,
			GasPrice:         "1000000000",
			GasLimit:         21100,
			L1GasFee:         "150",
		},
		SigningData: &SigningData{
			Signature:              sig,
			SigningHash:            hash,
			Nonce:                  nonce,
			TSSFundMigrationAmount: transferAmount,
		},
	}
	b, err := json.Marshal(data)
	require.NoError(t, err)
	return b
}

func insertSignedFundMigrationEvent(t *testing.T, db *gorm.DB, eventID, chainID string, nonce uint64) {
	t.Helper()
	event := store.Event{
		EventID:           eventID,
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              store.EventTypeSignFundMigrate,
		ConfirmationType:  "INSTANT",
		Status:            store.StatusSigned,
		EventData:         makeSignedFundMigrationData(t, chainID, nonce),
	}
	require.NoError(t, db.Create(&event).Error)
}

func TestFundMigrationEVM_BroadcastSuccess(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedFundMigrationEvent(t, db, "fm-1", "eip155:1", 0)

	// Assert the broadcaster forwards gas-limit and L1 gas fee from the signed
	// event payload into FundMigrationData; otherwise sweep math diverges
	// from what the signer hashed.
	builder.On("BroadcastFundMigrationTx",
		mock.Anything,
		mock.Anything,
		mock.MatchedBy(func(d *common.FundMigrationData) bool {
			return d.GasLimit == 21100 &&
				d.L1GasFee != nil && d.L1GasFee.String() == "150" &&
				d.GasPrice != nil && d.GasPrice.String() == "1000000000"
		}),
		mock.Anything).
		Return("0xmigrate123", nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "fm-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	require.Equal(t, "eip155:1:0xmigrate123", ev.BroadcastedTxHash)
	builder.AssertExpectations(t)
}

// TestFundMigrationEVM_TSSFundMigrationAmountThreaded asserts the tss_fund_migration_amount captured
// at signing time is decoded onto the signing req passed to BroadcastFundMigrationTx. Without
// this, the second validator's broadcast queries balance=0 (post-sweep) and the assembler
// returns "insufficient balance" — leaving the event stuck in SIGNED forever and blocking
// migration consensus.
func TestFundMigrationEVM_TSSFundMigrationAmountThreaded(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	event := store.Event{
		EventID:           "fm-transfer",
		BlockHeight:       100,
		ExpiryBlockHeight: 99999,
		Type:              store.EventTypeSignFundMigrate,
		ConfirmationType:  "INSTANT",
		Status:            store.StatusSigned,
		EventData:         makeSignedFundMigrationDataWithTransfer(t, "eip155:1", 0, new(big.Int).SetUint64(777_000_000_000_000_000)),
	}
	require.NoError(t, db.Create(&event).Error)

	builder.On("BroadcastFundMigrationTx",
		mock.Anything,
		mock.MatchedBy(func(req *common.UnsignedSigningReq) bool {
			return req.TSSFundMigrationAmount != nil && req.TSSFundMigrationAmount.String() == "777000000000000000"
		}),
		mock.Anything,
		mock.Anything).
		Return("0xmigrate777", nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "fm-transfer")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
	builder.AssertExpectations(t)
}

func TestFundMigrationEVM_BroadcastFails_NonceConsumed(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedFundMigrationEvent(t, db, "fm-1", "eip155:1", 5)

	builder.On("BroadcastFundMigrationTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xfailed", fmt.Errorf("already known"))
	builder.On("GetNextNonce", mock.Anything, mock.Anything, true).Return(uint64(10), nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "fm-1")
	require.Equal(t, store.StatusBroadcasted, ev.Status)
}

func TestMarkBroadcasted_NonExistentEvent(t *testing.T) {
	evtStore, _ := setupTestDB(t)
	b := newBroadcaster(evtStore, nil, "")

	ev := &store.Event{EventID: "does-not-exist"}
	b.markBroadcasted(ev, "eip155:1", "0xdeadbeef")
	// The method logs a warning but does not panic; verify no event was created.
}

func TestMarkBroadcasted_SetsAllFields(t *testing.T) {
	evtStore, db := setupTestDB(t)
	insertSignedEvent(t, db, "ev-fields", "eip155:1", 5)

	b := newBroadcaster(evtStore, nil, "")
	ev := getEvent(t, db, "ev-fields")
	b.markBroadcasted(&ev, "eip155:42", "0xcafe")

	updated := getEvent(t, db, "ev-fields")
	require.Equal(t, store.StatusBroadcasted, updated.Status)
	require.Equal(t, "eip155:42:0xcafe", updated.BroadcastedTxHash)
}

func TestStart_ContextCancellation(t *testing.T) {
	evtStore, _ := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	b := NewBroadcaster(Config{
		EventStore:    evtStore,
		Chains:        ch,
		CheckInterval: 50 * time.Millisecond,
		Logger:        zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// run exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit after context cancellation")
	}
}

func TestFundMigrationEVM_BroadcastFails_NonceNotConsumed_StaysSigned(t *testing.T) {
	evtStore, db := setupTestDB(t)
	builder := &mockTxBuilder{}
	client := &mockChainClient{builder: builder}
	ch := newTestChains(t, "eip155:1", uregistrytypes.VmType_EVM, client)

	insertSignedFundMigrationEvent(t, db, "fm-1", "eip155:1", 5)

	builder.On("BroadcastFundMigrationTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("0xfailed", fmt.Errorf("rpc error"))
	builder.On("GetNextNonce", mock.Anything, mock.Anything, true).Return(uint64(3), nil)

	b := newBroadcaster(evtStore, ch, "")
	b.processSigned(context.Background())

	ev := getEvent(t, db, "fm-1")
	require.Equal(t, store.StatusSigned, ev.Status) // stays SIGNED for retry
}
