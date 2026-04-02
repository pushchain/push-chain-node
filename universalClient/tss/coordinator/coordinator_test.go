package coordinator

import (
	"context"
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
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

type coordMockTxBuilder struct{ mock.Mock }

func (m *coordMockTxBuilder) GetOutboundSigningRequest(ctx context.Context, data *uexecutortypes.OutboundCreatedEvent, nonce uint64) (*common.UnsignedSigningReq, error) {
	args := m.Called(ctx, data, nonce)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*common.UnsignedSigningReq), args.Error(1)
}

func (m *coordMockTxBuilder) GetNextNonce(ctx context.Context, addr string, useFinalized bool) (uint64, error) {
	args := m.Called(ctx, addr, useFinalized)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *coordMockTxBuilder) BroadcastOutboundSigningRequest(ctx context.Context, req *common.UnsignedSigningReq, data *uexecutortypes.OutboundCreatedEvent, sig []byte) (string, error) {
	args := m.Called(ctx, req, data, sig)
	return args.String(0), args.Error(1)
}

func (m *coordMockTxBuilder) VerifyBroadcastedTx(ctx context.Context, txHash string) (bool, uint64, uint64, uint8, error) {
	args := m.Called(ctx, txHash)
	return args.Bool(0), args.Get(1).(uint64), args.Get(2).(uint64), args.Get(3).(uint8), args.Error(4)
}

func (m *coordMockTxBuilder) IsAlreadyExecuted(ctx context.Context, txID string) (bool, error) {
	args := m.Called(ctx, txID)
	return args.Bool(0), args.Error(1)
}

func (m *coordMockTxBuilder) GetGasFeeUsed(ctx context.Context, txHash string) (string, error) {
	args := m.Called(ctx, txHash)
	return args.String(0), args.Error(1)
}

func (m *coordMockTxBuilder) GetFundMigrationSigningRequest(ctx context.Context, data *common.FundMigrationData, nonce uint64) (*common.UnsignedSigningReq, error) {
	args := m.Called(ctx, data, nonce)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*common.UnsignedSigningReq), args.Error(1)
}

func (m *coordMockTxBuilder) BroadcastFundMigrationTx(ctx context.Context, req *common.UnsignedSigningReq, data *common.FundMigrationData, sig []byte) (string, error) {
	args := m.Called(ctx, req, data, sig)
	return args.String(0), args.Error(1)
}

type coordMockChainClient struct {
	builder    *coordMockTxBuilder
	builderErr error
}

func (m *coordMockChainClient) Start(context.Context) error { return nil }
func (m *coordMockChainClient) Stop() error                 { return nil }
func (m *coordMockChainClient) IsHealthy() bool             { return true }
func (m *coordMockChainClient) GetTxBuilder() (common.TxBuilder, error) {
	if m.builderErr != nil {
		return nil, m.builderErr
	}
	return m.builder, nil
}

func newTestChainsForCoordinator(t *testing.T, chainID string, vmType uregistrytypes.VmType, client common.ChainClient) *chains.Chains {
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
		eligible := coord.GetEligibleUV("SIGN_OUTBOUND")
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
		{0, 1}, // edge: 0 or fewer → 1
		{1, 1},
		{3, 3}, // (2*3)/3+1 = 3
		{4, 3}, // (2*4)/3+1 = 3
		{5, 4}, // (2*5)/3+1 = 4
		{6, 5}, // (2*6)/3+1 = 5
		{9, 7}, // (2*9)/3+1 = 7
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

func TestExtractFundMigrateChain(t *testing.T) {
	t.Run("valid JSON with chain field", func(t *testing.T) {
		data := []byte(`{"migration_id":1,"chain":"eip155:421614","old_key_id":"key-1"}`)
		assert.Equal(t, "eip155:421614", extractFundMigrateChain(data))
	})

	t.Run("missing chain field", func(t *testing.T) {
		assert.Equal(t, "", extractFundMigrateChain([]byte(`{"migration_id":1}`)))
	})

	t.Run("nil or empty input", func(t *testing.T) {
		assert.Equal(t, "", extractFundMigrateChain(nil))
		assert.Equal(t, "", extractFundMigrateChain([]byte{}))
	})

	t.Run("invalid JSON", func(t *testing.T) {
		assert.Equal(t, "", extractFundMigrateChain([]byte("not-json")))
	})
}

func TestDeriveEVMAddressFromPubkey(t *testing.T) {
	t.Run("valid compressed secp256k1 pubkey", func(t *testing.T) {
		// Generator point pubkey - well-known test vector
		addr, err := DeriveEVMAddressFromPubkey("03d5d5d290a0ecec420e843fc2a57f1696781ec657e204406fc67bb5fe0c751317")
		fmt.Println("addr", addr)
		require.NoError(t, err)
		assert.True(t, len(addr) == 42, "address should be 42 chars (0x + 40 hex)")
		assert.Equal(t, "0x", addr[:2])
	})

	t.Run("deterministic output", func(t *testing.T) {
		pubkey := "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
		addr1, _ := DeriveEVMAddressFromPubkey(pubkey)
		addr2, _ := DeriveEVMAddressFromPubkey(pubkey)
		assert.Equal(t, addr1, addr2)
	})

	t.Run("different pubkeys produce different addresses", func(t *testing.T) {
		addr1, _ := DeriveEVMAddressFromPubkey("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
		addr2, _ := DeriveEVMAddressFromPubkey("02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5")
		assert.NotEqual(t, addr1, addr2)
	})

	t.Run("handles 0x prefix", func(t *testing.T) {
		addr1, _ := DeriveEVMAddressFromPubkey("0x0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
		addr2, _ := DeriveEVMAddressFromPubkey("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
		assert.Equal(t, addr1, addr2)
	})

	t.Run("invalid hex returns error", func(t *testing.T) {
		_, err := DeriveEVMAddressFromPubkey("not-hex")
		require.Error(t, err)
	})

	t.Run("wrong length returns error", func(t *testing.T) {
		_, err := DeriveEVMAddressFromPubkey("0279be667ef9dc")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 33")
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := DeriveEVMAddressFromPubkey("")
		require.Error(t, err)
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
	db.Create(&store.Event{EventID: "e1", Type: "SIGN_OUTBOUND", Status: store.StatusInProgress, EventData: ethData})
	db.Create(&store.Event{EventID: "e2", Type: "SIGN_OUTBOUND", Status: store.StatusInProgress, EventData: ethData})
	db.Create(&store.Event{EventID: "e3", Type: "SIGN_OUTBOUND", Status: store.StatusSigned, EventData: polyData})

	// These must NOT be counted.
	db.Create(&store.Event{EventID: "e4", Type: "SIGN_OUTBOUND", Status: store.StatusConfirmed, EventData: ethData})   // not yet in-flight
	db.Create(&store.Event{EventID: "e5", Type: "SIGN_OUTBOUND", Status: store.StatusBroadcasted, EventData: ethData}) // pending nonce RPC covers it
	db.Create(&store.Event{EventID: "e6", Type: "KEYGEN", Status: store.StatusInProgress})                             // not a SIGN event

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

	t.Run("chain client not found", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		builder := &coordMockTxBuilder{}
		client := &coordMockChainClient{builder: builder}
		coord.chains = newTestChainsForCoordinator(t, "eip155:1", uregistrytypes.VmType_EVM, client)

		data := []byte(`{"tx_id":"0x1","destination_chain":"eip155:999"}`)
		_, err := coord.buildSignTransaction(ctx, data, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get client for chain")
	})

	t.Run("GetTxBuilder returns error", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		client := &coordMockChainClient{builder: nil, builderErr: fmt.Errorf("builder init failed")}
		coord.chains = newTestChainsForCoordinator(t, "eip155:1", uregistrytypes.VmType_EVM, client)

		data := []byte(`{"tx_id":"0x1","destination_chain":"eip155:1"}`)
		_, err := coord.buildSignTransaction(ctx, data, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get tx builder")
	})

	t.Run("nil assignedNonce returns error", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		builder := &coordMockTxBuilder{}
		client := &coordMockChainClient{builder: builder}
		coord.chains = newTestChainsForCoordinator(t, "eip155:1", uregistrytypes.VmType_EVM, client)

		data := []byte(`{"tx_id":"0x1","destination_chain":"eip155:1"}`)
		_, err := coord.buildSignTransaction(ctx, data, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "assigned nonce is required")
	})

	t.Run("GetOutboundSigningRequest returns error", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		builder := &coordMockTxBuilder{}
		client := &coordMockChainClient{builder: builder}
		coord.chains = newTestChainsForCoordinator(t, "eip155:1", uregistrytypes.VmType_EVM, client)

		builder.On("GetOutboundSigningRequest", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, fmt.Errorf("ABI encoding failed"))

		nonce := uint64(5)
		data := []byte(`{"tx_id":"0x1","destination_chain":"eip155:1"}`)
		_, err := coord.buildSignTransaction(ctx, data, &nonce)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get outbound signing request")
	})

	t.Run("GetOutboundSigningRequest succeeds", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		builder := &coordMockTxBuilder{}
		client := &coordMockChainClient{builder: builder}
		coord.chains = newTestChainsForCoordinator(t, "eip155:1", uregistrytypes.VmType_EVM, client)

		expectedReq := &common.UnsignedSigningReq{
			SigningHash: []byte{0xaa, 0xbb},
			Nonce:       5,
		}
		builder.On("GetOutboundSigningRequest", mock.Anything, mock.Anything, uint64(5)).
			Return(expectedReq, nil)

		nonce := uint64(5)
		data := []byte(`{"tx_id":"0x1","destination_chain":"eip155:1","recipient":"0xRecipient","amount":"1000"}`)
		result, err := coord.buildSignTransaction(ctx, data, &nonce)
		require.NoError(t, err)
		assert.Equal(t, expectedReq, result)
		builder.AssertExpectations(t)
	})
}

func TestAssignSignNonce_SkippedChain(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	skippedChains := map[string]bool{"eip155:1": true}

	nonce, ok := coord.assignSignNonce(
		context.Background(),
		store.Event{EventID: "e1"},
		"eip155:1",
		map[string]int{},
		map[string]uint64{},
		skippedChains,
	)
	assert.False(t, ok)
	assert.Equal(t, uint64(0), nonce)
}

func TestAssignSignNonce_SubsequentEventUsesCache(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)

	nonceByChain := map[string]uint64{"eip155:1": 10}
	inFlightPerChain := map[string]int{"eip155:1": 1}

	nonce, ok := coord.assignSignNonce(
		context.Background(),
		store.Event{EventID: "e1"},
		"eip155:1",
		inFlightPerChain,
		nonceByChain,
		map[string]bool{},
	)
	assert.True(t, ok)
	assert.Equal(t, uint64(11), nonce)
	assert.Equal(t, 2, inFlightPerChain["eip155:1"])
}

func TestAssignSignNonce_SubsequentEventCapReached(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)

	nonceByChain := map[string]uint64{"eip155:1": 10}
	inFlightPerChain := map[string]int{"eip155:1": PerChainCap}

	nonce, ok := coord.assignSignNonce(
		context.Background(),
		store.Event{EventID: "e1"},
		"eip155:1",
		inFlightPerChain,
		nonceByChain,
		map[string]bool{},
	)
	assert.False(t, ok)
	assert.Equal(t, uint64(0), nonce)
}

func TestAssignSignNonce_FirstEventWithInFlight_SkipsUntilThreshold(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)

	inFlightPerChain := map[string]int{"eip155:1": 1}
	nonceByChain := map[string]uint64{}
	skippedChains := map[string]bool{}

	nonce, ok := coord.assignSignNonce(
		context.Background(),
		store.Event{EventID: "e1"},
		"eip155:1",
		inFlightPerChain,
		nonceByChain,
		skippedChains,
	)
	assert.False(t, ok)
	assert.Equal(t, uint64(0), nonce)
	assert.True(t, skippedChains["eip155:1"], "chain should be marked as skipped")

	coord.chainWaitMu.Lock()
	assert.Equal(t, 1, coord.consecutiveWaitPerChain["eip155:1"])
	coord.chainWaitMu.Unlock()
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

func TestGetPartyIDFromPeerID(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		partyID, err := coord.GetPartyIDFromPeerID(ctx, "peer1")
		require.NoError(t, err)
		assert.Equal(t, "validator1", partyID)
	})

	t.Run("second validator", func(t *testing.T) {
		partyID, err := coord.GetPartyIDFromPeerID(ctx, "peer2")
		require.NoError(t, err)
		assert.Equal(t, "validator2", partyID)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := coord.GetPartyIDFromPeerID(ctx, "unknown-peer")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetPeerIDFromPartyID(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		peerID, err := coord.GetPeerIDFromPartyID(ctx, "validator1")
		require.NoError(t, err)
		assert.Equal(t, "peer1", peerID)
	})

	t.Run("second validator", func(t *testing.T) {
		peerID, err := coord.GetPeerIDFromPartyID(ctx, "validator2")
		require.NoError(t, err)
		assert.Equal(t, "peer2", peerID)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := coord.GetPeerIDFromPartyID(ctx, "unknown-validator")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetMultiAddrsFromPeerID(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		addrs, err := coord.GetMultiAddrsFromPeerID(ctx, "peer1")
		require.NoError(t, err)
		assert.Equal(t, []string{"/ip4/127.0.0.1/tcp/9001"}, addrs)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := coord.GetMultiAddrsFromPeerID(ctx, "unknown-peer")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestHandleACK(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	t.Run("ack for untracked event is ignored", func(t *testing.T) {
		err := coord.HandleACK(ctx, "peer1", "unknown-event")
		assert.NoError(t, err)
	})

	t.Run("ack tracking with registered event", func(t *testing.T) {
		coord.ackMu.Lock()
		coord.ackTracking["test-event"] = &ackState{
			participants: []string{"validator1", "validator2", "validator3"},
			ackedBy:      make(map[string]bool),
			ackCount:     0,
		}
		coord.ackMu.Unlock()

		// First ACK
		err := coord.HandleACK(ctx, "peer1", "test-event")
		assert.NoError(t, err)

		coord.ackMu.RLock()
		state := coord.ackTracking["test-event"]
		assert.Equal(t, 1, state.ackCount)
		assert.True(t, state.ackedBy["peer1"])
		coord.ackMu.RUnlock()

		// Duplicate ACK from same peer should not increment
		err = coord.HandleACK(ctx, "peer1", "test-event")
		assert.NoError(t, err)

		coord.ackMu.RLock()
		assert.Equal(t, 1, coord.ackTracking["test-event"].ackCount)
		coord.ackMu.RUnlock()

		// ACK from second peer
		err = coord.HandleACK(ctx, "peer2", "test-event")
		assert.NoError(t, err)

		coord.ackMu.RLock()
		assert.Equal(t, 2, coord.ackTracking["test-event"].ackCount)
		coord.ackMu.RUnlock()
	})

	t.Run("ack from non-participant is rejected", func(t *testing.T) {
		coord.ackMu.Lock()
		coord.ackTracking["restricted-event"] = &ackState{
			participants: []string{"validator1"},
			ackedBy:      make(map[string]bool),
			ackCount:     0,
		}
		coord.ackMu.Unlock()

		// peer2 maps to validator2 which is not in participants
		err := coord.HandleACK(ctx, "peer2", "restricted-event")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a participant")
	})
}

func TestCoordinator_DoubleStartStop(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	// Start twice - second should be no-op
	coord.Start(ctx)
	time.Sleep(10 * time.Millisecond)
	coord.Start(ctx)

	coord.mu.RLock()
	assert.True(t, coord.running)
	coord.mu.RUnlock()

	// Stop
	coord.Stop()
	time.Sleep(10 * time.Millisecond)

	// Stop again - should be no-op
	coord.Stop()

	coord.mu.RLock()
	assert.False(t, coord.running)
	coord.mu.RUnlock()
}

func TestNewCoordinator_DefaultPollInterval(t *testing.T) {
	evtStore := eventstore.NewStore(nil, zerolog.Nop())
	coord := NewCoordinator(
		evtStore,
		&pushcore.Client{},
		nil, nil,
		"validator1", 100,
		0, // zero poll interval should default to 10s
		nil,
		zerolog.Nop(),
	)
	assert.Equal(t, 10*time.Second, coord.pollInterval)
}

func TestGetEligibleUV_FundMigrate(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)

	eligible := coord.GetEligibleUV("SIGN_FUND_MIGRATE")
	assert.Len(t, eligible, 2)
	addrs := validatorAddresses(eligible)
	assert.True(t, addrs["validator1"])
	assert.True(t, addrs["validator2"])
	assert.False(t, addrs["validator3"])
}

func TestCoordinator_StopWithoutStart(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	// Stop on a coordinator that was never started should not panic.
	coord.Stop()
	coord.mu.RLock()
	assert.False(t, coord.running)
	coord.mu.RUnlock()
}

func TestGetPartyIDFromPeerID_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("nil NetworkInfo is skipped", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		coord.mu.Lock()
		coord.allValidators = []*types.UniversalValidator{
			{
				IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "val-no-net"},
				NetworkInfo:   nil,
				LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
			},
		}
		coord.mu.Unlock()

		_, err := coord.GetPartyIDFromPeerID(ctx, "any-peer")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("nil IdentifyInfo with matching NetworkInfo", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		coord.mu.Lock()
		coord.allValidators = []*types.UniversalValidator{
			{
				IdentifyInfo:  nil,
				NetworkInfo:   &types.NetworkInfo{PeerId: "peer-no-id"},
				LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
			},
		}
		coord.mu.Unlock()

		// NetworkInfo matches but IdentifyInfo is nil, so the address is "" and it falls through
		_, err := coord.GetPartyIDFromPeerID(ctx, "peer-no-id")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetPeerIDFromPartyID_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("nil IdentifyInfo is skipped", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		coord.mu.Lock()
		coord.allValidators = []*types.UniversalValidator{
			{
				IdentifyInfo:  nil,
				NetworkInfo:   &types.NetworkInfo{PeerId: "peer-x"},
				LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
			},
		}
		coord.mu.Unlock()

		_, err := coord.GetPeerIDFromPartyID(ctx, "any-val")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("nil NetworkInfo with matching IdentifyInfo", func(t *testing.T) {
		coord, _, _ := setupTestCoordinator(t)
		coord.mu.Lock()
		coord.allValidators = []*types.UniversalValidator{
			{
				IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "val-no-net"},
				NetworkInfo:   nil,
				LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
			},
		}
		coord.mu.Unlock()

		// IdentifyInfo matches but NetworkInfo is nil, falls through to not-found
		_, err := coord.GetPeerIDFromPartyID(ctx, "val-no-net")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetMultiAddrsFromPeerID_NilNetworkInfo(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	coord.mu.Lock()
	coord.allValidators = []*types.UniversalValidator{
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v"},
			NetworkInfo:   nil,
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
	}
	coord.mu.Unlock()

	_, err := coord.GetMultiAddrsFromPeerID(ctx, "any-peer")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHandleACK_UnknownPeerID(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	coord.ackMu.Lock()
	coord.ackTracking["evt-unknown-peer"] = &ackState{
		participants: []string{"validator1"},
		ackedBy:      make(map[string]bool),
		ackCount:     0,
	}
	coord.ackMu.Unlock()

	err := coord.HandleACK(ctx, "totally-unknown-peer", "evt-unknown-peer")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get partyID")
}

func TestHandleACK_AllACKsTriggersBEGIN(t *testing.T) {
	coord, _, _ := setupTestCoordinator(t)
	ctx := context.Background()

	var sentMessages []string
	coord.send = func(_ context.Context, peerID string, _ []byte) error {
		sentMessages = append(sentMessages, peerID)
		return nil
	}

	coord.ackMu.Lock()
	coord.ackTracking["evt-begin"] = &ackState{
		participants: []string{"validator1", "validator2"},
		ackedBy:      map[string]bool{"peer1": true},
		ackCount:     1,
	}
	coord.ackMu.Unlock()

	// Second ACK completes the set
	err := coord.HandleACK(ctx, "peer2", "evt-begin")
	require.NoError(t, err)

	// BEGIN should have been sent to both participants
	assert.Len(t, sentMessages, 2)
	assert.Contains(t, sentMessages, "peer1")
	assert.Contains(t, sentMessages, "peer2")

	// ACK tracking should be cleaned up
	coord.ackMu.RLock()
	_, exists := coord.ackTracking["evt-begin"]
	coord.ackMu.RUnlock()
	assert.False(t, exists, "ack tracking should be removed after all ACKs received")
}

func TestGetActiveParticipants(t *testing.T) {
	validators := []*types.UniversalValidator{
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v1"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v2"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v3"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_INACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v4"}, LifecycleInfo: nil},
	}

	active := getActiveParticipants(validators)
	require.Len(t, active, 1)
	assert.Equal(t, "v1", active[0].IdentifyInfo.CoreValidatorAddress)

	assert.Nil(t, getActiveParticipants(nil))
}

func TestGetCoordinatorParticipants(t *testing.T) {
	t.Run("returns active validators when available", func(t *testing.T) {
		validators := []*types.UniversalValidator{
			{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v1"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
			{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v2"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN}},
		}
		result := getCoordinatorParticipants(validators)
		require.Len(t, result, 1)
		assert.Equal(t, "v1", result[0].IdentifyInfo.CoreValidatorAddress)
	})

	t.Run("falls back to all validators when no active", func(t *testing.T) {
		validators := []*types.UniversalValidator{
			{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v1"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN}},
		}
		result := getCoordinatorParticipants(validators)
		require.Len(t, result, 1)
		assert.Equal(t, "v1", result[0].IdentifyInfo.CoreValidatorAddress)
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		assert.Empty(t, getCoordinatorParticipants(nil))
	})
}

func TestGetSignEligible(t *testing.T) {
	validators := []*types.UniversalValidator{
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v1"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v2"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_LEAVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v3"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v4"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_INACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v5"}, LifecycleInfo: nil},
	}

	eligible := getSignEligible(validators)
	require.Len(t, eligible, 2)
	addrs := validatorAddresses(eligible)
	assert.True(t, addrs["v1"])
	assert.True(t, addrs["v2"])
	assert.False(t, addrs["v3"])
	assert.False(t, addrs["v4"])
	assert.False(t, addrs["v5"])
}

func TestGetEligibleForProtocol(t *testing.T) {
	validators := []*types.UniversalValidator{
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v1"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v2"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN}},
		{IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "v3"}, LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_LEAVE}},
	}

	t.Run("KEYGEN includes Active+PendingJoin", func(t *testing.T) {
		result := getEligibleForProtocol("KEYGEN", validators)
		assert.Len(t, result, 2)
		addrs := validatorAddresses(result)
		assert.True(t, addrs["v1"])
		assert.True(t, addrs["v2"])
	})

	t.Run("KEYREFRESH includes Active+PendingLeave", func(t *testing.T) {
		result := getEligibleForProtocol("KEYREFRESH", validators)
		assert.Len(t, result, 2)
		addrs := validatorAddresses(result)
		assert.True(t, addrs["v1"])
		assert.True(t, addrs["v3"])
	})

	t.Run("SIGN_OUTBOUND includes Active+PendingLeave", func(t *testing.T) {
		result := getEligibleForProtocol("SIGN_OUTBOUND", validators)
		assert.Len(t, result, 2)
		addrs := validatorAddresses(result)
		assert.True(t, addrs["v1"])
		assert.True(t, addrs["v3"])
	})

	t.Run("SIGN_FUND_MIGRATE includes Active+PendingLeave", func(t *testing.T) {
		result := getEligibleForProtocol("SIGN_FUND_MIGRATE", validators)
		assert.Len(t, result, 2)
	})

	t.Run("QUORUM_CHANGE includes Active+PendingJoin", func(t *testing.T) {
		result := getEligibleForProtocol("QUORUM_CHANGE", validators)
		assert.Len(t, result, 2)
		addrs := validatorAddresses(result)
		assert.True(t, addrs["v1"])
		assert.True(t, addrs["v2"])
	})

	t.Run("unknown returns nil", func(t *testing.T) {
		assert.Nil(t, getEligibleForProtocol("UNKNOWN", validators))
	})
}
