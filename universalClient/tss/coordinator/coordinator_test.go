package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
)

// mockDataProvider is a mock implementation of DataProvider for testing.
type mockDataProvider struct {
	latestBlock      uint64
	validators       []*UniversalValidator
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

func (m *mockDataProvider) GetUniversalValidators(ctx context.Context) ([]*UniversalValidator, error) {
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

// setupTestCoordinator creates a test coordinator with mock dependencies.
func setupTestCoordinator(t *testing.T) (*Coordinator, *mockDataProvider, *eventstore.Store) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&store.TSSEvent{}))

	evtStore := eventstore.NewStore(db, zerolog.Nop())
	mockDP := &mockDataProvider{
		latestBlock:     100,
		currentTSSKeyId: "test-key-id",
		validators: []*UniversalValidator{
			{
				ValidatorAddress: "validator1",
				Status:           UVStatusActive,
				Network: NetworkInfo{
					PeerID:     "peer1",
					Multiaddrs: []string{"/ip4/127.0.0.1/tcp/9001"},
				},
			},
			{
				ValidatorAddress: "validator2",
				Status:           UVStatusActive,
				Network: NetworkInfo{
					PeerID:     "peer2",
					Multiaddrs: []string{"/ip4/127.0.0.1/tcp/9002"},
				},
			},
			{
				ValidatorAddress: "validator3",
				Status:           UVStatusPendingJoin,
				Network: NetworkInfo{
					PeerID:     "peer3",
					Multiaddrs: []string{"/ip4/127.0.0.1/tcp/9003"},
				},
			},
		},
	}

	keyshareMgr, err := keyshare.NewManager(t.TempDir(), "test-password")
	require.NoError(t, err)

	sendFn := func(ctx context.Context, peerID string, data []byte) error {
		return nil
	}

	coord := NewCoordinator(
		evtStore,
		mockDP,
		keyshareMgr,
		"validator1",
		100, // coordinatorRange
		100*time.Millisecond,
		sendFn,
		zerolog.Nop(),
	)

	return coord, mockDP, evtStore
}

func TestIsPeerCoordinator(t *testing.T) {
	coord, mockDP, _ := setupTestCoordinator(t)
	ctx := context.Background()

	// Update validators cache
	coord.updateValidators(ctx)

	t.Run("peer is coordinator", func(t *testing.T) {
		// Block 100, epoch 1, should be validator2 (index 1)
		mockDP.latestBlock = 100
		isCoord, err := coord.IsPeerCoordinator(ctx, "peer2")
		require.NoError(t, err)
		assert.True(t, isCoord, "peer2 should be coordinator at block 100")
	})

	t.Run("peer is not coordinator", func(t *testing.T) {
		// Block 100, epoch 1, should be validator2 (index 1), not validator1
		mockDP.latestBlock = 100
		isCoord, err := coord.IsPeerCoordinator(ctx, "peer1")
		require.NoError(t, err)
		assert.False(t, isCoord, "peer1 should not be coordinator at block 100")
	})

	t.Run("peer not found", func(t *testing.T) {
		mockDP.latestBlock = 100
		isCoord, err := coord.IsPeerCoordinator(ctx, "unknown-peer")
		require.NoError(t, err)
		assert.False(t, isCoord, "unknown peer should not be coordinator")
	})

	t.Run("no validators", func(t *testing.T) {
		coord.mu.Lock()
		coord.allValidators = nil
		coord.mu.Unlock()

		mockDP.latestBlock = 100
		isCoord, err := coord.IsPeerCoordinator(ctx, "peer1")
		require.NoError(t, err)
		assert.False(t, isCoord, "should return false when no validators")
	})

	t.Run("error getting block number", func(t *testing.T) {
		mockDP.getBlockNumErr = errors.New("block number error")
		isCoord, err := coord.IsPeerCoordinator(ctx, "peer1")
		require.Error(t, err)
		assert.False(t, isCoord)
	})
}

func TestGetEligibleUV(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	// Update validators cache
	coord.updateValidators(ctx)

	t.Run("keygen protocol", func(t *testing.T) {
		eligible := coord.GetEligibleUV("keygen")
		// Should return Active + Pending Join: validator1, validator2, validator3
		assert.Len(t, eligible, 3)
		addresses := make(map[string]bool)
		for _, v := range eligible {
			addresses[v.ValidatorAddress] = true
		}
		assert.True(t, addresses["validator1"])
		assert.True(t, addresses["validator2"])
		assert.True(t, addresses["validator3"])
	})

	t.Run("keyrefresh protocol", func(t *testing.T) {
		eligible := coord.GetEligibleUV("keyrefresh")
		// Should return only Active: validator1, validator2 (not validator3 which is PendingJoin)
		assert.Len(t, eligible, 2)
		addresses := make(map[string]bool)
		for _, v := range eligible {
			addresses[v.ValidatorAddress] = true
		}
		assert.True(t, addresses["validator1"])
		assert.True(t, addresses["validator2"])
		assert.False(t, addresses["validator3"]) // PendingJoin not eligible for keyrefresh
	})

	t.Run("quorumchange protocol", func(t *testing.T) {
		eligible := coord.GetEligibleUV("quorumchange")
		// Should return Active + Pending Join: validator1, validator2, validator3
		assert.Len(t, eligible, 3)
		addresses := make(map[string]bool)
		for _, v := range eligible {
			addresses[v.ValidatorAddress] = true
		}
		assert.True(t, addresses["validator1"])
		assert.True(t, addresses["validator2"])
		assert.True(t, addresses["validator3"])
	})

	t.Run("sign protocol", func(t *testing.T) {
		eligible := coord.GetEligibleUV("sign")
		// Should return random subset of Active + Pending Leave
		// validator1 and validator2 are Active, validator3 is PendingJoin (not eligible)
		// So should return validator1 and validator2 (or subset if >2/3 threshold applies)
		assert.GreaterOrEqual(t, len(eligible), 1)
		assert.LessOrEqual(t, len(eligible), 2)
	})

	t.Run("unknown protocol", func(t *testing.T) {
		eligible := coord.GetEligibleUV("unknown")
		assert.Nil(t, eligible)
	})

	t.Run("no validators", func(t *testing.T) {
		coord.mu.Lock()
		coord.allValidators = nil
		coord.mu.Unlock()

		eligible := coord.GetEligibleUV("keygen")
		assert.Nil(t, eligible)
	})
}

func TestGetKeygenKeyrefreshParticipants(t *testing.T) {
	validators := []*UniversalValidator{
		{ValidatorAddress: "v1", Status: UVStatusActive},
		{ValidatorAddress: "v2", Status: UVStatusPendingJoin},
		{ValidatorAddress: "v3", Status: UVStatusPendingLeave},
		{ValidatorAddress: "v4", Status: UVStatusInactive},
	}

	participants := getQuorumChangeParticipants(validators)
	assert.Len(t, participants, 2)
	assert.Equal(t, "v1", participants[0].ValidatorAddress)
	assert.Equal(t, "v2", participants[1].ValidatorAddress)
}

func TestGetSignParticipants(t *testing.T) {
	validators := []*UniversalValidator{
		{ValidatorAddress: "v1", Status: UVStatusActive},
		{ValidatorAddress: "v2", Status: UVStatusActive},
		{ValidatorAddress: "v3", Status: UVStatusPendingLeave},
		{ValidatorAddress: "v4", Status: UVStatusPendingJoin},
		{ValidatorAddress: "v5", Status: UVStatusInactive},
	}

	participants := getSignParticipants(validators)
	// Eligible: v1, v2, v3 (Active + Pending Leave)
	// Threshold for 3: (2*3)/3 + 1 = 3
	// So should return all 3
	assert.Len(t, participants, 3)

	addresses := make(map[string]bool)
	for _, v := range participants {
		addresses[v.ValidatorAddress] = true
	}
	assert.True(t, addresses["v1"])
	assert.True(t, addresses["v2"])
	assert.True(t, addresses["v3"])
	assert.False(t, addresses["v4"]) // PendingJoin not eligible
	assert.False(t, addresses["v5"]) // Inactive not eligible
}

func TestCalculateThreshold(t *testing.T) {
	tests := []struct {
		name            string
		numParticipants int
		expected        int
	}{
		{"3 participants", 3, 3}, // (2*3)/3 + 1 = 3
		{"4 participants", 4, 3}, // (2*4)/3 + 1 = 3
		{"5 participants", 5, 4}, // (2*5)/3 + 1 = 4
		{"6 participants", 6, 5}, // (2*6)/3 + 1 = 5
		{"1 participant", 1, 1},
		{"0 participants", 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateThreshold(tt.numParticipants)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSelectRandomThreshold(t *testing.T) {
	validators := []*UniversalValidator{
		{ValidatorAddress: "v1", Status: UVStatusActive},
		{ValidatorAddress: "v2", Status: UVStatusActive},
		{ValidatorAddress: "v3", Status: UVStatusActive},
		{ValidatorAddress: "v4", Status: UVStatusActive},
		{ValidatorAddress: "v5", Status: UVStatusActive},
	}

	t.Run("returns threshold subset", func(t *testing.T) {
		// For 5 participants, threshold is 4
		selected := selectRandomThreshold(validators)
		assert.Len(t, selected, 4)
	})

	t.Run("returns all when fewer than threshold", func(t *testing.T) {
		smallList := validators[:2] // 2 participants, threshold is 2
		selected := selectRandomThreshold(smallList)
		assert.Len(t, selected, 2)
	})

	t.Run("returns nil for empty list", func(t *testing.T) {
		selected := selectRandomThreshold(nil)
		assert.Nil(t, selected)
	})
}

func TestDeriveKeyIDBytes(t *testing.T) {
	keyID := "test-key-id"
	bytes := deriveKeyIDBytes(keyID)

	// Should be SHA256 hash (32 bytes)
	assert.Len(t, bytes, 32)
	assert.NotNil(t, bytes)

	// Should be deterministic
	bytes2 := deriveKeyIDBytes(keyID)
	assert.Equal(t, bytes, bytes2)

	// Different keyID should produce different hash
	bytes3 := deriveKeyIDBytes("different-key-id")
	assert.NotEqual(t, bytes, bytes3)
}

func TestCoordinator_StartStop(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	// Test Start
	coord.Start(ctx)
	time.Sleep(50 * time.Millisecond) // Let it run briefly

	coord.mu.RLock()
	running := coord.running
	coord.mu.RUnlock()
	assert.True(t, running, "coordinator should be running")

	// Test Stop
	coord.Stop()
	time.Sleep(50 * time.Millisecond)

	coord.mu.RLock()
	running = coord.running
	coord.mu.RUnlock()
	assert.False(t, running, "coordinator should be stopped")
}
