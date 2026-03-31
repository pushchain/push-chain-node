package integrationtest

import (
	"fmt"
	"strconv"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"

	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const testChain = "eip155:11155111"

// setupFundMigrationTest initializes app with validators, a finalized keygen key, and a chain config.
// Returns app, ctx, validator addresses, and the finalized key ID.
func setupFundMigrationTest(t *testing.T, numVals int, outboundEnabled bool) (*app.ChainApp, sdk.Context, []string, string) {
	t.Helper()

	app, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	// Register universal validators
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		pubkey := "pubkey-tss-" + coreValAddr
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		err := app.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, network)
		require.NoError(t, err)

		finalizeAutoInitiatedTssProcess(t, app, ctx, pubkey, "Key-id-tss-"+strconv.Itoa(i))
		universalVals[i] = coreValAddr
	}

	// Now do a keygen to get a proper key
	err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	keygenKeyId := "keygen-key-1"
	keygenPubkey := "keygen-pubkey-1"

	// Vote to finalize the keygen
	process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	for _, val := range universalVals {
		valAddr, err := sdk.ValAddressFromBech32(val)
		require.NoError(t, err)
		err = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, keygenPubkey, keygenKeyId, process.Id)
		require.NoError(t, err)
	}

	// Verify key is finalized
	currentKey, err := app.UtssKeeper.CurrentTssKey.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, keygenKeyId, currentKey.KeyId)

	// Now do ANOTHER keygen so the first becomes the "old" key
	err = app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_KEYGEN)
	require.NoError(t, err)

	newKeyId := "keygen-key-2"
	newPubkey := "keygen-pubkey-2"

	process, err = app.UtssKeeper.CurrentTssProcess.Get(ctx)
	require.NoError(t, err)

	for _, val := range universalVals {
		valAddr, err := sdk.ValAddressFromBech32(val)
		require.NoError(t, err)
		err = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, newPubkey, newKeyId, process.Id)
		require.NoError(t, err)
	}

	// Verify new key is current, old key is in history
	currentKey, err = app.UtssKeeper.CurrentTssKey.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, newKeyId, currentKey.KeyId)

	oldKey, err := app.UtssKeeper.TssKeyHistory.Get(ctx, keygenKeyId)
	require.NoError(t, err)
	require.Equal(t, keygenKeyId, oldKey.KeyId)

	// Set up chain config
	chainConfig := uregistrytypes.ChainConfig{
		Chain:          testChain,
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{{
			Name:             "addFunds",
			Identifier:       "",
			EventIdentifier:  "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
			ConfirmationType: 5,
		}},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: outboundEnabled,
		},
	}

	err = app.UregistryKeeper.ChainConfigs.Set(ctx, testChain, chainConfig)
	require.NoError(t, err)

	return app, ctx, universalVals, keygenKeyId
}

func TestInitiateFundMigration(t *testing.T) {
	t.Run("Successfully initiates fund migration", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.NoError(t, err)
		require.Equal(t, uint64(0), migrationId)

		// Verify migration is stored
		migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING, migration.Status)
		require.Equal(t, oldKeyId, migration.OldKeyId)
		require.Equal(t, testChain, migration.Chain)
		require.Equal(t, uint64(21000), migration.GasLimit)
		require.NotEmpty(t, migration.GasPrice)

		// Verify pending index
		_, err = app.UtssKeeper.PendingMigrations.Get(ctx, migrationId)
		require.NoError(t, err)

		// Verify event emitted
		events := ctx.EventManager().Events()
		var found bool
		for _, ev := range events {
			if ev.Type == utsstypes.EventTypeFundMigrationInitiated {
				found = true
				break
			}
		}
		require.True(t, found, "FundMigrationInitiatedEvent should be emitted")
	})

	t.Run("Fails if old key not found", func(t *testing.T) {
		app, ctx, _, _ := setupFundMigrationTest(t, 3, false)

		_, err := app.UtssKeeper.InitiateFundMigration(ctx, "nonexistent-key", testChain)
		require.ErrorContains(t, err, "not found in TssKeyHistory")
	})

	t.Run("Fails if old key was not from keygen", func(t *testing.T) {
		app, ctx, universalVals, _ := setupFundMigrationTest(t, 3, false)

		// Initiate a refresh process and finalize it
		err := app.UtssKeeper.InitiateTssKeyProcess(ctx, utsstypes.TssProcessType_TSS_PROCESS_REFRESH)
		require.NoError(t, err)

		refreshKeyId := "refresh-key-1"
		refreshPubkey := "refresh-pubkey-1"

		process, err := app.UtssKeeper.CurrentTssProcess.Get(ctx)
		require.NoError(t, err)

		for _, val := range universalVals {
			valAddr, _ := sdk.ValAddressFromBech32(val)
			_ = app.UtssKeeper.VoteTssKeyProcess(ctx, valAddr, refreshPubkey, refreshKeyId, process.Id)
		}

		// Try to migrate from the refresh key — should fail
		_, err = app.UtssKeeper.InitiateFundMigration(ctx, refreshKeyId, testChain)
		require.ErrorContains(t, err, "not keygen")
	})

	t.Run("Fails if old key is the current key", func(t *testing.T) {
		app, ctx, _, _ := setupFundMigrationTest(t, 3, false)

		currentKey, err := app.UtssKeeper.CurrentTssKey.Get(ctx)
		require.NoError(t, err)

		_, err = app.UtssKeeper.InitiateFundMigration(ctx, currentKey.KeyId, testChain)
		require.ErrorContains(t, err, "current active key")
	})

	t.Run("Fails if outbound is still enabled", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, true) // outbound enabled

		_, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.ErrorContains(t, err, "outbound is still enabled")
	})

	t.Run("Fails if duplicate pending migration exists", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

		_, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.NoError(t, err)

		// Try again — should fail
		_, err = app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.ErrorContains(t, err, "pending migration already exists")
	})
}

func TestVoteFundMigration(t *testing.T) {
	t.Run("Full migration flow: initiate → vote → complete", func(t *testing.T) {
		app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		// Initiate migration
		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.NoError(t, err)

		txHash := "0xdeadbeef1234567890"

		// Vote with all validators (2/3 quorum needed, so 3 votes for 3 validators)
		for i, val := range universalVals {
			valAddr, err := sdk.ValAddressFromBech32(val)
			require.NoError(t, err)

			err = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, txHash, true)
			require.NoError(t, err)

			// Check if finalized after enough votes
			migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
			require.NoError(t, err)

			if i < len(universalVals)-1 {
				// Not yet finalized
				require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING, migration.Status)
			}
		}

		// Verify migration is now completed
		migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_COMPLETED, migration.Status)
		require.Equal(t, txHash, migration.TxHash)

		// Verify removed from pending
		_, err = app.UtssKeeper.PendingMigrations.Get(ctx, migrationId)
		require.Error(t, err) // should not be found

		// Verify completion event
		events := ctx.EventManager().Events()
		var found bool
		for _, ev := range events {
			if ev.Type == utsstypes.EventTypeFundMigrationCompleted {
				found = true
				break
			}
		}
		require.True(t, found, "FundMigrationCompletedEvent should be emitted")
	})

	t.Run("Migration failure flow", func(t *testing.T) {
		app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.NoError(t, err)

		txHash := "0xfailedtx"

		// Vote failure with all validators
		for _, val := range universalVals {
			valAddr, _ := sdk.ValAddressFromBech32(val)
			err = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, txHash, false)
			require.NoError(t, err)
		}

		migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_FAILED, migration.Status)
	})

	t.Run("Fails to vote on non-existent migration", func(t *testing.T) {
		app, ctx, universalVals, _ := setupFundMigrationTest(t, 3, false)

		valAddr, _ := sdk.ValAddressFromBech32(universalVals[0])
		err := app.UtssKeeper.VoteFundMigration(ctx, valAddr, 999, "0xtx", true)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("Fails to vote on already finalized migration", func(t *testing.T) {
		app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.NoError(t, err)

		// Finalize it first
		for _, val := range universalVals {
			valAddr, _ := sdk.ValAddressFromBech32(val)
			_ = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, "0xtx", true)
		}

		// Try to vote again
		valAddr, _ := sdk.ValAddressFromBech32(universalVals[0])
		err = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, "0xtx2", true)
		require.ErrorContains(t, err, "already finalized")
	})
}

func TestFundMigrationQueries(t *testing.T) {
	t.Run("GetFundMigration returns correct migration", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.NoError(t, err)

		migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, oldKeyId, migration.OldKeyId)
		require.Equal(t, testChain, migration.Chain)
	})

	t.Run("PendingMigrations tracks correctly", func(t *testing.T) {
		app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain)
		require.NoError(t, err)

		// Should be in pending
		var pendingCount int
		_ = app.UtssKeeper.PendingMigrations.Walk(ctx, nil, func(k uint64, v uint64) (bool, error) {
			pendingCount++
			return false, nil
		})
		require.Equal(t, 1, pendingCount)

		// Finalize it
		for _, val := range universalVals {
			valAddr, _ := sdk.ValAddressFromBech32(val)
			_ = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, "0xtx", true)
		}

		// Should be removed from pending
		pendingCount = 0
		_ = app.UtssKeeper.PendingMigrations.Walk(ctx, nil, func(k uint64, v uint64) (bool, error) {
			pendingCount++
			return false, nil
		})
		require.Equal(t, 0, pendingCount)
	})
}
