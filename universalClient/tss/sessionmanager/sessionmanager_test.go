package sessionmanager

import (
	"bytes"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/dkls"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// containsAny checks if the string contains any of the substrings.
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		for i := 0; i <= len(s)-len(substr); i++ {
			if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
				return true
			}
		}
	}
	return false
}

// mockPushCore is a stub PushCoreClient for tests so the coordinator's
// IsPeerCoordinator path doesn't need a live Push Chain RPC. Returns a fixed
// block height (0 by default) so coordinator-at-block math is deterministic.
type mockPushCore struct {
	block uint64
}

func (m *mockPushCore) GetLatestBlock(_ context.Context) (uint64, error) {
	return m.block, nil
}

func (m *mockPushCore) GetCurrentKey(_ context.Context) (*utsstypes.TssKey, error) {
	return &utsstypes.TssKey{KeyId: "test-key"}, nil
}

func (m *mockPushCore) GetAllUniversalValidators(_ context.Context) ([]*types.UniversalValidator, error) {
	return nil, nil
}

// mockSession is a mock implementation of dkls.Session for testing.
type mockSession struct {
	mock.Mock
}

func (m *mockSession) Step() ([]dkls.Message, bool, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).([]dkls.Message), args.Bool(1), args.Error(2)
}

func (m *mockSession) InputMessage(data []byte) error {
	args := m.Called(data)
	return args.Error(0)
}

func (m *mockSession) GetResult() (*dkls.Result, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dkls.Result), args.Error(1)
}

func (m *mockSession) Close() {
	m.Called()
}

// setupTestSessionManager creates a test session manager with real coordinator and test dependencies.
func setupTestSessionManager(t *testing.T) (*SessionManager, *coordinator.Coordinator, *eventstore.Store, *keyshare.Manager, *mockPushCore, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&store.Event{}))

	evtStore := eventstore.NewStore(db, zerolog.Nop())
	keyshareMgr, err := keyshare.NewManager(t.TempDir(), "test-password")
	require.NoError(t, err)

	// Inject a stub PushCoreClient so coordinator RPC paths return canned data.
	testClient := &mockPushCore{block: 0}

	sendFn := func(ctx context.Context, peerID string, data []byte) error {
		return nil
	}

	// Create test validators
	testValidators := []*types.UniversalValidator{
		{
			IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "validator1"},
			NetworkInfo: &types.NetworkInfo{
				PeerId:     "peer1",
				MultiAddrs: []string{"/ip4/127.0.0.1/tcp/9001"},
			},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "validator2"},
			NetworkInfo: &types.NetworkInfo{
				PeerId:     "peer2",
				MultiAddrs: []string{"/ip4/127.0.0.1/tcp/9002"},
			},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "validator3"},
			NetworkInfo: &types.NetworkInfo{
				PeerId:     "peer3",
				MultiAddrs: []string{"/ip4/127.0.0.1/tcp/9003"},
			},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN},
		},
	}

	coord := coordinator.NewCoordinator(
		evtStore,
		testClient,
		keyshareMgr,
		nil, // chains - nil for tests
		"validator1",
		100, // coordinatorRange
		100*time.Millisecond,
		sendFn,
		zerolog.Nop(),
	)

	// Manually set validators in coordinator for testing using reflection and unsafe
	// (since mu and allValidators are unexported)
	coordValue := reflect.ValueOf(coord).Elem()
	allValidatorsField := coordValue.FieldByName("allValidators")
	if allValidatorsField.IsValid() {
		// Use unsafe to set unexported field
		fieldPtr := unsafe.Pointer(allValidatorsField.UnsafeAddr())
		*(*[]*types.UniversalValidator)(fieldPtr) = testValidators
	}
	// Also mark the cache as freshly refreshed so IsPeerCoordinator doesn't
	// trip the staleness halt (the test fixture doesn't run the poll loop
	// that would normally populate lastValidatorsRefreshAt).
	if refreshField := coordValue.FieldByName("lastValidatorsRefreshAt"); refreshField.IsValid() {
		*(*time.Time)(unsafe.Pointer(refreshField.UnsafeAddr())) = time.Now()
	}

	sm := NewSessionManager(
		evtStore,
		coord,
		keyshareMgr,
		nil, // pushCore - nil for testing
		nil, // chains - nil for testing
		sendFn,
		"validator1",
		3*time.Minute,  // sessionExpiryTime
		30*time.Second, // sessionExpiryCheckInterval
		60,             // sessionExpiryBlockDelay
		zerolog.Nop(),
		nil,
	)

	return sm, coord, evtStore, keyshareMgr, testClient, db
}

func TestHandleIncomingMessage_InvalidMessage(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("unknown message type", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "unknown",
			EventID: "event1",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown message type")
	})
}

func TestHandleSetupMessage_Validation(t *testing.T) {
	sm, _, _, _, _, testDB := setupTestSessionManager(t)
	ctx := context.Background()

	// Create a test event by inserting it directly into the database
	event := store.Event{
		EventID:     "event1",
		BlockHeight: 100,
		Type:        "KEYGEN",
		Status:      store.StatusConfirmed,
	}
	require.NoError(t, testDB.Create(&event).Error)

	t.Run("event not found", func(t *testing.T) {
		// peer1 is the coordinator at block 0 (validator1, slot 0), so the
		// sender check passes and we reach the DB lookup, which fails.
		msg := coordinator.Message{
			Type:    "setup",
			EventID: "nonexistent",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in database")
	})

	t.Run("sender not coordinator", func(t *testing.T) {
		// peer2 is not the coordinator at block 0 (validator1/peer1 is).
		msg := coordinator.Message{
			Type:    "setup",
			EventID: event.EventID,
		}
		err := sm.HandleIncomingMessage(ctx, "peer2", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "is not the coordinator")
	})

	t.Run("invalid participants", func(t *testing.T) {
		msg := coordinator.Message{
			Type:         "setup",
			EventID:      event.EventID,
			Participants: []string{"invalid"},
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "participants validation failed")
	})

	t.Run("non-coordinator sender for non-existent event hits coord check first", func(t *testing.T) {
		// Locks in the ordering invariant: IsPeerCoordinator runs before the
		// DB lookup, so a bogus SETUP from a non-coordinator peer is rejected
		// without touching the event store even when the event id is unknown.
		msg := coordinator.Message{
			Type:    "setup",
			EventID: "nonexistent",
		}
		err := sm.HandleIncomingMessage(ctx, "peer2", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "is not the coordinator")
		assert.NotContains(t, err.Error(), "not found in database")
	})
}

func TestHandleSetupMessage_Expiry(t *testing.T) {
	sm, _, _, _, _, testDB := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("event with ExpiryBlockHeight <= current block is rejected", func(t *testing.T) {
		past := store.Event{
			EventID:           "past-event",
			BlockHeight:       1,
			Type:              "KEYGEN",
			Status:            store.StatusConfirmed,
			ExpiryBlockHeight: 1,
		}
		require.NoError(t, testDB.Create(&past).Error)

		// Bump the coordinator's mock to block 5 so 1 <= 5 fires the guard.
		setCoordinatorPushCore(sm.coordinator, &mockPushCore{block: 5})

		msg := coordinator.Message{Type: "setup", EventID: past.EventID}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "has expired")
	})

	t.Run("event with ExpiryBlockHeight 0 is treated as no-expiry", func(t *testing.T) {
		event := store.Event{
			EventID:           "no-expiry-event",
			BlockHeight:       1,
			Type:              "KEYGEN",
			Status:            store.StatusConfirmed,
			ExpiryBlockHeight: 0,
		}
		require.NoError(t, testDB.Create(&event).Error)

		setCoordinatorPushCore(sm.coordinator, &mockPushCore{block: 0})
		msg := coordinator.Message{Type: "setup", EventID: event.EventID}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		// A later check (participants) fails, but the expiry branch must not fire.
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), "has expired")
	})
}

// setCoordinatorPushCore swaps the coordinator's pushCore field via reflect+unsafe
// so individual tests can override the mock per-case.
func setCoordinatorPushCore(coord *coordinator.Coordinator, client coordinator.PushCoreClient) {
	coordValue := reflect.ValueOf(coord).Elem()
	field := coordValue.FieldByName("pushCore")
	if !field.IsValid() {
		return
	}
	*(*coordinator.PushCoreClient)(unsafe.Pointer(field.UnsafeAddr())) = client
}

func TestHandleStepMessage_Validation(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("session not found", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "step",
			EventID: "nonexistent",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("sender not in participants", func(t *testing.T) {
		// Create a mock session
		mockSess := new(mockSession)

		sm.mu.Lock()
		sm.sessions["event1"] = &sessionState{
			session:      mockSess,
			protocolType: "KEYGEN",
			coordinator:  "coordinator1",
			expiryTime:   time.Now().Add(5 * time.Minute),
			participants: []string{"validator2", "validator3"},
		}
		sm.mu.Unlock()

		// peer1 (validator1) is not in participants, so should fail
		msg := coordinator.Message{
			Type:    "step",
			EventID: "event1",
			Payload: []byte("test"),
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not in session participants")
		mockSess.AssertExpectations(t)
	})
}

// setCoordinatorValidators overrides the allValidators field in the coordinator using
// reflect+unsafe (the field is unexported and the coordinator goroutine is not started in tests).
func setCoordinatorValidators(coord *coordinator.Coordinator, validators []*types.UniversalValidator) {
	coordValue := reflect.ValueOf(coord).Elem()
	field := coordValue.FieldByName("allValidators")
	if field.IsValid() {
		*(*[]*types.UniversalValidator)(unsafe.Pointer(field.UnsafeAddr())) = validators
	}
}

func makeActiveValidator(addr string) *types.UniversalValidator {
	return &types.UniversalValidator{
		IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: addr},
		LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
	}
}

// TestValidateParticipants tests the participant validation rules for each protocol.
// SIGN uses a threshold subset (coordinator picks >2/3); keygen/keyrefresh/quorumchange require all eligible.
func TestValidateParticipants(t *testing.T) {
	sm, coord, _, _, _, _ := setupTestSessionManager(t)

	// Use 4 active validators: threshold(4) = (2*4)/3+1 = 3.
	// This lets us test a valid partial subset (3 of 4) for SIGN.
	fourActive := []*types.UniversalValidator{
		makeActiveValidator("v1"),
		makeActiveValidator("v2"),
		makeActiveValidator("v3"),
		makeActiveValidator("v4"),
	}
	setCoordinatorValidators(coord, fourActive)

	signEvent := &store.Event{EventID: "sign-1", Type: "SIGN_OUTBOUND"}
	keygenEvent := &store.Event{EventID: "keygen-1", Type: "KEYGEN"}

	// --- SIGN: threshold subset rules ---

	t.Run("SIGN: threshold subset is valid", func(t *testing.T) {
		// 3 of 4 eligible satisfies threshold(4)=3
		assert.NoError(t, sm.validateParticipants([]string{"v1", "v2", "v3"}, signEvent))
	})

	t.Run("SIGN: all eligible is also valid (threshold is a minimum)", func(t *testing.T) {
		assert.NoError(t, sm.validateParticipants([]string{"v1", "v2", "v3", "v4"}, signEvent))
	})

	t.Run("SIGN: below threshold is rejected", func(t *testing.T) {
		// 2 < threshold(4)=3
		err := sm.validateParticipants([]string{"v1", "v2"}, signEvent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "threshold")
	})

	t.Run("SIGN: non-eligible participant is rejected", func(t *testing.T) {
		err := sm.validateParticipants([]string{"v1", "v2", "unknown"}, signEvent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not eligible")
	})

	// --- SIGN_FUND_MIGRATE: same threshold rules as SIGN_OUTBOUND ---

	fmEvent := &store.Event{EventID: "fm-1", Type: store.EventTypeSignFundMigrate}

	t.Run("SIGN_FUND_MIGRATE: threshold subset is valid", func(t *testing.T) {
		assert.NoError(t, sm.validateParticipants([]string{"v1", "v2", "v3"}, fmEvent))
	})

	t.Run("SIGN_FUND_MIGRATE: below threshold is rejected", func(t *testing.T) {
		err := sm.validateParticipants([]string{"v1", "v2"}, fmEvent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "threshold")
	})

	// --- KEYGEN: exact-match rules (all eligible must participate) ---

	t.Run("KEYGEN: all eligible is valid", func(t *testing.T) {
		assert.NoError(t, sm.validateParticipants([]string{"v1", "v2", "v3", "v4"}, keygenEvent))
	})

	t.Run("KEYGEN: missing participant is rejected", func(t *testing.T) {
		err := sm.validateParticipants([]string{"v1", "v2", "v3"}, keygenEvent) // v4 missing
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match eligible count")
	})

	t.Run("KEYGEN: non-eligible participant is rejected", func(t *testing.T) {
		err := sm.validateParticipants([]string{"v1", "v2", "v3", "v4", "unknown"}, keygenEvent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not eligible")
	})
}

func TestSessionManager_Integration(t *testing.T) {
	sm, _, _, _, _, testDB := setupTestSessionManager(t)
	ctx := context.Background()

	// Create a keygen event by inserting it directly into the database
	event := store.Event{
		EventID:     "keygen-event",
		BlockHeight: 100,
		Type:        "KEYGEN",
		Status:      store.StatusConfirmed,
	}
	require.NoError(t, testDB.Create(&event).Error)

	// Create a setup message (this will fail because we can't create a real DKLS session without the library)
	// But we can test the validation logic
	msg := coordinator.Message{
		Type:         "setup",
		EventID:      event.EventID,
		Participants: []string{"validator1", "validator2", "validator3"},
		Payload:      []byte("invalid setup data"), // Will fail when creating session
	}

	// This will fail at session creation or GetLatestBlockNum, but validation should pass
	err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
	// We expect an error because we can't create a real DKLS session with invalid data
	// or because GetLatestBlockNum fails
	assert.Error(t, err)
	// Error should be about session creation, DKLS library, or no endpoints
	assert.True(t,
		containsAny(err.Error(), []string{"failed to create session", "DKLS", "dkls", "session", "no endpoints"}),
		"error should be about session creation or endpoints, got: %s", err.Error())
}

func TestVerifySigningRequest_OutboundDisabled(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	// Create a Chains manager with empty maps — IsChainOutboundEnabled returns false for all chains
	chainsManager := chains.NewChains(nil, nil, &config.Config{PushChainID: "test-chain"}, zerolog.Nop())
	sm.chains = chainsManager

	outboundData := uexecutortypes.OutboundCreatedEvent{
		DestinationChain: "eip155:1",
	}
	eventDataBytes, _ := json.Marshal(outboundData)

	event := &store.Event{
		EventID:   "sign-event-1",
		Type:      "SIGN_OUTBOUND",
		Status:    store.StatusConfirmed,
		EventData: eventDataBytes,
	}

	req := &common.UnsignedSigningReq{
		SigningHash: []byte{0x01, 0x02, 0x03},
	}

	t.Run("rejects signing when outbound disabled for destination chain", func(t *testing.T) {
		err := sm.verifyOutboundSigningRequest(ctx, event, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outbound disabled")
		assert.Contains(t, err.Error(), "eip155:1")
	})

	t.Run("nil chains skips outbound check", func(t *testing.T) {
		sm.chains = nil
		err := sm.verifyOutboundSigningRequest(ctx, event, req)
		// Should pass the outbound check (skipped) and fail later on gas price validation
		// or hash verification — but NOT on "outbound disabled"
		if err != nil {
			assert.NotContains(t, err.Error(), "outbound disabled")
		}
	})
}

func TestVerifyOutboundSigningRequest_Validation(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	outboundData := uexecutortypes.OutboundCreatedEvent{
		DestinationChain: "eip155:1",
	}
	eventDataBytes, _ := json.Marshal(outboundData)

	event := &store.Event{
		EventID:   "sign-event-1",
		Type:      "SIGN_OUTBOUND",
		Status:    store.StatusConfirmed,
		EventData: eventDataBytes,
	}

	t.Run("nil request is rejected", func(t *testing.T) {
		err := sm.verifyOutboundSigningRequest(ctx, event, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsigned transaction request is required")
	})

	t.Run("empty signing hash is rejected", func(t *testing.T) {
		err := sm.verifyOutboundSigningRequest(ctx, event, &common.UnsignedSigningReq{
			SigningHash: []byte{},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signing hash is missing")
	})

	t.Run("invalid event data JSON is rejected", func(t *testing.T) {
		badEvent := &store.Event{
			EventID:   "sign-bad-json",
			Type:      "SIGN_OUTBOUND",
			EventData: []byte("not-json"),
		}
		err := sm.verifyOutboundSigningRequest(ctx, badEvent, &common.UnsignedSigningReq{
			SigningHash: []byte{0x01},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse outbound event data")
	})

	t.Run("empty destination chain is rejected", func(t *testing.T) {
		noChainData := uexecutortypes.OutboundCreatedEvent{}
		noChainBytes, _ := json.Marshal(noChainData)
		noChainEvent := &store.Event{
			EventID:   "sign-no-chain",
			Type:      "SIGN_OUTBOUND",
			EventData: noChainBytes,
		}
		err := sm.verifyOutboundSigningRequest(ctx, noChainEvent, &common.UnsignedSigningReq{
			SigningHash: []byte{0x01},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "destination chain is missing")
	})

	t.Run("nil chains manager skips hash verification and succeeds", func(t *testing.T) {
		sm.chains = nil
		err := sm.verifyOutboundSigningRequest(ctx, event, &common.UnsignedSigningReq{
			SigningHash: []byte{0x01, 0x02},
		})
		assert.NoError(t, err)
	})
}

func TestVerifyFundMigrationSigningRequest_Validation(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("nil request is rejected", func(t *testing.T) {
		event := &store.Event{EventID: "fm-1", Type: store.EventTypeSignFundMigrate}
		err := sm.verifyFundMigrationSigningRequest(ctx, event, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsigned transaction request is required")
	})

	t.Run("empty signing hash is rejected", func(t *testing.T) {
		event := &store.Event{EventID: "fm-2", Type: store.EventTypeSignFundMigrate}
		err := sm.verifyFundMigrationSigningRequest(ctx, event, &common.UnsignedSigningReq{
			SigningHash: []byte{},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signing hash is missing")
	})

	t.Run("invalid event data JSON is rejected", func(t *testing.T) {
		event := &store.Event{
			EventID:   "fm-bad-json",
			Type:      store.EventTypeSignFundMigrate,
			EventData: []byte("not valid json"),
		}
		err := sm.verifyFundMigrationSigningRequest(ctx, event, &common.UnsignedSigningReq{
			SigningHash: []byte{0x01},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse fund migration event data")
	})

	t.Run("invalid old TSS pubkey is rejected", func(t *testing.T) {
		migrationData := utsstypes.FundMigrationInitiatedEventData{
			OldTssPubkey:     "not-a-valid-pubkey",
			CurrentTssPubkey: "also-invalid",
			Chain:            "eip155:1",
		}
		eventDataBytes, _ := json.Marshal(migrationData)
		event := &store.Event{
			EventID:   "fm-bad-pubkey",
			Type:      store.EventTypeSignFundMigrate,
			EventData: eventDataBytes,
		}
		err := sm.verifyFundMigrationSigningRequest(ctx, event, &common.UnsignedSigningReq{
			SigningHash: []byte{0x01},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to derive old TSS address")
	})

	t.Run("invalid current TSS pubkey is rejected", func(t *testing.T) {
		// Valid old key but invalid current key
		validPub := "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
		migrationData := utsstypes.FundMigrationInitiatedEventData{
			OldTssPubkey:     validPub,
			CurrentTssPubkey: "not-valid",
			Chain:            "eip155:1",
		}
		eventDataBytes, _ := json.Marshal(migrationData)
		event := &store.Event{
			EventID:   "fm-bad-cur-pubkey",
			Type:      store.EventTypeSignFundMigrate,
			EventData: eventDataBytes,
		}
		err := sm.verifyFundMigrationSigningRequest(ctx, event, &common.UnsignedSigningReq{
			SigningHash: []byte{0x01},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to derive current TSS address")
	})

	t.Run("nil chains manager skips hash verification and succeeds", func(t *testing.T) {
		// Use the well-known secp256k1 generator point (valid compressed pubkey)
		genPoint := "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"

		// Include L1GasFee to assert the new proto field survives JSON roundtrip
		// through the event data on its way into verifyFundMigrationSigningRequest.
		migrationData := utsstypes.FundMigrationInitiatedEventData{
			OldTssPubkey:     genPoint,
			CurrentTssPubkey: genPoint,
			Chain:            "eip155:10",
			GasPrice:         "1000000000",
			GasLimit:         21100,
			L1GasFee:         "150",
		}
		eventDataBytes, _ := json.Marshal(migrationData)
		event := &store.Event{
			EventID:   "fm-nil-chains",
			Type:      store.EventTypeSignFundMigrate,
			EventData: eventDataBytes,
		}
		sm.chains = nil
		req := &common.UnsignedSigningReq{SigningHash: []byte{0x01, 0x02}}
		err := sm.verifyFundMigrationSigningRequest(ctx, event, req)
		assert.NoError(t, err)
		assert.Nil(t, req.TSSFundMigrationAmount, "amount stays nil when chain/builder is skipped")
	})
}

func TestHandleSetupMessage_EventStatus(t *testing.T) {
	sm, _, _, _, _, testDB := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("event in IN_PROGRESS status is rejected", func(t *testing.T) {
		event := store.Event{
			EventID:     "event-in-progress",
			BlockHeight: 100,
			Type:        "KEYGEN",
			Status:      store.StatusInProgress,
		}
		require.NoError(t, testDB.Create(&event).Error)

		msg := coordinator.Message{
			Type:    "setup",
			EventID: "event-in-progress",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in confirmed status")
	})

	t.Run("event in COMPLETED status is rejected", func(t *testing.T) {
		event := store.Event{
			EventID:     "event-completed",
			BlockHeight: 100,
			Type:        "KEYGEN",
			Status:      store.StatusCompleted,
		}
		require.NoError(t, testDB.Create(&event).Error)

		msg := coordinator.Message{
			Type:    "setup",
			EventID: "event-completed",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in confirmed status")
	})

	t.Run("event in REVERTED status is rejected", func(t *testing.T) {
		event := store.Event{
			EventID:     "event-reverted",
			BlockHeight: 100,
			Type:        "KEYGEN",
			Status:      store.StatusReverted,
		}
		require.NoError(t, testDB.Create(&event).Error)

		msg := coordinator.Message{
			Type:    "setup",
			EventID: "event-reverted",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in confirmed status")
	})

	t.Run("event in SIGNED status is rejected", func(t *testing.T) {
		event := store.Event{
			EventID:     "event-signed",
			BlockHeight: 100,
			Type:        "SIGN_OUTBOUND",
			Status:      store.StatusSigned,
		}
		require.NoError(t, testDB.Create(&event).Error)

		msg := coordinator.Message{
			Type:    "setup",
			EventID: "event-signed",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in confirmed status")
	})

	t.Run("duplicate setup for existing session is silently ignored", func(t *testing.T) {
		// Pre-create a session entry
		mockSess := new(mockSession)
		sm.mu.Lock()
		sm.sessions["event-dup"] = &sessionState{
			session:      mockSess,
			protocolType: "KEYGEN",
			coordinator:  "peer1",
			expiryTime:   time.Now().Add(5 * time.Minute),
			participants: []string{"validator1", "validator2"},
		}
		sm.mu.Unlock()

		msg := coordinator.Message{
			Type:    "setup",
			EventID: "event-dup",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.NoError(t, err, "duplicate setup should be silently ignored")
	})
}

func TestHandleBeginMessage(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("session not found", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "begin",
			EventID: "nonexistent",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("sender is not the session coordinator", func(t *testing.T) {
		mockSess := new(mockSession)
		sm.mu.Lock()
		sm.sessions["begin-event-1"] = &sessionState{
			session:      mockSess,
			protocolType: "KEYGEN",
			coordinator:  "peer1",
			expiryTime:   time.Now().Add(5 * time.Minute),
			participants: []string{"validator1", "validator2"},
		}
		sm.mu.Unlock()

		msg := coordinator.Message{
			Type:    "begin",
			EventID: "begin-event-1",
		}
		err := sm.HandleIncomingMessage(ctx, "peer2", &msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "begin message must come from coordinator")
	})
}

func TestSendACK(t *testing.T) {
	t.Run("marshals and sends ACK message correctly", func(t *testing.T) {
		var capturedPeerID string
		var capturedData []byte

		sendFn := func(ctx context.Context, peerID string, data []byte) error {
			capturedPeerID = peerID
			capturedData = data
			return nil
		}

		sm := NewSessionManager(
			nil, nil, nil, nil, nil,
			sendFn,
			"validator1",
			3*time.Minute, 30*time.Second, 60,
			zerolog.Nop(),
			nil,
		)

		err := sm.sendACK(context.Background(), "coord-peer", "evt-123", nil)
		require.NoError(t, err)
		assert.Equal(t, "coord-peer", capturedPeerID)

		var msg coordinator.Message
		require.NoError(t, json.Unmarshal(capturedData, &msg))
		assert.Equal(t, coordinator.MessageTypeACK, msg.Type)
		assert.Equal(t, "evt-123", msg.EventID)
		assert.Nil(t, msg.Payload)
		assert.Nil(t, msg.Participants)
		assert.Nil(t, msg.SignedData)
	})

	t.Run("returns error when send fails", func(t *testing.T) {
		sendFn := func(ctx context.Context, peerID string, data []byte) error {
			return fmt.Errorf("network error")
		}

		sm := NewSessionManager(
			nil, nil, nil, nil, nil,
			sendFn,
			"validator1",
			3*time.Minute, 30*time.Second, 60,
			zerolog.Nop(),
			nil,
		)

		err := sm.sendACK(context.Background(), "coord-peer", "evt-456", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send ACK message")
	})

	t.Run("SignedData payload round-trips through JSON", func(t *testing.T) {
		var capturedData []byte
		sendFn := func(_ context.Context, _ string, data []byte) error {
			capturedData = data
			return nil
		}
		sm := NewSessionManager(
			nil, nil, nil, nil, nil,
			sendFn,
			"validator1",
			3*time.Minute, 30*time.Second, 60,
			zerolog.Nop(),
			nil,
		)

		signed := &coordinator.SignedDataPayload{
			Signature:              bytes.Repeat([]byte{0xaa}, 64),
			SigningHash:            bytes.Repeat([]byte{0xbb}, 32),
			Nonce:                  42,
			TSSFundMigrationAmount: big.NewInt(123_456),
		}
		require.NoError(t, sm.sendACK(context.Background(), "coord-peer", "evt-signed", signed))

		var msg coordinator.Message
		require.NoError(t, json.Unmarshal(capturedData, &msg))
		assert.Equal(t, coordinator.MessageTypeACK, msg.Type)
		assert.Equal(t, "evt-signed", msg.EventID)
		require.NotNil(t, msg.SignedData)
		assert.Equal(t, signed.Signature, msg.SignedData.Signature)
		assert.Equal(t, signed.SigningHash, msg.SignedData.SigningHash)
		assert.Equal(t, signed.Nonce, msg.SignedData.Nonce)
		require.NotNil(t, msg.SignedData.TSSFundMigrationAmount)
		assert.Equal(t, 0, signed.TSSFundMigrationAmount.Cmp(msg.SignedData.TSSFundMigrationAmount))
	})
}

func TestHandleSetupMessage_PriorSignedDataShortCircuits(t *testing.T) {
	cases := []struct {
		name   string
		status string
	}{
		{"SIGNED", store.StatusSigned},
		{"BROADCASTED", store.StatusBroadcasted},
		{"COMPLETED", store.StatusCompleted},
		{"REVERTED", store.StatusReverted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sm, _, _, _, _, testDB := setupTestSessionManager(t)

			sigHex := hex.EncodeToString(bytes.Repeat([]byte{0xaa}, 64))
			hashHex := hex.EncodeToString(bytes.Repeat([]byte{0xbb}, 32))
			eventData, _ := json.Marshal(map[string]any{
				"signing_data": map[string]any{
					"signature":    sigHex,
					"signing_hash": hashHex,
					"nonce":        uint64(7),
				},
			})
			require.NoError(t, testDB.Create(&store.Event{
				EventID:     "evt-" + tc.name,
				BlockHeight: 100,
				Type:        store.EventTypeSignOutbound,
				Status:      tc.status,
				EventData:   eventData,
			}).Error)

			var sent []coordinator.Message
			sm.send = func(_ context.Context, _ string, data []byte) error {
				var m coordinator.Message
				require.NoError(t, json.Unmarshal(data, &m))
				sent = append(sent, m)
				return nil
			}

			msg := coordinator.Message{Type: coordinator.MessageTypeSetup, EventID: "evt-" + tc.name}
			err := sm.HandleIncomingMessage(context.Background(), "peer1", &msg)
			require.NoError(t, err)

			sm.mu.RLock()
			_, sessionCreated := sm.sessions["evt-"+tc.name]
			sm.mu.RUnlock()
			assert.False(t, sessionCreated, "no session must be created on short-circuit")

			require.Len(t, sent, 1, "exactly one ACK must be sent")
			ack := sent[0]
			assert.Equal(t, coordinator.MessageTypeACK, ack.Type)
			assert.Equal(t, "evt-"+tc.name, ack.EventID)
			require.NotNil(t, ack.SignedData)
			assert.Equal(t, uint64(7), ack.SignedData.Nonce)
			assert.Len(t, ack.SignedData.Signature, 64)
			assert.Len(t, ack.SignedData.SigningHash, 32)
		})
	}
}

func TestCleanSession(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)

	mockSess := new(mockSession)
	mockSess.On("Close").Return()

	sm.mu.Lock()
	sm.sessions["clean-evt"] = &sessionState{
		session:      mockSess,
		protocolType: "KEYGEN",
		coordinator:  "peer1",
		expiryTime:   time.Now().Add(5 * time.Minute),
		participants: []string{"validator1"},
	}
	sm.mu.Unlock()

	state := sm.sessions["clean-evt"]
	sm.cleanSession("clean-evt", state)

	sm.mu.RLock()
	_, exists := sm.sessions["clean-evt"]
	sm.mu.RUnlock()
	assert.False(t, exists, "session should be removed after cleanup")
	mockSess.AssertCalled(t, "Close")
}

func TestStart_ContextCancellation(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	// Use a very short check interval so the goroutine ticks quickly.
	sm.sessionExpiryCheckInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	sm.Start(ctx)

	// Let it run a couple of ticks, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Give goroutine time to exit — no panic, no hang.
	time.Sleep(50 * time.Millisecond)
}

func TestHandleStepMessage_InputAndStep(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("successful input routes to session", func(t *testing.T) {
		mockSess := new(mockSession)
		mockSess.On("InputMessage", []byte("step-payload")).Return(nil)
		// processSessionStep will call Step()
		mockSess.On("Step").Return([]dkls.Message{}, false, nil)

		sm.mu.Lock()
		sm.sessions["step-evt"] = &sessionState{
			session:      mockSess,
			protocolType: "KEYGEN",
			coordinator:  "peer1",
			expiryTime:   time.Now().Add(5 * time.Minute),
			participants: []string{"validator1", "validator2"},
		}
		sm.mu.Unlock()

		msg := coordinator.Message{
			Type:    "step",
			EventID: "step-evt",
			Payload: []byte("step-payload"),
		}
		// peer1 maps to validator1 which is in participants
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		assert.NoError(t, err)
		mockSess.AssertCalled(t, "InputMessage", []byte("step-payload"))
		mockSess.AssertCalled(t, "Step")
	})

	t.Run("InputMessage error is propagated", func(t *testing.T) {
		mockSess := new(mockSession)
		mockSess.On("InputMessage", []byte("bad-data")).Return(fmt.Errorf("decode error"))

		sm.mu.Lock()
		sm.sessions["step-err-evt"] = &sessionState{
			session:      mockSess,
			protocolType: "KEYGEN",
			coordinator:  "peer1",
			expiryTime:   time.Now().Add(5 * time.Minute),
			participants: []string{"validator1"},
		}
		sm.mu.Unlock()

		msg := coordinator.Message{
			Type:    "step",
			EventID: "step-err-evt",
			Payload: []byte("bad-data"),
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to input message")
	})
}

func TestHandleSigningComplete(t *testing.T) {
	sm, _, _, _, _, testDB := setupTestSessionManager(t)

	t.Run("nil signing request returns error", func(t *testing.T) {
		err := sm.handleSigningComplete(context.Background(), "evt-1", []byte(`{}`), []byte{0x01}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signing request is nil")
	})

	t.Run("invalid event data JSON returns error", func(t *testing.T) {
		req := &common.UnsignedSigningReq{
			SigningHash: []byte{0xab, 0xcd},
			Nonce:       42,
		}
		err := sm.handleSigningComplete(context.Background(), "evt-2", []byte("not json"), []byte{0x01}, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse event data")
	})

	t.Run("successful signing complete persists data", func(t *testing.T) {
		// Create an event in DB to be updated
		event := store.Event{
			EventID:     "sign-complete-1",
			BlockHeight: 200,
			Type:        store.EventTypeSignOutbound,
			Status:      store.StatusInProgress,
			EventData:   []byte(`{"destination_chain":"eip155:1","recipient":"0xabc"}`),
		}
		require.NoError(t, testDB.Create(&event).Error)

		req := &common.UnsignedSigningReq{
			SigningHash: []byte{0xde, 0xad},
			Nonce:       99,
		}
		err := sm.handleSigningComplete(context.Background(), "sign-complete-1", event.EventData, []byte{0xbe, 0xef}, req)
		require.NoError(t, err)

		// Verify event was updated
		var updated store.Event
		require.NoError(t, testDB.Where("event_id = ?", "sign-complete-1").First(&updated).Error)
		assert.Equal(t, store.StatusSigned, updated.Status)

		// Verify signing_data was injected into event_data
		var rawData map[string]any
		require.NoError(t, json.Unmarshal(updated.EventData, &rawData))
		signingData, ok := rawData["signing_data"].(map[string]any)
		require.True(t, ok, "signing_data should be present in event data")
		assert.Equal(t, "beef", signingData["signature"])
		assert.Equal(t, "dead", signingData["signing_hash"])
		assert.Equal(t, float64(99), signingData["nonce"])
		_, hasAmount := signingData["tss_fund_migration_amount"]
		assert.False(t, hasAmount, "tss_fund_migration_amount is omitted for outbound events")
	})

	t.Run("fund migration signing complete persists tss_fund_migration_amount", func(t *testing.T) {
		event := store.Event{
			EventID:     "fm-complete-1",
			BlockHeight: 250,
			Type:        store.EventTypeSignFundMigrate,
			Status:      store.StatusInProgress,
			EventData:   []byte(`{"migration_id":7,"chain":"eip155:1"}`),
		}
		require.NoError(t, testDB.Create(&event).Error)

		req := &common.UnsignedSigningReq{
			SigningHash:            []byte{0xca, 0xfe},
			Nonce:                  3,
			TSSFundMigrationAmount: new(big.Int).SetUint64(123456789),
		}
		err := sm.handleSigningComplete(context.Background(), "fm-complete-1", event.EventData, []byte{0xbe, 0xef}, req)
		require.NoError(t, err)

		var updated store.Event
		require.NoError(t, testDB.Where("event_id = ?", "fm-complete-1").First(&updated).Error)
		assert.Equal(t, store.StatusSigned, updated.Status)

		// Decode the field into *big.Int directly — unmarshalling into map[string]any
		// would coerce the JSON number into float64 and lose precision for wei values.
		var decoded struct {
			SigningData struct {
				TSSFundMigrationAmount *big.Int `json:"tss_fund_migration_amount"`
			} `json:"signing_data"`
		}
		require.NoError(t, json.Unmarshal(updated.EventData, &decoded))
		require.NotNil(t, decoded.SigningData.TSSFundMigrationAmount,
			"tss_fund_migration_amount must survive the sign→broadcast handoff so broadcast reproduces the signed tx")
		assert.Equal(t, "123456789", decoded.SigningData.TSSFundMigrationAmount.String())
	})
}

func TestHandleIncomingMessage_Routing(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("begin message routes to handleBeginMessage", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "begin",
			EventID: "no-such-event",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		// Should fail with "does not exist" from handleBeginMessage (not "unknown type")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("setup message routes to handleSetupMessage", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "setup",
			EventID: "no-such-event",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		// Should fail with "not found in database" from handleSetupMessage
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in database")
	})

	t.Run("step message routes to handleStepMessage", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "step",
			EventID: "no-such-event",
		}
		err := sm.HandleIncomingMessage(ctx, "peer1", &msg)
		// Should fail with "does not exist" from handleStepMessage
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})
}

// TestHandleSignatureBroadcast_IdempotentAcrossStatuses locks in the
// idempotency invariant: a signature_broadcast arriving for an event already
// past CONFIRMED is a no-op (no error, no DB write). Failing this test would
// mean a duplicate broadcast could overwrite a SIGNED event, leak a session,
// or cause a redundant tx broadcast.
func TestHandleSignatureBroadcast_IdempotentAcrossStatuses(t *testing.T) {
	statuses := []string{
		store.StatusSigned,
		store.StatusBroadcasted,
		store.StatusCompleted,
		store.StatusReverted,
	}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			sm, _, _, _, _, testDB := setupTestSessionManager(t)
			ctx := context.Background()

			origEventData, _ := json.Marshal(map[string]any{"x": 1})
			require.NoError(t, testDB.Create(&store.Event{
				EventID:     "evt-idem-" + status,
				BlockHeight: 100,
				Type:        store.EventTypeSignOutbound,
				Status:      status,
				EventData:   origEventData,
			}).Error)

			msg := coordinator.Message{
				Type:    coordinator.MessageTypeSignatureBroadcast,
				EventID: "evt-idem-" + status,
				SignedData: &coordinator.SignedDataPayload{
					Signature:   bytes.Repeat([]byte{0xff}, 64), // intentionally garbage — should never be verified
					SigningHash: bytes.Repeat([]byte{0xee}, 32),
					Nonce:       42,
				},
			}
			// No error, no event mutation, no verify attempt.
			require.NoError(t, sm.HandleIncomingMessage(ctx, "peer1", &msg))

			var after store.Event
			require.NoError(t, testDB.Where("event_id = ?", "evt-idem-"+status).First(&after).Error)
			assert.Equal(t, status, after.Status, "status must not change on idempotent skip")
			assert.JSONEq(t, string(origEventData), string(after.EventData),
				"event_data must not change on idempotent skip")
		})
	}
}

// TestBroadcastSignature_SelfSkipAndFanout exercises sessionmanager's
// fanout: validators are iterated, self is excluded by partyID, and per-peer
// send failures don't abort the loop. Locks in the Option C invariants on
// the sender side.
func TestBroadcastSignature_SelfSkipAndFanout(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)

	var sentTo []string
	sendFn := func(_ context.Context, peerID string, _ []byte) error {
		// Fail on peer2 to verify per-peer failure tolerance.
		if peerID == "peer2" {
			return fmt.Errorf("simulated network error")
		}
		sentTo = append(sentTo, peerID)
		return nil
	}
	sm.send = sendFn

	sm.broadcastSignature(context.Background(), "evt-fanout", &coordinator.SignedDataPayload{
		Signature:   bytes.Repeat([]byte{0x01}, 64),
		SigningHash: bytes.Repeat([]byte{0x02}, 32),
		Nonce:       1,
	})

	// Test fixture has validator1 (sm.partyID), validator2 (peer2), validator3 (peer3).
	// validator1 is self → skipped. peer2's send fails → not counted. peer3 succeeds.
	assert.NotContains(t, sentTo, "peer1", "self (validator1/peer1) must be skipped")
	assert.NotContains(t, sentTo, "peer2", "failed send must not appear in successful sends")
	assert.Contains(t, sentTo, "peer3", "non-failing non-self peer must receive the broadcast")
}

// TestBroadcastSignature_EmptyValidatorCache verifies the fanout no-ops
// gracefully when the validator cache is empty (stale on the coordinator side).
func TestBroadcastSignature_EmptyValidatorCache(t *testing.T) {
	sm, coord, _, _, _, _ := setupTestSessionManager(t)

	// Clear the validator cache by reaching into the coordinator.
	coordValue := reflect.ValueOf(coord).Elem()
	if f := coordValue.FieldByName("allValidators"); f.IsValid() {
		*(*[]*types.UniversalValidator)(unsafe.Pointer(f.UnsafeAddr())) = nil
	}

	called := false
	sm.send = func(_ context.Context, _ string, _ []byte) error {
		called = true
		return nil
	}
	sm.broadcastSignature(context.Background(), "evt-empty", &coordinator.SignedDataPayload{
		Signature:   bytes.Repeat([]byte{0x01}, 64),
		SigningHash: bytes.Repeat([]byte{0x02}, 32),
	})
	assert.False(t, called, "no send should happen when validator set is empty")
}

// TestExtractSignedDataFromEvent_CorruptDataIsObservable verifies that
// malformed signing_data is surfaced via the error return rather than silently
// swallowed. Audit-relevant: future DB corruption must be diagnosable.
func TestExtractSignedDataFromEvent_CorruptDataIsObservable(t *testing.T) {
	t.Run("nil event returns no error", func(t *testing.T) {
		signed, err := extractSignedDataFromEvent(nil)
		assert.Nil(t, signed)
		assert.NoError(t, err)
	})

	t.Run("no signing_data returns no error", func(t *testing.T) {
		ev := &store.Event{EventData: []byte(`{"x":1}`)}
		signed, err := extractSignedDataFromEvent(ev)
		assert.Nil(t, signed)
		assert.NoError(t, err)
	})

	t.Run("bad JSON returns error", func(t *testing.T) {
		ev := &store.Event{EventData: []byte("not json")}
		signed, err := extractSignedDataFromEvent(ev)
		assert.Nil(t, signed)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal")
	})

	t.Run("bad hex in signature returns error", func(t *testing.T) {
		ev := &store.Event{EventData: []byte(`{"signing_data":{"signature":"not-hex","signing_hash":"deadbeef","nonce":1}}`)}
		signed, err := extractSignedDataFromEvent(ev)
		assert.Nil(t, signed)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode signing_data.signature hex")
	})

	t.Run("valid signing_data returns payload", func(t *testing.T) {
		ev := &store.Event{EventData: []byte(`{"signing_data":{"signature":"aabb","signing_hash":"cc","nonce":42}}`)}
		signed, err := extractSignedDataFromEvent(ev)
		require.NoError(t, err)
		require.NotNil(t, signed)
		assert.Equal(t, []byte{0xaa, 0xbb}, signed.Signature)
		assert.Equal(t, []byte{0xcc}, signed.SigningHash)
		assert.Equal(t, uint64(42), signed.Nonce)
	})
}
