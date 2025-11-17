package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// Integration tests for VoteTssKeyProcess
func TestVoteTssKeyProcess(t *testing.T) {

	//-----------------------------------------------------------
	t.Run("Allows vote but does not finalize with insufficient votes", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		// first vote only (quorum = 2)
		v := validators[0]
		valAddr, _ := sdk.ValAddressFromBech32(v)

		pub := "pub-k1"
		key := "key-k2"

		err = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key)
		require.NoError(t, err)

		// should not finalize yet
		tssKey, err := app.UtssKeeper.CurrentTssKey.Get(ctx)
		if err == nil {
			require.NotEqual(t, pub, tssKey.TssPubkey)
			require.NotEqual(t, key, tssKey.KeyId)
		}
	})

	//-----------------------------------------------------------
	t.Run("Successfully finalizes after quorum reached", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		pub := "pub-final"
		key := "key-final"

		// vote from all 3 → quorum = 2 reached → finalized
		for _, v := range validators {
			valAddr, _ := sdk.ValAddressFromBech32(v)
			err := app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key)
			require.NoError(t, err)
		}

		tssKey, err := app.UtssKeeper.CurrentTssKey.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, key, tssKey.KeyId)
		require.Equal(t, pub, tssKey.TssPubkey)
	})

	//-----------------------------------------------------------
	t.Run("Fails when no active process exists", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 2)

		// clear active process
		app.UtssKeeper.CurrentTssProcess.Remove(ctx)

		valAddr, _ := sdk.ValAddressFromBech32(validators[0])

		err := app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pk", "key")
		require.ErrorContains(t, err, "no active TSS process")
	})

	//-----------------------------------------------------------
	t.Run("Fails when keyId already exists", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		pub := "dupPub"
		key := "dupKey"

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		// finalize once
		for _, v := range validators {
			valAddr, _ := sdk.ValAddressFromBech32(v)
			_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key)
		}

		err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		// vote again with SAME keyId
		valAddr, _ := sdk.ValAddressFromBech32(validators[0])
		err = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key)
		require.ErrorContains(t, err, "already exists")
	})

	//-----------------------------------------------------------
	t.Run("Fails when process expired", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 2)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		process, _ := app.UtssKeeper.CurrentTssProcess.Get(ctx)

		// set block height to AFTER expiry
		ctx = ctx.WithBlockHeight(process.ExpiryHeight + 10)

		valAddr, _ := sdk.ValAddressFromBech32(validators[0])
		err = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, "pub", "key")
		require.ErrorContains(t, err, "expired")
	})

	//-----------------------------------------------------------
	t.Run("Emits correct TSS_KEY_FINALIZED event", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		pub := "eventPub"
		key := "eventKey"

		for _, v := range validators {
			valAddr, _ := sdk.ValAddressFromBech32(v)
			_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pub, key)
		}

		events := ctx.EventManager().Events()
		require.NotEmpty(t, events)

		// check LAST event
		last := events[len(events)-1]
		require.Equal(t, utsstypes.EventTypeTssKeyFinalized, last.Type)

		attrs := last.Attributes
		require.Equal(t, key, attrs[1].Value)
		require.Equal(t, pub, attrs[2].Value)
	})

	//-----------------------------------------------------------
	t.Run("Updates validator lifecycle from pending->active", func(t *testing.T) {
		app, ctx, validators := setupTssKeyProcessTest(t, 3)

		// set validator[0] to pending_join
		v1 := validators[0]
		valAddr1, _ := sdk.ValAddressFromBech32(v1)

		app.UvalidatorKeeper.UpdateValidatorStatus(ctx,
			valAddr1,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN,
		)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		pubkey := "ls_pub"
		keyId := "ls_key"

		// vote from all validators → finalizes
		for _, v := range validators {
			valAddr, _ := sdk.ValAddressFromBech32(v)
			err = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pubkey, keyId)
			require.NoError(t, err)
		}

		uv, found, err := app.UvalidatorKeeper.GetUniversalValidator(ctx, valAddr1)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t,
			uvalidatortypes.UVStatus_UV_STATUS_ACTIVE,
			uv.LifecycleInfo.CurrentStatus,
		)
	})
}
