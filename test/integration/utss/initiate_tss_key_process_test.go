package integrationtest

import (
	"fmt"
	"strconv"
	"testing"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"

	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// setupTssKeyProcessTest initializes app, context, and validators
func setupTssKeyProcessTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string) {
	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	// register them as universal validators (eligible)
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		pubkey := "pubkey-tss-" + coreValAddr
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, network)
		require.NoError(t, err)

		// Finalize the auto-initiated TSS process BEFORE next validator is added
		finalizeAutoInitiatedTssProcess(t, app, ctx, pubkey, "Key-id-tss-"+strconv.Itoa(i))
		universalVals[i] = coreValAddr
	}

	return app, ctx, universalVals
}

func finalizeAutoInitiatedTssProcess(t *testing.T, app *app.ChainApp, ctx sdk.Context, pubKey, keyId string) {
	// Step 1: check if a process exists
	_, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	if err != nil {
		return // nothing to finalize
	}

	// Step 2: get current eligible voters
	voters, err := app.UvalidatorKeeper.GetEligibleVoters(ctx)
	require.NoError(t, err)

	// Step 3: cast votes until process finalizes
	for _, uv := range voters {
		coreVal := uv.IdentifyInfo.CoreValidatorAddress
		valAddr, err := sdk.ValAddressFromBech32(coreVal)
		require.NoError(t, err)

		// This triggers your normal Vote flow and internally finalizes when quorum reached
		err = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, pubKey, keyId)
		require.NoError(t, err)

		// Step 4: Check if finalized now
		p, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
		if err != nil || p.Status == utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_SUCCESS {
			return
		}
	}
}

func TestInitiateTssKeyProcess(t *testing.T) {
	t.Run("Successfully initiates new keygen process", func(t *testing.T) {
		app, ctx, _ := setupTssKeyProcessTest(t, 4)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		current, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
		fmt.Println(current)
		require.NoError(t, err)
		require.Equal(t, utsstypes.TssKeyProcessStatus_TSS_KEY_PROCESS_PENDING, current.Status)
		require.Equal(t, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN, current.ProcessType)
		require.NotZero(t, current.Id)
		require.NotEmpty(t, current.Participants)
	})

	t.Run("Fails when active process already exists", func(t *testing.T) {
		app, ctx, _ := setupTssKeyProcessTest(t, 3)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		// simulate that process still active (same block height)
		err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_REFRESH)
		require.ErrorContains(t, err, "active TSS process already exists")
	})

	t.Run("Allows new process after expiry height", func(t *testing.T) {
		app, ctx, _ := setupTssKeyProcessTest(t, 2)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)
		current, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
		fmt.Println(current)

		// move block height beyond expiry
		ctx = ctx.WithBlockHeight(int64(utsstypes.DefaultTssProcessExpiryAfterBlocks) + 100)

		err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_REFRESH)
		require.NoError(t, err)

		current, err = app.UtssKeeper.CurrentTssProcess.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, utsstypes.TssProcessType_TSS_PROCESS_REFRESH, current.ProcessType)
	})

	t.Run("Fails if eligible validators cannot be fetched", func(t *testing.T) {
		app, ctx, _ := setupTssKeyProcessTest(t, 1)

		// corrupt the uvalidator keeper mock or clear state
		app.UvalidatorKeeper.UniversalValidatorSet.Clear(ctx, collections.Ranger[sdk.ValAddress](nil))

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.ErrorContains(t, err, "invalid tss process: participants list cannot be empty")
	})

	t.Run("Emits correct event on initiation", func(t *testing.T) {
		universalValsNum := 3
		app, ctx, _ := setupTssKeyProcessTest(t, universalValsNum)

		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
		require.NoError(t, err)

		events := ctx.EventManager().Events()
		require.NotEmpty(t, events)

		last := events[len(events)-1]
		require.Equal(t, utsstypes.EventTypeTssProcessInitiated, last.Type)

		pid := last.Attributes[0].Value
		require.Equal(t, strconv.Itoa(universalValsNum), pid)

	})
}
