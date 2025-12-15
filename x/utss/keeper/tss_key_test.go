package keeper_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/utss/types"
	"github.com/stretchr/testify/require"
)

func TestSetAndGetCurrentTssKey(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	key := types.TssKey{
		KeyId:                "tss-key-001",
		TssPubkey:            "pubkey123",
		Participants:         []string{"validator1", "validator2"},
		KeygenBlockHeight:    1,
		FinalizedBlockHeight: 2,
		ProcessId:            1,
	}

	// Set key
	err := f.k.SetCurrentTssKey(ctx, key)
	require.NoError(t, err)

	// Get key
	got, found, err := f.k.GetCurrentTssKey(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, key.KeyId, got.KeyId)
	require.Equal(t, key.TssPubkey, got.TssPubkey)
}

func TestGetTssKeyByID(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	key := types.TssKey{
		KeyId:                "key-abc",
		TssPubkey:            "pub123",
		Participants:         []string{"validator1", "validator2"},
		KeygenBlockHeight:    1,
		FinalizedBlockHeight: 2,
		ProcessId:            1,
	}

	err := f.k.SetCurrentTssKey(ctx, key)
	require.NoError(t, err)

	got, found, err := f.k.GetTssKeyByID(ctx, "key-abc")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, key.KeyId, got.KeyId)
	require.Equal(t, key.TssPubkey, got.TssPubkey)
}

func TestGetCurrentTssKey_NotFound(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	got, found, err := f.k.GetCurrentTssKey(ctx)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, got.KeyId)
}
