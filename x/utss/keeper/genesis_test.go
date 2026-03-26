package keeper_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/utss/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	f := SetupTest(t)

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
	}

	f.k.InitGenesis(f.ctx, genesisState)

	got := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, got)
}

func TestGenesisExportImportRoundTrip(t *testing.T) {
	f := SetupTest(t)
	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams()})

	// Populate state: set a TSS key
	tssKey := types.TssKey{
		TssPubkey:            "pubkey123",
		KeyId:                "key-1",
		Participants:         []string{"val1", "val2"},
		FinalizedBlockHeight: 10,
		KeygenBlockHeight:    5,
		ProcessId:            1,
	}
	require.NoError(t, f.k.CurrentTssKey.Set(f.ctx, tssKey))
	require.NoError(t, f.k.TssKeyHistory.Set(f.ctx, "key-1", tssKey))

	// Populate state: set a process
	process := types.TssKeyProcess{
		Id:           1,
		Status:       types.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS,
		ProcessType:  types.TssProcessType_TSS_PROCESS_KEYGEN,
		Participants: []string{"val1", "val2"},
		BlockHeight:  5,
		ExpiryHeight: 100,
	}
	require.NoError(t, f.k.CurrentTssProcess.Set(f.ctx, process))
	require.NoError(t, f.k.ProcessHistory.Set(f.ctx, uint64(1), process))

	// Set next process ID
	require.NoError(t, f.k.NextProcessId.Set(f.ctx, 2))

	// Export
	exported := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, exported)
	require.NotNil(t, exported.CurrentTssKey)
	require.Equal(t, "pubkey123", exported.CurrentTssKey.TssPubkey)
	require.Len(t, exported.TssKeyHistory, 1)
	require.NotNil(t, exported.CurrentTssProcess)
	require.Len(t, exported.ProcessHistory, 1)
	require.Equal(t, uint64(2), exported.NextProcessId)

	// Re-init on fresh fixture
	f2 := SetupTest(t)
	f2.k.InitGenesis(f2.ctx, exported)

	// Export again and compare
	reExported := f2.k.ExportGenesis(f2.ctx)
	require.NotNil(t, reExported.CurrentTssKey)
	require.Equal(t, exported.CurrentTssKey.TssPubkey, reExported.CurrentTssKey.TssPubkey)
	require.Equal(t, exported.CurrentTssKey.KeyId, reExported.CurrentTssKey.KeyId)
	require.Len(t, reExported.TssKeyHistory, 1)
	require.NotNil(t, reExported.CurrentTssProcess)
	require.Equal(t, exported.CurrentTssProcess.Id, reExported.CurrentTssProcess.Id)
	require.Len(t, reExported.ProcessHistory, 1)
	require.Equal(t, exported.NextProcessId, reExported.NextProcessId)
}

func TestGenesisEmptyState(t *testing.T) {
	f := SetupTest(t)
	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams()})

	// Export with no TSS key or process set
	exported := f.k.ExportGenesis(f.ctx)
	require.NotNil(t, exported)
	require.Nil(t, exported.CurrentTssKey)
	require.Nil(t, exported.CurrentTssProcess)
	require.Empty(t, exported.TssKeyHistory)
	require.Empty(t, exported.ProcessHistory)
}
