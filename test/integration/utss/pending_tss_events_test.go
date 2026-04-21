package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	utsskeeper "github.com/pushchain/push-chain-node/x/utss/keeper"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

func TestPendingTssProcess(t *testing.T) {

	t.Run("pending tss process exists after initiation", func(t *testing.T) {
		testApp, ctx, _ := setupTssKeyProcessTest(t, 4)

		err := testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		// CurrentTssProcess should exist and be PENDING
		current, err := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING, current.Status)
		require.NotZero(t, current.Id)

		// Process should also exist in ProcessHistory
		process, found, err := testApp.UtssKeeper.GetTssKeyProcessByID(ctx, current.Id)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING, process.Status)
	})

	t.Run("pending tss process removed after finalization via quorum", func(t *testing.T) {
		testApp, ctx, validators := setupTssKeyProcessTest(t, 3)

		err := testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		process, _ := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		processId := process.Id

		pub := "pub-finalize"
		key := "key-finalize"

		// All 3 validators vote -- quorum should be reached and process finalized
		for _, v := range validators {
			valAddr, _ := sdk.ValAddressFromBech32(v)
			err := testApp.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key, processId)
			require.NoError(t, err)
		}

		// CurrentTssProcess should no longer exist (removed after finalization)
		_, err = testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		require.Error(t, err, "CurrentTssProcess should be cleared after finalization")

		// ProcessHistory should still have it, but with SUCCESS status
		historicalProcess, found, err := testApp.UtssKeeper.GetTssKeyProcessByID(ctx, processId)
		require.NoError(t, err)
		require.True(t, found, "process should remain in history")
		require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS, historicalProcess.Status)
	})

	t.Run("pending tss process removed after force-expiry by new initiation", func(t *testing.T) {
		testApp, ctx, _ := setupTssKeyProcessTest(t, 3)

		// First initiation
		err := testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		oldProcess, _ := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		oldProcessId := oldProcess.Id

		// Second initiation force-expires the old one
		err = testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_QUORUM_CHANGE)
		require.NoError(t, err)

		// The old process should be in history with a force-expired expiry height
		expiredProcess, found, err := testApp.UtssKeeper.GetTssKeyProcessByID(ctx, oldProcessId)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, ctx.BlockHeight()-1, expiredProcess.ExpiryHeight,
			"old process should be force-expired")

		// CurrentTssProcess should be the new one
		newCurrent, err := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		require.NoError(t, err)
		require.NotEqual(t, oldProcessId, newCurrent.Id)
		require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING, newCurrent.Status)
	})

	t.Run("AllProcesses returns only finalized after quorum", func(t *testing.T) {
		testApp, ctx, validators := setupTssKeyProcessTest(t, 3)

		// Initiate process 1 and finalize it
		err := testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		process1, _ := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		process1Id := process1.Id

		for _, v := range validators {
			valAddr, _ := sdk.ValAddressFromBech32(v)
			_ = testApp.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pub1", "key1", process1Id)
		}

		// Initiate process 2 -- still pending
		err = testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		process2, _ := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		process2Id := process2.Id

		// Query AllProcesses -- should contain both
		q := utsskeeper.NewQuerier(testApp.UtssKeeper)
		resp, err := q.AllProcesses(sdk.WrapSDKContext(ctx), &utsstypes.QueryAllProcessesRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Count pending vs finalized
		pendingCount := 0
		finalizedCount := 0
		for _, p := range resp.Processes {
			if p.Status == utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING {
				pendingCount++
			} else if p.Status == utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS {
				finalizedCount++
			}
		}

		// Process 1 should be finalized (SUCCESS), process 2 should be pending
		require.GreaterOrEqual(t, finalizedCount, 1, "should have at least one finalized process")
		require.GreaterOrEqual(t, pendingCount, 1, "should have at least one pending process")

		// Verify specific processes
		p1, _, _ := testApp.UtssKeeper.GetTssKeyProcessByID(ctx, process1Id)
		require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS, p1.Status)

		p2, _, _ := testApp.UtssKeeper.GetTssKeyProcessByID(ctx, process2Id)
		require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING, p2.Status)
	})

	t.Run("AllProcesses pagination reverse returns descending order", func(t *testing.T) {
		testApp, ctx, validators := setupTssKeyProcessTest(t, 3)

		// Create multiple processes (initiate + finalize to free up CurrentTssProcess)
		var processIds []uint64
		for i := 0; i < 3; i++ {
			err := testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
			require.NoError(t, err)

			process, _ := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
			processIds = append(processIds, process.Id)

			// Finalize each process so we can create the next one
			pub := "pub-batch"
			key := "key-batch-" + string(rune('a'+i))

			for _, v := range validators {
				valAddr, _ := sdk.ValAddressFromBech32(v)
				_ = testApp.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key, process.Id)
			}
		}

		require.GreaterOrEqual(t, len(processIds), 3, "should have created at least 3 processes")

		// Query with reverse pagination
		q := utsskeeper.NewQuerier(testApp.UtssKeeper)
		resp, err := q.AllProcesses(sdk.WrapSDKContext(ctx), &utsstypes.QueryAllProcessesRequest{
			Pagination: &query.PageRequest{
				Reverse: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.GreaterOrEqual(t, len(resp.Processes), 3, "should return at least 3 processes")

		// Verify descending order by process ID
		for i := 1; i < len(resp.Processes); i++ {
			require.GreaterOrEqual(t, resp.Processes[i-1].Id, resp.Processes[i].Id,
				"processes should be in descending order when reverse=true")
		}
	})

	t.Run("HasOngoingTss returns true only when process exists and not expired", func(t *testing.T) {
		testApp, ctx, _ := setupTssKeyProcessTest(t, 3)

		// No process -- HasOngoingTss should be false
		hasOngoing, err := testApp.UtssKeeper.HasOngoingTss(ctx)
		require.NoError(t, err)
		require.False(t, hasOngoing, "no ongoing TSS before initiation")

		// Initiate a process
		err = testApp.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		// Now it should be true
		hasOngoing, err = testApp.UtssKeeper.HasOngoingTss(ctx)
		require.NoError(t, err)
		require.True(t, hasOngoing, "should have ongoing TSS after initiation")

		// Advance past expiry height
		process, _ := testApp.UtssKeeper.CurrentTssProcess.Get(ctx)
		ctx = ctx.WithBlockHeight(process.ExpiryHeight + 1)

		hasOngoing, err = testApp.UtssKeeper.HasOngoingTss(ctx)
		require.NoError(t, err)
		require.False(t, hasOngoing, "should not have ongoing TSS past expiry height")
	})
}
