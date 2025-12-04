package coordinator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// Note: Tests that depend on mockDataProvider have been removed because
// the coordinator now uses *pushcore.Client directly (a concrete type).
// Only pure function tests are kept.

func TestGetKeygenKeyrefreshParticipants(t *testing.T) {
	validators := []*types.UniversalValidator{
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v1"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v2"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v3"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_LEAVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v4"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_INACTIVE},
		},
	}

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
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v1"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v2"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v3"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_LEAVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v4"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_PENDING_JOIN},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v5"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_INACTIVE},
		},
	}

	participants := getSignParticipants(validators)
	// Eligible: v1, v2, v3 (Active + Pending Leave)
	// Threshold for 3: (2*3)/3 + 1 = 3
	// So should return all 3
	assert.Len(t, participants, 3)

	addresses := make(map[string]bool)
	for _, v := range participants {
		if v.IdentifyInfo != nil {
			addresses[v.IdentifyInfo.CoreValidatorAddress] = true
		}
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
	validators := []*types.UniversalValidator{
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v1"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v2"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v3"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v4"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
		{
			IdentifyInfo:  &types.IdentityInfo{CoreValidatorAddress: "v5"},
			LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
		},
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
