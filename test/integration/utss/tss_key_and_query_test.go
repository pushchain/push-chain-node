package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/utss/keeper"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	utils "github.com/pushchain/push-chain-node/test/utils"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// buildValidTssKey returns a TssKey that passes ValidateBasic at the given ctx
// block height.
func buildValidTssKey(ctx sdk.Context, keyID, pubkey string, processID uint64, participants []string) utsstypes.TssKey {
	bh := ctx.BlockHeight()
	return utsstypes.TssKey{
		KeyId:                keyID,
		TssPubkey:            pubkey,
		Participants:         participants,
		KeygenBlockHeight:    bh,
		FinalizedBlockHeight: bh + 1,
		ProcessId:            processID,
	}
}

// ---------------------------------------------------------------------------
// tss_key.go — SetCurrentTssKey / GetCurrentTssKey / GetTssKeyByID
// ---------------------------------------------------------------------------

// TestSetAndGetCurrentTssKey verifies that a stored key can be retrieved.
func TestSetAndGetCurrentTssKey(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	tssKey := buildValidTssKey(ctx, "key-001", "pubkey-001", 1, validators)

	err := app.UtssKeeper.SetCurrentTssKey(ctx, tssKey)
	require.NoError(t, err)

	got, found, err := app.UtssKeeper.GetCurrentTssKey(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, tssKey.KeyId, got.KeyId)
	require.Equal(t, tssKey.TssPubkey, got.TssPubkey)
	require.Equal(t, tssKey.ProcessId, got.ProcessId)
	require.Equal(t, tssKey.Participants, got.Participants)
}

// TestGetCurrentTssKeyNotFound verifies that GetCurrentTssKey returns
// (empty, false, nil) when no key has been stored.
// Note: setupTssKeyProcessTest may auto-initiate a TSS process via hooks,
// but no key is finalized until votes reach quorum, so CurrentTssKey should still be empty.
func TestGetCurrentTssKeyNotFound(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	// Clear any key that might have been set during setup
	_ = app.UtssKeeper.CurrentTssKey.Remove(ctx)

	key, found, err := app.UtssKeeper.GetCurrentTssKey(ctx)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, utsstypes.TssKey{}, key)
}

// TestSetCurrentTssKeyInvalid verifies that an invalid key (empty pubkey) is
// rejected by ValidateBasic inside SetCurrentTssKey.
func TestSetCurrentTssKeyInvalid(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 2)

	bad := utsstypes.TssKey{
		KeyId:        "k1",
		TssPubkey:    "", // invalid — empty
		Participants: validators,
		ProcessId:    1,
	}

	err := app.UtssKeeper.SetCurrentTssKey(ctx, bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid tss key")
}

// TestSetCurrentTssKeyAlsoWritesHistory verifies that SetCurrentTssKey stores
// the key in TssKeyHistory so it is retrievable by GetTssKeyByID.
func TestSetCurrentTssKeyAlsoWritesHistory(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	tssKey := buildValidTssKey(ctx, "hist-key-001", "hist-pub-001", 1, validators)

	err := app.UtssKeeper.SetCurrentTssKey(ctx, tssKey)
	require.NoError(t, err)

	hist, found, err := app.UtssKeeper.GetTssKeyByID(ctx, tssKey.KeyId)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, tssKey.KeyId, hist.KeyId)
}

// TestOverwriteCurrentTssKey verifies that calling SetCurrentTssKey a second
// time replaces the current key.
func TestOverwriteCurrentTssKey(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	first := buildValidTssKey(ctx, "key-first", "pub-first", 1, validators)
	second := buildValidTssKey(ctx, "key-second", "pub-second", 2, validators)

	require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, first))
	require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, second))

	got, found, err := app.UtssKeeper.GetCurrentTssKey(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, second.KeyId, got.KeyId)
}

// ---------------------------------------------------------------------------
// tss_key_process.go — GetCurrentTssParticipants
// ---------------------------------------------------------------------------

// TestGetCurrentTssParticipantsNoProcess verifies an empty slice is returned
// when no current process exists.
func TestGetCurrentTssParticipantsNoProcess(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)
	// After setup all auto-initiated processes have been finalized, so there
	// should be no current process.
	participants, err := app.UtssKeeper.GetCurrentTssParticipants(ctx)
	require.NoError(t, err)
	require.Empty(t, participants)
}

// TestGetCurrentTssParticipantsWithActiveProcess verifies the participants of
// a running (non-expired) process are returned correctly.
func TestGetCurrentTssParticipantsWithActiveProcess(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 4)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	// Block height is still within the process window.
	participants, err := app.UtssKeeper.GetCurrentTssParticipants(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, participants)
	require.Equal(t, process.Participants, participants)
}

// TestGetCurrentTssParticipantsExpiredProcess verifies an empty slice is
// returned once the current process has passed its expiry height.
func TestGetCurrentTssParticipantsExpiredProcess(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	// Advance past expiry height.
	ctx = ctx.WithBlockHeight(process.ExpiryHeight + 1)

	participants, err := app.UtssKeeper.GetCurrentTssParticipants(ctx)
	require.NoError(t, err)
	require.Empty(t, participants)
}

// ---------------------------------------------------------------------------
// tss_key_process.go — GetTssKeyProcessByID
// ---------------------------------------------------------------------------

// TestGetTssKeyProcessByIDValid verifies a process can be retrieved by ID
// after initiation.
func TestGetTssKeyProcessByIDValid(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	current, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	process, found, err := app.UtssKeeper.GetTssKeyProcessByID(ctx, current.Id)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, current.Id, process.Id)
	require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING, process.Status)
}

// TestGetTssKeyProcessByIDNotFound verifies (empty, false, nil) is returned
// for a non-existent process ID.
func TestGetTssKeyProcessByIDNotFound(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	process, found, err := app.UtssKeeper.GetTssKeyProcessByID(ctx, 99999)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, utsstypes.TssKeyProcess{}, process)
}

// TestGetTssKeyProcessByIDAfterFinalization verifies the process status is
// updated to SUCCESS in history after quorum is reached.
func TestGetTssKeyProcessByIDAfterFinalization(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	current, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)
	processID := current.Id

	for _, v := range validators {
		valAddr, err := sdk.ValAddressFromBech32(v)
		require.NoError(t, err)
		_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pub-final", "key-final", processID)
	}

	hist, found, err := app.UtssKeeper.GetTssKeyProcessByID(ctx, processID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS, hist.Status)
}

// ---------------------------------------------------------------------------
// msg_update_params.go — UpdateParams
// ---------------------------------------------------------------------------

// TestUpdateParams verifies that Params can be updated and re-read.
func TestUpdateParams(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	newParams := utsstypes.Params{Admin: "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"}
	err := app.UtssKeeper.UpdateParams(ctx, newParams)
	require.NoError(t, err)

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.Params(ctx, &utsstypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Params)
	require.Equal(t, newParams.Admin, resp.Params.Admin)
}

// ---------------------------------------------------------------------------
// query_server.go — Params
// ---------------------------------------------------------------------------

// TestQueryParams verifies that the Params query returns the module params.
func TestQueryParams(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	// Initialize params so they exist in state. DefaultParams() now returns an
	// empty Admin (production operators must set it explicitly in genesis), so
	// the query test supplies its own.
	const testAdmin = "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"
	require.NoError(t, app.UtssKeeper.Params.Set(ctx, utsstypes.Params{Admin: testAdmin}))

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.Params(ctx, &utsstypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
	require.Equal(t, testAdmin, resp.Params.Admin)
}

// ---------------------------------------------------------------------------
// query_server.go — CurrentProcess
// ---------------------------------------------------------------------------

// TestQueryCurrentProcessExists verifies a running process is returned.
func TestQueryCurrentProcessExists(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.CurrentProcess(ctx, &utsstypes.QueryCurrentProcessRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Process)
	require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING, resp.Process.Status)
	require.Equal(t, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN, resp.Process.ProcessType)
}

// TestQueryCurrentProcessNotFound verifies an error is returned when no
// current process exists.
func TestQueryCurrentProcessNotFound(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)
	// No process initiated after setup finalized all auto-initiated processes.
	querier := keeper.NewQuerier(app.UtssKeeper)
	_, err := querier.CurrentProcess(ctx, &utsstypes.QueryCurrentProcessRequest{})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// query_server.go — ProcessById
// ---------------------------------------------------------------------------

// TestQueryProcessByIdValid verifies a process in history is returned.
func TestQueryProcessByIdValid(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	current, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.ProcessById(ctx, &utsstypes.QueryProcessByIdRequest{Id: current.Id})
	require.NoError(t, err)
	require.NotNil(t, resp.Process)
	require.Equal(t, current.Id, resp.Process.Id)
}

// TestQueryProcessByIdNotFound verifies an error is returned for an unknown ID.
func TestQueryProcessByIdNotFound(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	querier := keeper.NewQuerier(app.UtssKeeper)
	_, err := querier.ProcessById(ctx, &utsstypes.QueryProcessByIdRequest{Id: 88888})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// query_server.go — CurrentKey
// ---------------------------------------------------------------------------

// TestQueryCurrentKey verifies a stored key is returned by the query.
func TestQueryCurrentKey(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	tssKey := buildValidTssKey(ctx, "query-key-001", "query-pub-001", 1, validators)
	require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, tssKey))

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.CurrentKey(ctx, &utsstypes.QueryCurrentKeyRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Key)
	require.Equal(t, tssKey.KeyId, resp.Key.KeyId)
	require.Equal(t, tssKey.TssPubkey, resp.Key.TssPubkey)
}

// TestQueryCurrentKeyNotFound verifies an error is returned when no key has
// been stored.
func TestQueryCurrentKeyNotFound(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	// Clear any key that may have been set during setup hooks
	_ = app.UtssKeeper.CurrentTssKey.Remove(ctx)

	querier := keeper.NewQuerier(app.UtssKeeper)
	_, err := querier.CurrentKey(ctx, &utsstypes.QueryCurrentKeyRequest{})
	require.Error(t, err)
}

// TestQueryCurrentKeyAfterVoteFinalization verifies that after a TSS process
// is finalized via votes the current key is available through the query.
func TestQueryCurrentKeyAfterVoteFinalization(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	pub := "vote-pub"
	keyID := "vote-key"

	for _, v := range validators {
		valAddr, err := sdk.ValAddressFromBech32(v)
		require.NoError(t, err)
		_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, keyID, process.Id)
	}

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.CurrentKey(ctx, &utsstypes.QueryCurrentKeyRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Key)
	require.Equal(t, keyID, resp.Key.KeyId)
	require.Equal(t, pub, resp.Key.TssPubkey)
}

// ---------------------------------------------------------------------------
// query_server.go — KeyById
// ---------------------------------------------------------------------------

// TestQueryKeyByIdValid verifies a key stored in history is returned.
func TestQueryKeyByIdValid(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	tssKey := buildValidTssKey(ctx, "hist-qkey-001", "hist-qpub-001", 1, validators)
	require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, tssKey))

	querier := keeper.NewQuerier(app.UtssKeeper)
	resp, err := querier.KeyById(ctx, &utsstypes.QueryKeyByIdRequest{KeyId: tssKey.KeyId})
	require.NoError(t, err)
	require.NotNil(t, resp.Key)
	require.Equal(t, tssKey.KeyId, resp.Key.KeyId)
}

// TestQueryKeyByIdNotFound verifies an error is returned for an unknown key ID.
func TestQueryKeyByIdNotFound(t *testing.T) {
	app, ctx, _ := setupTssKeyProcessTest(t, 2)

	querier := keeper.NewQuerier(app.UtssKeeper)
	_, err := querier.KeyById(ctx, &utsstypes.QueryKeyByIdRequest{KeyId: "does-not-exist"})
	require.Error(t, err)
}

// TestQueryKeyByIdMultipleKeys verifies that multiple stored keys can be
// individually retrieved by their respective IDs.
func TestQueryKeyByIdMultipleKeys(t *testing.T) {
	app, ctx, validators := setupTssKeyProcessTest(t, 3)

	key1 := buildValidTssKey(ctx, "multi-key-1", "multi-pub-1", 1, validators)
	key2 := buildValidTssKey(ctx, "multi-key-2", "multi-pub-2", 2, validators)

	require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, key1))
	require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, key2))

	querier := keeper.NewQuerier(app.UtssKeeper)

	resp1, err := querier.KeyById(ctx, &utsstypes.QueryKeyByIdRequest{KeyId: key1.KeyId})
	require.NoError(t, err)
	require.Equal(t, key1.KeyId, resp1.Key.KeyId)

	resp2, err := querier.KeyById(ctx, &utsstypes.QueryKeyByIdRequest{KeyId: key2.KeyId})
	require.NoError(t, err)
	require.Equal(t, key2.KeyId, resp2.Key.KeyId)
}

// ---------------------------------------------------------------------------
// query_server.go — AllKeys
// ---------------------------------------------------------------------------

// TestQueryAllKeys verifies that all stored keys are returned by AllKeys.
func TestQueryAllKeys(t *testing.T) {
	t.Run("returns all stored keys including newly added ones", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		key1 := buildValidTssKey(ctx, "allkeys-1", "allkeys-pub-1", 100, validators)
		key2 := buildValidTssKey(ctx, "allkeys-2", "allkeys-pub-2", 101, validators)

		require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, key1))
		require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, key2))

		querier := keeper.NewQuerier(app.UtssKeeper)
		resp, err := querier.AllKeys(ctx, &utsstypes.QueryAllKeysRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// setupTssKeyProcessTest may store keys during setup; ensure our two are present.
		keyIDs := make(map[string]struct{}, len(resp.Keys))
		for _, k := range resp.Keys {
			keyIDs[k.KeyId] = struct{}{}
		}
		require.Contains(t, keyIDs, key1.KeyId)
		require.Contains(t, keyIDs, key2.KeyId)
	})

	t.Run("returns empty list when no keys stored", func(t *testing.T) {
		// Use a raw app with no universal validators registered so no TSS
		// process is auto-initiated and no keys are written to history.
		freshApp, freshCtx, _, _ := utils.SetAppWithMultipleValidators(t, 2)

		querier := keeper.NewQuerier(freshApp.UtssKeeper)
		resp, err := querier.AllKeys(freshCtx, &utsstypes.QueryAllKeysRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Keys)
	})

	t.Run("count matches number of distinct keys stored", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		key1 := buildValidTssKey(ctx, "ck-1", "ck-pub-1", 10, validators)
		key2 := buildValidTssKey(ctx, "ck-2", "ck-pub-2", 11, validators)
		key3 := buildValidTssKey(ctx, "ck-3", "ck-pub-3", 12, validators)

		require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, key1))
		require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, key2))
		require.NoError(t, app.UtssKeeper.SetCurrentTssKey(ctx, key3))

		querier := keeper.NewQuerier(app.UtssKeeper)
		resp, err := querier.AllKeys(ctx, &utsstypes.QueryAllKeysRequest{})
		require.NoError(t, err)
		// setupTssKeyProcessTest also stores keys during setup, so there are
		// at least 3 in addition to any auto-created ones.
		require.GreaterOrEqual(t, len(resp.Keys), 3)
	})
}

// ---------------------------------------------------------------------------
// query_server.go — GetPendingTssEvent
// ---------------------------------------------------------------------------

// TestQueryGetPendingTssEvent verifies that after initiating a TSS process the
// pending event can be retrieved by the process ID.
func TestQueryGetPendingTssEvent(t *testing.T) {
	t.Run("returns pending event for an active process", func(t *testing.T) {
		app, ctx, _ := setupTssKeyProcessTest(t, 4)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
		require.NoError(t, err)

		querier := keeper.NewQuerier(app.UtssKeeper)
		resp, err := querier.GetPendingTssEvent(ctx, &utsstypes.QueryGetPendingTssEventRequest{
			ProcessId: process.Id,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.Event)
		require.Equal(t, process.Id, resp.Event.ProcessId)
		require.Equal(t, utsstypes.TssEventStatus_TSS_EVENT_ACTIVE, resp.Event.Status)
	})

	t.Run("returns error for unknown process ID", func(t *testing.T) {
		app, ctx, _ := setupTssKeyProcessTest(t, 2)

		querier := keeper.NewQuerier(app.UtssKeeper)
		_, err := querier.GetPendingTssEvent(ctx, &utsstypes.QueryGetPendingTssEventRequest{
			ProcessId: 99999,
		})
		require.Error(t, err)
	})

	t.Run("pending event is removed after process finalization", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
		require.NoError(t, err)
		processID := process.Id

		// Vote until quorum to finalize the process
		for _, v := range validators {
			valAddr, err := sdk.ValAddressFromBech32(v)
			require.NoError(t, err)
			_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "final-pub", "final-key", processID)
		}

		querier := keeper.NewQuerier(app.UtssKeeper)
		_, err = querier.GetPendingTssEvent(ctx, &utsstypes.QueryGetPendingTssEventRequest{
			ProcessId: processID,
		})
		require.Error(t, err, "pending event should be removed after finalization")
	})
}
