package keeper_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/utss/types"
	"github.com/stretchr/testify/require"
)

func TestFinalizeTssKeyProcess(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	process := types.TssKeyProcess{
		Id:           1,
		Status:       types.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING,
		Participants: []string{"val1", "val2"},
		BlockHeight:  100,
		ExpiryHeight: 200,
		ProcessType:  types.TssProcessType_TSS_PROCESS_KEYGEN,
	}

	// Store a pending process
	err := f.k.ProcessHistory.Set(ctx, process.Id, process)
	require.NoError(t, err)

	// Set this as the current process
	err = f.k.CurrentTssProcess.Set(ctx, process)
	require.NoError(t, err)

	// Finalize with SUCCESS
	err = f.k.FinalizeTssKeyProcess(ctx, process.Id, types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS)
	require.NoError(t, err)

	// Check ProcessHistory updated
	got, found, err := f.k.GetTssKeyProcessByID(ctx, process.Id)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS, got.Status)

	// Ensure current process is removed after finalize
	_, err = f.k.CurrentTssProcess.Get(ctx)
	require.Error(t, err)
}

func TestFinalizeTssKeyProcess_NotFound(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	err := f.k.FinalizeTssKeyProcess(ctx, 999, types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS)
	require.ErrorContains(t, err, "not found")
}

func TestGetTssKeyProcessByID(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	proc := types.TssKeyProcess{
		Id:          2,
		Status:      types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS,
		ProcessType: types.TssProcessType_TSS_PROCESS_KEYGEN,
	}
	err := f.k.ProcessHistory.Set(ctx, proc.Id, proc)
	require.NoError(t, err)

	got, found, err := f.k.GetTssKeyProcessByID(ctx, proc.Id)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, proc.Status, got.Status)
	require.Equal(t, proc.ProcessType, got.ProcessType)
}

func TestGetTssKeyProcessByID_NotFound(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	_, found, err := f.k.GetTssKeyProcessByID(ctx, 42)
	require.NoError(t, err)
	require.False(t, found)
}

func TestGetCurrentTssParticipants(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	proc := types.TssKeyProcess{
		Id:           10,
		Participants: []string{"alice", "bob"},
		ExpiryHeight: 5,
		BlockHeight:  1,
		ProcessType:  types.TssProcessType_TSS_PROCESS_KEYGEN,
	}
	err := f.k.CurrentTssProcess.Set(ctx, proc)
	require.NoError(t, err)

	// Case 1: blockHeight < ExpiryHeight → return empty
	f.ctx = f.ctx.WithBlockHeight(3)
	participants, err := f.k.GetCurrentTssParticipants(f.ctx)
	require.NoError(t, err)
	require.Empty(t, participants)

	// Case 2: blockHeight > ExpiryHeight → return participants
	f.ctx = f.ctx.WithBlockHeight(10)
	participants, err = f.k.GetCurrentTssParticipants(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "bob"}, participants)
}

func TestGetCurrentTssParticipants_NotFound(t *testing.T) {
	f := SetupTest(t)
	ctx := f.ctx

	participants, err := f.k.GetCurrentTssParticipants(ctx)
	require.NoError(t, err)
	require.Empty(t, participants)
}
