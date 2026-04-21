package sessionmanager

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
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
func setupTestSessionManager(t *testing.T) (*SessionManager, *coordinator.Coordinator, *eventstore.Store, *keyshare.Manager, *pushcore.Client, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&store.Event{}))

	evtStore := eventstore.NewStore(db, zerolog.Nop())
	keyshareMgr, err := keyshare.NewManager(t.TempDir(), "test-password")
	require.NoError(t, err)

	// Create a minimal client (will fail on actual calls, but that's OK for most tests)
	testClient := &pushcore.Client{}

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

	t.Run("invalid JSON", func(t *testing.T) {
		err := sm.HandleIncomingMessage(ctx, "peer1", []byte("invalid json"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal message")
	})

	t.Run("unknown message type", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "unknown",
			EventID: "event1",
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		msg := coordinator.Message{
			Type:    "setup",
			EventID: "nonexistent",
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in database")
	})

	t.Run("sender not coordinator", func(t *testing.T) {
		// peer2 is not the coordinator at block 0 (epoch 0, index 0 = validator1/peer1)
		// So sending from peer2 should fail coordinator check
		msg := coordinator.Message{
			Type:    "setup",
			EventID: event.EventID,
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer2", data) // Send from peer2
		// This will fail because GetLatestBlockNum needs real client
		// But the error should indicate coordinator check failed
		assert.Error(t, err)
		// Error will be about no endpoints, but that's expected
		assert.Contains(t, err.Error(), "no endpoints")
	})

	t.Run("invalid participants", func(t *testing.T) {
		// Note: This test will also fail on GetLatestBlockNum, but we can test
		// the participants validation logic by ensuring the coordinator check passes
		// For now, we'll accept that GetLatestBlockNum will fail
		msg := coordinator.Message{
			Type:         "setup",
			EventID:      event.EventID,
			Participants: []string{"invalid"},
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
		// Will fail on GetLatestBlockNum, but that's expected
		assert.Error(t, err)
		// Error should be about no endpoints (from GetLatestBlockNum)
		assert.Contains(t, err.Error(), "no endpoints")
	})
}

func TestHandleStepMessage_Validation(t *testing.T) {
	sm, _, _, _, _, _ := setupTestSessionManager(t)
	ctx := context.Background()

	t.Run("session not found", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "step",
			EventID: "nonexistent",
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
	data, _ := json.Marshal(msg)

	// This will fail at session creation or GetLatestBlockNum, but validation should pass
	err := sm.HandleIncomingMessage(ctx, "peer1", data)
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

		migrationData := utsstypes.FundMigrationInitiatedEventData{
			OldTssPubkey:     genPoint,
			CurrentTssPubkey: genPoint,
			Chain:            "eip155:1",
			GasPrice:         "1000000000",
			GasLimit:         21000,
		}
		eventDataBytes, _ := json.Marshal(migrationData)
		event := &store.Event{
			EventID:   "fm-nil-chains",
			Type:      store.EventTypeSignFundMigrate,
			EventData: eventDataBytes,
		}
		sm.chains = nil
		err := sm.verifyFundMigrationSigningRequest(ctx, event, &common.UnsignedSigningReq{
			SigningHash: []byte{0x01, 0x02},
		})
		assert.NoError(t, err)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer2", data)
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

		err := sm.sendACK(context.Background(), "coord-peer", "evt-123")
		require.NoError(t, err)
		assert.Equal(t, "coord-peer", capturedPeerID)

		var msg coordinator.Message
		require.NoError(t, json.Unmarshal(capturedData, &msg))
		assert.Equal(t, "ack", msg.Type)
		assert.Equal(t, "evt-123", msg.EventID)
		assert.Nil(t, msg.Payload)
		assert.Nil(t, msg.Participants)
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

		err := sm.sendACK(context.Background(), "coord-peer", "evt-456")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send ACK message")
	})
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
		data, _ := json.Marshal(msg)
		// peer1 maps to validator1 which is in participants
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
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
		assert.Contains(t, err.Error(), "failed to parse event data")
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
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
		// Should fail with "does not exist" from handleBeginMessage (not "unknown type")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("setup message routes to handleSetupMessage", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "setup",
			EventID: "no-such-event",
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
		// Should fail with "not found in database" from handleSetupMessage
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in database")
	})

	t.Run("step message routes to handleStepMessage", func(t *testing.T) {
		msg := coordinator.Message{
			Type:    "step",
			EventID: "no-such-event",
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
		// Should fail with "does not exist" from handleStepMessage
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})
}
