package sessionmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/universalClient/tss/dkls"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// containsAny checks if the string contains any of the substrings.
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
	}
	return false
}

// Note: We can't easily mock *coordinator.Coordinator since it's a concrete type.
// For testing, we'll use a real coordinator with mock dependencies.

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

// mockDataProvider is a mock implementation of coordinator.DataProvider for testing.
type mockDataProvider struct {
	latestBlock      uint64
	validators       []*types.UniversalValidator
	currentTSSKeyId  string
	getBlockNumErr   error
	getValidatorsErr error
	getKeyIdErr      error
}

func (m *mockDataProvider) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	if m.getBlockNumErr != nil {
		return 0, m.getBlockNumErr
	}
	return m.latestBlock, nil
}

func (m *mockDataProvider) GetUniversalValidators(ctx context.Context) ([]*types.UniversalValidator, error) {
	if m.getValidatorsErr != nil {
		return nil, m.getValidatorsErr
	}
	return m.validators, nil
}

func (m *mockDataProvider) GetCurrentTSSKeyId(ctx context.Context) (string, error) {
	if m.getKeyIdErr != nil {
		return "", m.getKeyIdErr
	}
	return m.currentTSSKeyId, nil
}

// setupTestSessionManager creates a test session manager with real coordinator and mock dependencies.
func setupTestSessionManager(t *testing.T) (*SessionManager, *coordinator.Coordinator, *eventstore.Store, *keyshare.Manager, *mockDataProvider, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&store.TSSEvent{}))

	evtStore := eventstore.NewStore(db, zerolog.Nop())
	keyshareMgr, err := keyshare.NewManager(t.TempDir(), "test-password")
	require.NoError(t, err)

	mockDP := &mockDataProvider{
		latestBlock:     100,
		currentTSSKeyId: "test-key-id",
		validators: []*types.UniversalValidator{
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
		},
	}

	sendFn := func(ctx context.Context, peerID string, data []byte) error {
		return nil
	}

	coord := coordinator.NewCoordinator(
		evtStore,
		mockDP,
		keyshareMgr,
		"validator1",
		100, // coordinatorRange
		100*time.Millisecond,
		sendFn,
		zerolog.Nop(),
	)

	sm := NewSessionManager(
		evtStore,
		coord,
		keyshareMgr,
		sendFn,
		"validator1",
		3*time.Minute, // sessionExpiryTime
		zerolog.Nop(),
	)

	return sm, coord, evtStore, keyshareMgr, mockDP, db
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
	sm, coord, _, _, mockDP, testDB := setupTestSessionManager(t)
	ctx := context.Background()

	// Update coordinator's validator cache by calling IsPeerCoordinator which will update cache if empty
	_, _ = coord.IsPeerCoordinator(ctx, "peer1")

	// Create a test event by inserting it directly into the database
	event := store.TSSEvent{
		EventID:      "event1",
		ProtocolType: "keygen",
		Status:       eventstore.StatusPending,
		BlockNumber:  100,
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
		// Set block so peer1 is not coordinator
		mockDP.latestBlock = 100 // epoch 1, should be validator2 (index 1)

		msg := coordinator.Message{
			Type:    "setup",
			EventID: event.EventID,
		}
		data, _ := json.Marshal(msg)
		err := sm.HandleIncomingMessage(ctx, "peer1", data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not the coordinator")
	})

	t.Run("invalid participants", func(t *testing.T) {
		// Set block so peer1 is coordinator
		mockDP.latestBlock = 0 // epoch 0, should be validator1 (index 0)

		// Ensure coordinator cache is populated by calling GetPartyIDFromPeerID
		// This will trigger updateValidators if cache is empty
		_, _ = coord.GetPartyIDFromPeerID(ctx, "peer1")

		// Now verify peer1 is coordinator
		isCoord, err := coord.IsPeerCoordinator(ctx, "peer1")
		require.NoError(t, err)
		require.True(t, isCoord, "peer1 should be coordinator at block 0")

		msg := coordinator.Message{
			Type:         "setup",
			EventID:      event.EventID,
			Participants: []string{"invalid"},
		}
		data, _ := json.Marshal(msg)
		err = sm.HandleIncomingMessage(ctx, "peer1", data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "participants validation failed")
	})
}

func TestHandleStepMessage_Validation(t *testing.T) {
	sm, coord, _, _, mockDP, _ := setupTestSessionManager(t)
	ctx := context.Background()

	// Update coordinator's validator cache (using reflection or internal method)
	// For now, we'll trigger it by calling IsPeerCoordinator which will update cache if empty
	_, _ = coord.IsPeerCoordinator(ctx, "peer1")

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
			protocolType: "keygen",
			coordinator:  "coordinator1",
			expiryTime:   time.Now().Add(5 * time.Minute),
			participants: []string{"validator2", "validator3"},
		}
		sm.mu.Unlock()

		// Set up mockDP so GetPartyIDFromPeerID works
		mockDP.validators = []*types.UniversalValidator{
			{
				IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "validator1"},
				NetworkInfo:  &types.NetworkInfo{PeerId: "peer1"},
			},
		}
		// Trigger validator cache update by calling IsPeerCoordinator
		_, _ = coord.IsPeerCoordinator(ctx, "peer1")

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

func TestSessionManager_Integration(t *testing.T) {
	sm, coord, _, _, mockDP, testDB := setupTestSessionManager(t)
	ctx := context.Background()

	// Set block so peer1 is coordinator
	mockDP.latestBlock = 0 // epoch 0, should be validator1 (index 0)

	// Ensure coordinator cache is populated by calling GetPartyIDFromPeerID
	// This will trigger updateValidators if cache is empty
	_, _ = coord.GetPartyIDFromPeerID(ctx, "peer1")

	// Verify peer1 is coordinator
	isCoord, err := coord.IsPeerCoordinator(ctx, "peer1")
	require.NoError(t, err)
	require.True(t, isCoord, "peer1 should be coordinator at block 0")

	// Create a keygen event by inserting it directly into the database
	event := store.TSSEvent{
		EventID:      "keygen-event",
		ProtocolType: "keygen",
		Status:       eventstore.StatusPending,
		BlockNumber:  100,
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

	// This will fail at session creation, but validation should pass
	err = sm.HandleIncomingMessage(ctx, "peer1", data)
	// We expect an error because we can't create a real DKLS session with invalid data
	assert.Error(t, err)
	// Error should be about session creation or DKLS library, not validation
	assert.True(t,
		containsAny(err.Error(), []string{"failed to create session", "DKLS", "dkls", "session"}),
		"error should be about session creation, got: %s", err.Error())
}
