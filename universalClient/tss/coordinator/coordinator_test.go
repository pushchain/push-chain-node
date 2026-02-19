package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// setupTestCoordinator creates a test coordinator with in-memory dependencies.
// The pushcore client is a zero-value *pushcore.Client that will fail on any live RPC call —
// tests that need coordinator logic should use the pure-function helpers directly.
func setupTestCoordinator(t *testing.T) (*Coordinator, *eventstore.Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&store.Event{}))

	evtStore := eventstore.NewStore(db, zerolog.Nop())
	keyshareMgr, err := keyshare.NewManager(t.TempDir(), "test-password")
	require.NoError(t, err)

	sendFn := func(_ context.Context, _ string, _ []byte) error { return nil }

	testValidators := []*types.UniversalValidator{
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "validator1"},
			NetworkInfo:   &types.NetworkInfo{PeerId: "peer1", MultiAddrs: []string{"/ip4/127.0.0.1/tcp/9001"}},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "validator2"},
			NetworkInfo:   &types.NetworkInfo{PeerId: "peer2", MultiAddrs: []string{"/ip4/127.0.0.1/tcp/9002"}},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "validator3"},
			NetworkInfo:   &types.NetworkInfo{PeerId: "peer3", MultiAddrs: []string{"/ip4/127.0.0.1/tcp/9003"}},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN},
		},
	}

	coord := NewCoordinator(
		evtStore,
		&pushcore.Client{}, // real but empty — fails on any RPC call (expected)
		keyshareMgr,
		nil, // chains — nil for most tests
		"validator1",
		100, // coordinatorRange
		100*time.Millisecond,
		sendFn,
		zerolog.Nop(),
	)

	coord.mu.Lock()
	coord.allValidators = testValidators
	coord.mu.Unlock()

	return coord, evtStore, db
}

// validatorAddresses extracts the CoreValidatorAddress set from a validator slice.
func validatorAddresses(vs []*types.UniversalValidator) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		if v.IdentifyInfo != nil {
			m[v.IdentifyInfo.CoreValidatorAddress] = true
		}
	}
	return m
}

// --- Coordinator rotation logic ---

// TestCoordinatorAddressForBlock tests the pure coordinator-rotation helper.
// This covers the logic that was previously only reachable through IsPeerCoordinator
// (which requires a live pushcore client and cannot be easily unit-tested).
func TestCoordinatorAddressForBlock(t *testing.T) {
	active1 := &types.UniversalValidator{
		IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "validator1"},
		LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
	}
	active2 := &types.UniversalValidator{
		IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "validator2"},
		LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
	}
	pendingJoin := &types.UniversalValidator{
		IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "validator3"},
		LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN},
	}
	validators := []*types.UniversalValidator{active1, active2, pendingJoin}

	const coordRange = 100

	t.Run("epoch 0 → first active validator", func(t *testing.T) {
		// block 0: epoch = 0/100 = 0, pool=[v1,v2], idx = 0%2 = 0 → validator1
		assert.Equal(t, "validator1", coordinatorAddressForBlock(validators, coordRange, 0))
	})

	t.Run("epoch 1 → second active validator", func(t *testing.T) {
		// block 100: epoch = 1, idx = 1%2 = 1 → validator2
		assert.Equal(t, "validator2", coordinatorAddressForBlock(validators, coordRange, 100))
	})

	t.Run("epoch 2 wraps back to first", func(t *testing.T) {
		// block 200: epoch = 2, idx = 2%2 = 0 → validator1
		assert.Equal(t, "validator1", coordinatorAddressForBlock(validators, coordRange, 200))
	})

	t.Run("mid-epoch block uses current epoch floor", func(t *testing.T) {
		// block 150: epoch = 150/100 = 1, idx = 1%2 = 1 → validator2
		assert.Equal(t, "validator2", coordinatorAddressForBlock(validators, coordRange, 150))
	})

	t.Run("PendingJoin validators are excluded from coordinator pool", func(t *testing.T) {
		// Active pool = [v1, v2]; v3 (PendingJoin) must never be selected.
		for block := uint64(0); block < 400; block += 100 {
			addr := coordinatorAddressForBlock(validators, coordRange, block)
			assert.NotEqual(t, "validator3", addr, "PendingJoin should not be coordinator at block %d", block)
		}
	})

	t.Run("no active validators falls back to all validators", func(t *testing.T) {
		// Bootstrap / single-node case: no Active validators → use all (just pendingJoin here).
		pendingOnly := []*types.UniversalValidator{pendingJoin}
		assert.Equal(t, "validator3", coordinatorAddressForBlock(pendingOnly, coordRange, 0))
	})

	t.Run("empty validator list returns empty string", func(t *testing.T) {
		assert.Equal(t, "", coordinatorAddressForBlock(nil, coordRange, 0))
		assert.Equal(t, "", coordinatorAddressForBlock([]*types.UniversalValidator{}, coordRange, 0))
	})
}

// --- Participant selection ---

func TestGetEligibleUV(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)

	coord.mu.RLock()
	require.NotEmpty(t, coord.allValidators, "validators must be set in setup")
	coord.mu.RUnlock()

	// Fixture: validator1=Active, validator2=Active, validator3=PendingJoin

	t.Run("keygen includes Active+PendingJoin", func(t *testing.T) {
		eligible := coord.GetEligibleUV("KEYGEN")
		require.Len(t, eligible, 3, "keygen = active(2) + pending_join(1)")
		addrs := validatorAddresses(eligible)
		assert.True(t, addrs["validator1"])
		assert.True(t, addrs["validator2"])
		assert.True(t, addrs["validator3"])
	})

	t.Run("keyrefresh includes only Active+PendingLeave (not PendingJoin)", func(t *testing.T) {
		eligible := coord.GetEligibleUV("KEYREFRESH")
		assert.Len(t, eligible, 2, "keyrefresh = active(2); pending_join excluded")
		addrs := validatorAddresses(eligible)
		assert.True(t, addrs["validator1"])
		assert.True(t, addrs["validator2"])
		assert.False(t, addrs["validator3"], "PendingJoin not eligible for keyrefresh")
	})

	t.Run("quorum_change includes Active+PendingJoin", func(t *testing.T) {
		eligible := coord.GetEligibleUV("QUORUM_CHANGE")
		require.Len(t, eligible, 3)
		addrs := validatorAddresses(eligible)
		assert.True(t, addrs["validator1"])
		assert.True(t, addrs["validator2"])
		assert.True(t, addrs["validator3"])
	})

	t.Run("sign returns ALL Active+PendingLeave (no random selection here)", func(t *testing.T) {
		// GetEligibleUV is used for validation, not coordinator setup.
		// validator1 and validator2 are Active; validator3 is PendingJoin (not eligible for sign).
		eligible := coord.GetEligibleUV("SIGN")
		assert.Len(t, eligible, 2, "sign = active(2); pending_join excluded")
		addrs := validatorAddresses(eligible)
		assert.True(t, addrs["validator1"])
		assert.True(t, addrs["validator2"])
		assert.False(t, addrs["validator3"])
	})

	t.Run("unknown protocol returns nil", func(t *testing.T) {
		assert.Nil(t, coord.GetEligibleUV("unknown"))
	})

	t.Run("no validators returns nil", func(t *testing.T) {
		coord.mu.Lock()
		coord.allValidators = nil
		coord.mu.Unlock()
		assert.Nil(t, coord.GetEligibleUV("KEYGEN"))
	})
}

func TestGetKeygenKeyrefreshParticipants(t *testing.T) {
	validators := []*types.UniversalValidator{
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v1"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v2"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v3"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_LEAVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v4"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_INACTIVE}},
	}

	// getQuorumChangeParticipants returns Active + PendingJoin only.
	participants := getQuorumChangeParticipants(validators)
	assert.Len(t, participants, 2)
	if participants[0].IdentifyInfo != nil {
		assert.Equal(t, "v1", participants[0].IdentifyInfo.CoreValidatorAddress)
	}
	if participants[1].IdentifyInfo != nil {
		assert.Equal(t, "v2", participants[1].IdentifyInfo.CoreValidatorAddress)
	}
}

func TestGetSignParticipants(t *testing.T) {
	validators := []*types.UniversalValidator{
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v1"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v2"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v3"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_LEAVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v4"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v5"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_INACTIVE}},
	}

	// Eligible: v1 (Active), v2 (Active), v3 (PendingLeave) = 3 eligible.
	// threshold(3) = (2*3)/3 + 1 = 3, so all 3 are returned.
	participants := getSignParticipants(validators)
	assert.Len(t, participants, 3)
	addrs := validatorAddresses(participants)
	assert.True(t, addrs["v1"])
	assert.True(t, addrs["v2"])
	assert.True(t, addrs["v3"])
	assert.False(t, addrs["v4"], "PendingJoin not eligible for sign")
	assert.False(t, addrs["v5"], "Inactive not eligible for sign")
}

// --- Threshold and random selection ---

func TestCalculateThreshold(t *testing.T) {
	tests := []struct {
		n        int
		expected int
	}{
		{0, 1},  // edge: 0 or fewer → 1
		{1, 1},
		{3, 3},  // (2*3)/3+1 = 3
		{4, 3},  // (2*4)/3+1 = 3
		{5, 4},  // (2*5)/3+1 = 4
		{6, 5},  // (2*6)/3+1 = 5
		{9, 7},  // (2*9)/3+1 = 7
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, CalculateThreshold(tt.n), "n=%d", tt.n)
	}
}

func TestSelectRandomThreshold(t *testing.T) {
	makeN := func(n int) []*types.UniversalValidator {
		vs := make([]*types.UniversalValidator, n)
		names := []string{"A", "B", "C", "D", "E", "F"}
		for i := range vs {
			vs[i] = &types.UniversalValidator{
				IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: names[i]},
				LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
			}
		}
		return vs
	}

	t.Run("returns exactly threshold count", func(t *testing.T) {
		// threshold(5) = 4
		assert.Len(t, selectRandomThreshold(makeN(5)), 4)
	})

	t.Run("returns all when count equals threshold", func(t *testing.T) {
		// threshold(2) = 2 → returns all 2
		assert.Len(t, selectRandomThreshold(makeN(2)), 2)
	})

	t.Run("returns all when count is below threshold", func(t *testing.T) {
		// threshold(1) = 1 → returns all 1
		assert.Len(t, selectRandomThreshold(makeN(1)), 1)
	})

	t.Run("returns nil for empty list", func(t *testing.T) {
		assert.Nil(t, selectRandomThreshold(nil))
	})
}

// --- Utility functions ---

func TestExtractDestinationChain(t *testing.T) {
	t.Run("valid JSON with destination_chain", func(t *testing.T) {
		data := []byte(`{"tx_id":"0x1","destination_chain":"ethereum","amount":"100"}`)
		assert.Equal(t, "ethereum", extractDestinationChain(data))
	})

	t.Run("missing destination_chain field", func(t *testing.T) {
		assert.Equal(t, "", extractDestinationChain([]byte(`{"tx_id":"0x1"}`)))
	})

	t.Run("nil or empty input", func(t *testing.T) {
		assert.Equal(t, "", extractDestinationChain(nil))
		assert.Equal(t, "", extractDestinationChain([]byte{}))
	})

	t.Run("invalid JSON", func(t *testing.T) {
		assert.Equal(t, "", extractDestinationChain([]byte("not-json")))
	})
}

func TestDeriveKeyIDBytes(t *testing.T) {
	b := deriveKeyIDBytes("test-key-id")
	assert.Len(t, b, 32, "SHA256 hash is always 32 bytes")

	// Deterministic: same input → same output.
	assert.Equal(t, b, deriveKeyIDBytes("test-key-id"))

	// Different inputs produce different hashes.
	assert.NotEqual(t, b, deriveKeyIDBytes("different-key-id"))
}

// --- In-flight counting ---

func TestGetInFlightSignCountPerChain(t *testing.T) {
	coord, _, db := setupTestCoordinator(t)

	ethData := []byte(`{"destination_chain":"ethereum"}`)
	polyData := []byte(`{"destination_chain":"polygon"}`)

	// IN_PROGRESS and SIGNED both count as in-flight.
	db.Create(&store.Event{EventID: "e1", Type: "SIGN", Status: eventstore.StatusInProgress, EventData: ethData})
	db.Create(&store.Event{EventID: "e2", Type: "SIGN", Status: eventstore.StatusInProgress, EventData: ethData})
	db.Create(&store.Event{EventID: "e3", Type: "SIGN", Status: eventstore.StatusSigned, EventData: polyData})

	// These must NOT be counted.
	db.Create(&store.Event{EventID: "e4", Type: "SIGN", Status: eventstore.StatusConfirmed, EventData: ethData})    // not yet in-flight
	db.Create(&store.Event{EventID: "e5", Type: "SIGN", Status: eventstore.StatusBroadcasted, EventData: ethData}) // pending nonce RPC covers it
	db.Create(&store.Event{EventID: "e6", Type: "KEYGEN", Status: eventstore.StatusInProgress})                    // not a SIGN event

	perChain, err := coord.getInFlightSignCountPerChain()
	require.NoError(t, err)
	assert.Equal(t, 2, perChain["ethereum"])
	assert.Equal(t, 1, perChain["polygon"])
	assert.Zero(t, perChain["arbitrum"], "unknown chain has zero count")
}

// --- Sign transaction building ---

func TestBuildSignTransaction(t *testing.T) {
	ctx := context.Background()

	t.Run("empty event data", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		_, err := coord.buildSignTransaction(ctx, []byte{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		_, err := coord.buildSignTransaction(ctx, []byte("not json"), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal")
	})

	t.Run("missing tx_id", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		_, err := coord.buildSignTransaction(ctx, []byte(`{"destination_chain":"ethereum"}`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tx_id")
	})

	t.Run("missing destination_chain", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		_, err := coord.buildSignTransaction(ctx, []byte(`{"tx_id":"0x1"}`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "destination_chain")
	})

	t.Run("chains manager not configured", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t) // chains is nil in test setup
		data := []byte(`{"tx_id":"0x1","destination_chain":"ethereum"}`)
		_, err := coord.buildSignTransaction(ctx, data, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chains manager not configured")
	})
}

// --- Lifecycle ---

func TestCoordinator_StartStop(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	coord.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	coord.mu.RLock()
	assert.True(t, coord.running, "should be running after Start")
	coord.mu.RUnlock()

	coord.Stop()
	time.Sleep(50 * time.Millisecond)
	coord.mu.RLock()
	assert.False(t, coord.running, "should be stopped after Stop")
	coord.mu.RUnlock()
}
