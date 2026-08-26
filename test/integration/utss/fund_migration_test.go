package integrationtest

import (
	"bytes"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"

	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utsskeeper "github.com/pushchain/push-chain-node/x/utss/keeper"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

const testChain = "eip155:11155111"

// universalCoreSetupABI exposes the admin methods needed to configure
// per-chain mappings during test setup. These are intentionally kept out of
// the production ABI (x/uexecutor/types/abi.go) — Go-side keeper code never
// calls them; only tests do.
const universalCoreSetupABI = `[
    {
      "type": "function",
      "name": "grantRole",
      "inputs": [
        { "name": "role",    "type": "bytes32", "internalType": "bytes32" },
        { "name": "account", "type": "address", "internalType": "address" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "setL1GasFeeByChain",
      "inputs": [
        { "name": "chainNamespace", "type": "string",  "internalType": "string" },
        { "name": "l1GasFee",       "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "setTssFundMigrationGasLimitByChain",
      "inputs": [
        { "name": "chainNamespace", "type": "string",  "internalType": "string" },
        { "name": "gasLimit",       "type": "uint256", "internalType": "uint256" }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    }
]`

// testBalance is the native balance the admin reports observing on the old TSS
// address. Comfortably above gas_price*21000 + 150 so the derived
// transfer_amount is positive for any oracle gas price the harness produces.
const testBalance = "1000000000000000000" // 1e18 wei

// seedFundMigrationChainValues grants MANAGER_ROLE to the admin and seeds the
// per-chain tss-fund-migration gas limit and L1 gas fee on UniversalCore.
// InitiateFundMigration rejects a zero gas limit, so without this seeding the
// keeper read returns 0 and the migration fails validation.
func seedFundMigrationChainValues(
	t *testing.T,
	chainApp *app.ChainApp,
	ctx sdk.Context,
	admin common.Address,
	chain string,
	gasLimit, l1GasFee *big.Int,
) {
	t.Helper()

	handlerAddr := utils.GetDefaultAddresses().HandlerAddr
	setupABI, err := abi.JSON(strings.NewReader(universalCoreSetupABI))
	require.NoError(t, err)

	managerRole := crypto.Keccak256Hash([]byte("MANAGER_ROLE"))
	var roleArg [32]byte
	copy(roleArg[:], managerRole.Bytes())

	_, err = chainApp.EVMKeeper.CallEVM(ctx, setupABI, admin, handlerAddr, true, nil, "grantRole", roleArg, admin)
	require.NoError(t, err, "grant MANAGER_ROLE")

	_, err = chainApp.EVMKeeper.CallEVM(ctx, setupABI, admin, handlerAddr, true, nil, "setTssFundMigrationGasLimitByChain", chain, gasLimit)
	require.NoError(t, err, "seed tss fund migration gas limit")

	_, err = chainApp.EVMKeeper.CallEVM(ctx, setupABI, admin, handlerAddr, true, nil, "setL1GasFeeByChain", chain, l1GasFee)
	require.NoError(t, err, "seed l1 gas fee")
}

// setupFundMigrationTest initializes app with validators, a finalized keygen key, and a chain config.
// Returns app, ctx, validator addresses, and the finalized key ID.
func setupFundMigrationTest(t *testing.T, numVals int, outboundEnabled bool) (*app.ChainApp, sdk.Context, []string, string) {
	t.Helper()

	app, ctx, baseAccounts, validators := utils.SetAppWithMultipleValidators(t, numVals)

	admin := common.BytesToAddress(baseAccounts[0].GetAddress().Bytes())
	seedFundMigrationChainValues(t, app, ctx, admin, testChain, big.NewInt(21000), big.NewInt(150))

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

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)
		// Ids start at 1, not 0 — 0 is reserved for "unset" and is rejected by
		// MsgVoteFundMigration.ValidateBasic (F-2026-18789).
		require.Equal(t, uint64(1), migrationId)

		// Verify migration is stored
		migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING, migration.Status)
		require.Equal(t, oldKeyId, migration.OldKeyId)
		require.Equal(t, testChain, migration.Chain)
		// GasLimit and L1GasFee come from UniversalCore's per-chain mappings,
		// seeded by seedFundMigrationChainValues.
		require.Equal(t, uint64(21000), migration.GasLimit)
		require.Equal(t, "150", migration.L1GasFee)
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

	t.Run("Derives transfer_amount from the observed balance and pinned fees", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)

		migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)

		// transfer_amount must equal balance - (gas_price * gas_limit) - l1_gas_fee,
		// computed from the very fields recorded alongside it. Deriving it here
		// rather than accepting it from the admin is what makes the two consistent
		// by construction — the admin cannot know these fees, they are read from
		// UniversalCore inside the handler (F-2026-18142).
		gasPrice, ok := new(big.Int).SetString(migration.GasPrice, 10)
		require.True(t, ok)
		l1GasFee, ok := new(big.Int).SetString(migration.L1GasFee, 10)
		require.True(t, ok)
		balance, ok := new(big.Int).SetString(testBalance, 10)
		require.True(t, ok)

		want := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(migration.GasLimit))
		want.Add(want, l1GasFee)
		want.Sub(balance, want)

		require.Equal(t, want.String(), migration.TransferAmount)
		require.Positive(t, want.Sign(), "the fixture balance must exceed the fees or this proves nothing")

		// The pinned amount must also reach the universal validators, which read
		// it off the event rather than re-deriving it from a live balance.
		var attr string
		for _, ev := range ctx.EventManager().Events() {
			if ev.Type != utsstypes.EventTypeFundMigrationInitiated {
				continue
			}
			for _, a := range ev.Attributes {
				if a.Key == "transfer_amount" {
					attr = a.Value
				}
			}
		}
		require.Equal(t, migration.TransferAmount, attr,
			"transfer_amount must be emitted on the event")
	})

	t.Run("Fails when the balance cannot cover the migration fee", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

		// 1 wei cannot cover gas_price*21000 + 150. Rejecting at initiate time
		// beats creating a PENDING migration that can never be signed.
		_, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, "1")
		require.ErrorContains(t, err, "does not cover the migration fee")

		// Nothing may be left behind under any id.
		var stored int
		require.NoError(t, app.UtssKeeper.FundMigrations.Walk(ctx, nil, func(uint64, utsstypes.FundMigration) (bool, error) {
			stored++
			return false, nil
		}))
		require.Zero(t, stored, "a rejected migration must not be stored")
	})

	t.Run("Fails if old key not found", func(t *testing.T) {
		app, ctx, _, _ := setupFundMigrationTest(t, 3, false)

		_, err := app.UtssKeeper.InitiateFundMigration(ctx, "nonexistent-key", testChain, testBalance)
		require.ErrorContains(t, err, "not found in TssKeyHistory")
	})

	t.Run("Fails if old key is the current key", func(t *testing.T) {
		app, ctx, _, _ := setupFundMigrationTest(t, 3, false)

		currentKey, err := app.UtssKeeper.CurrentTssKey.Get(ctx)
		require.NoError(t, err)

		_, err = app.UtssKeeper.InitiateFundMigration(ctx, currentKey.KeyId, testChain, testBalance)
		require.ErrorContains(t, err, "current active key")
	})

	t.Run("Fails if outbound is still enabled", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, true) // outbound enabled

		_, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.ErrorContains(t, err, "outbound is still enabled")
	})

	t.Run("Fails if duplicate pending migration exists", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

		_, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)

		// Try again — should fail (same chain already has pending migration)
		_, err = app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.ErrorContains(t, err, "pending migration already exists for chain")
	})
}

func TestVoteFundMigration(t *testing.T) {
	t.Run("Full migration flow: initiate → vote → complete", func(t *testing.T) {
		app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		// Initiate migration
		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)

		txHash := "0xdeadbeef12345678deadbeef12345678deadbeef12345678deadbeef12345678"

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

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)

		txHash := ""

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
		err := app.UtssKeeper.VoteFundMigration(ctx, valAddr, 999, "0x1111111111111111111111111111111111111111111111111111111111111111", true)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("Fails to vote on already finalized migration", func(t *testing.T) {
		app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)

		// Finalize it first
		for _, val := range universalVals {
			valAddr, _ := sdk.ValAddressFromBech32(val)
			_ = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, "0x1111111111111111111111111111111111111111111111111111111111111111", true)
		}

		// Try to vote again
		valAddr, _ := sdk.ValAddressFromBech32(universalVals[0])
		err = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, "0x2222222222222222222222222222222222222222222222222222222222222222", true)
		require.ErrorContains(t, err, "already finalized")
	})
}

func TestFundMigrationQueries(t *testing.T) {
	t.Run("GetFundMigration returns correct migration", func(t *testing.T) {
		app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)

		migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, oldKeyId, migration.OldKeyId)
		require.Equal(t, testChain, migration.Chain)
	})

	t.Run("PendingMigrations tracks correctly", func(t *testing.T) {
		app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
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
			_ = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, "0x1111111111111111111111111111111111111111111111111111111111111111", true)
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

// TestVoteFundMigration_EquivalentHashEncodingsConverge is the F-2026-17041
// regression: three validators submit the SAME migration tx hash in three
// different encodings (EIP-55-style mixed case, lowercase, no 0x prefix).
// Canonicalization in VoteFundMigration must land all votes on ONE ballot,
// finalizing the migration — pre-fix each encoding produced its own ballot
// and quorum never formed.
func TestVoteFundMigration_EquivalentHashEncodingsConverge(t *testing.T) {
	app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

	migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
	require.NoError(t, err)

	canonical := "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd"
	encodings := []string{
		"0xB28F49668E7E76DC96D7AABE5B7F63FECFBD1C3574774C05E8204E749FD96FBD", // uppercase
		"0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd", // lowercase
		"b28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",   // no prefix
	}
	require.Len(t, universalVals, 3)

	for i, val := range universalVals {
		valAddr, err := sdk.ValAddressFromBech32(val)
		require.NoError(t, err)
		require.NoError(t, app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, encodings[i], true),
			"vote %d with encoding %q must be accepted", i, encodings[i])
	}

	// All three encodings converged on one ballot → quorum reached → COMPLETED.
	migration, err := app.UtssKeeper.FundMigrations.Get(ctx, migrationId)
	require.NoError(t, err)
	require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_COMPLETED, migration.Status,
		"equivalent encodings must aggregate on one ballot and finalize")
	require.Equal(t, canonical, migration.TxHash,
		"stored tx hash must be the canonical 0x-lowercase form")
}

// TestVoteFundMigration_MalformedHashRejected: strict per-namespace
// validation rejects garbage hashes for EVM chains instead of keying a
// ballot off them.
func TestVoteFundMigration_MalformedHashRejected(t *testing.T) {
	app, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

	migrationId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
	require.NoError(t, err)

	valAddr, _ := sdk.ValAddressFromBech32(universalVals[0])
	err = app.UtssKeeper.VoteFundMigration(ctx, valAddr, migrationId, "0xnot-a-real-hash", true)
	require.ErrorContains(t, err, "invalid tx hash")
}

// TestInitiateFundMigration_FirstMigrationIsVotable is the F-2026-18789
// regression.
//
// Migration ids used to come straight off collections.Sequence, whose first
// value is 0, while MsgVoteFundMigration.ValidateBasic rejects
// migration_id == 0 as "unset". The very first migration on a fresh chain was
// therefore unvotable: every vote died in ValidateBasic before it ever reached
// the keeper, the migration never left PendingMigrations, and
// InitiateFundMigration then refused every later migration for that chain —
// the lane was bricked with no way out short of an upgrade. audit-fixes is a
// fresh-genesis branch, so this fires on first use.
//
// Ids are now allocated as sequence + 1: the first id is 1 and 0 stays
// reserved for "unset". The three cases are separate subtests on purpose, so
// that each one reports independently instead of the first failure masking
// the rest.
func TestInitiateFundMigration_FirstMigrationIsVotable(t *testing.T) {
	// firstMigration runs the very first migration a fresh chain ever
	// allocates — the case that used to be unreachable — and returns its id.
	firstMigration := func(t *testing.T) (*app.ChainApp, sdk.Context, []string, string, uint64) {
		t.Helper()
		chainApp, ctx, universalVals, oldKeyId := setupFundMigrationTest(t, 3, false)

		seq, err := chainApp.UtssKeeper.NextMigrationId.Peek(ctx)
		require.NoError(t, err)
		require.Zero(t, seq, "fixture must start from a virgin sequence or this proves nothing")

		migrationId, err := chainApp.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err)
		return chainApp, ctx, universalVals, oldKeyId, migrationId
	}

	const txHash = "0xdeadbeef12345678deadbeef12345678deadbeef12345678deadbeef12345678"

	voteMsg := func(t *testing.T, val string, migrationId uint64) *utsstypes.MsgVoteFundMigration {
		t.Helper()
		valAddr, err := sdk.ValAddressFromBech32(val)
		require.NoError(t, err)
		return &utsstypes.MsgVoteFundMigration{
			Signer:      sdk.AccAddress(valAddr).String(),
			MigrationId: migrationId,
			TxHash:      txHash,
			Success:     true,
		}
	}

	t.Run("the first id is never the reserved 0", func(t *testing.T) {
		chainApp, ctx, _, _, migrationId := firstMigration(t)

		require.NotZero(t, migrationId,
			"the first migration id must never be 0: MsgVoteFundMigration.ValidateBasic rejects 0 as unset")
		require.Equal(t, uint64(1), migrationId)

		// The record really is stored under that votable id.
		migration, err := chainApp.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING, migration.Status)
		require.Equal(t, testChain, migration.Chain)
	})

	t.Run("a vote on the first migration passes ValidateBasic", func(t *testing.T) {
		_, _, universalVals, _, migrationId := firstMigration(t)

		// This is the exact gate that bricked the lane: the message a universal
		// validator broadcasts is rejected here, before the keeper is reached.
		msg := voteMsg(t, universalVals[0], migrationId)
		require.NoError(t, msg.ValidateBasic(),
			"a vote on the first migration must survive ValidateBasic")
	})

	t.Run("the first migration finalizes and unblocks the chain", func(t *testing.T) {
		chainApp, ctx, universalVals, oldKeyId, migrationId := firstMigration(t)
		msgServer := utsskeeper.NewMsgServerImpl(chainApp.UtssKeeper)

		for _, val := range universalVals {
			msg := voteMsg(t, val, migrationId)
			require.NoError(t, msg.ValidateBasic())
			_, err := msgServer.VoteFundMigration(ctx, msg)
			require.NoError(t, err, "a vote on the first migration must reach the ballot")
		}

		migration, err := chainApp.UtssKeeper.FundMigrations.Get(ctx, migrationId)
		require.NoError(t, err)
		require.Equal(t, utsstypes.FundMigrationStatus_FUND_MIGRATION_STATUS_COMPLETED, migration.Status,
			"votes on the first migration must be able to finalize it")
		require.Equal(t, txHash, migration.TxHash)

		_, err = chainApp.UtssKeeper.PendingMigrations.Get(ctx, migrationId)
		require.ErrorIs(t, err, collections.ErrNotFound,
			"a finalized migration must leave PendingMigrations, or the chain stays blocked")

		// The outcome the bug denied: the chain is migratable again, under the
		// next votable id.
		secondId, err := chainApp.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
		require.NoError(t, err, "a second migration must be possible once the first finalized")
		require.Equal(t, uint64(2), secondId)
	})
}

// TestVoteFundMigration_ZeroMigrationIdStaysRejected pins the other half of
// the F-2026-18789 contract: 0 keeps meaning "unset". The fix moves ids off 0
// rather than allowing 0, so this guard must stay in place.
func TestVoteFundMigration_ZeroMigrationIdStaysRejected(t *testing.T) {
	msg := &utsstypes.MsgVoteFundMigration{
		Signer:      sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		MigrationId: 0,
		TxHash:      "0xdeadbeef12345678deadbeef12345678deadbeef12345678deadbeef12345678",
		Success:     true,
	}
	require.ErrorContains(t, msg.ValidateBasic(), "migration_id is required")
}

// TestFundMigrationIdsSurviveGenesisRoundTrip guards the interaction between
// the sequence + 1 allocation and genesis: the exported counter must not hand
// an already-used id back after an export/import cycle.
func TestFundMigrationIdsSurviveGenesisRoundTrip(t *testing.T) {
	app, ctx, _, oldKeyId := setupFundMigrationTest(t, 3, false)

	firstId, err := app.UtssKeeper.InitiateFundMigration(ctx, oldKeyId, testChain, testBalance)
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstId)

	// ExportGenesis reads Params, which this fixture never seeds.
	require.NoError(t, app.UtssKeeper.Params.Set(ctx, utsstypes.Params{
		Admin: "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a",
	}))

	exported := app.UtssKeeper.ExportGenesis(ctx)
	require.Equal(t, uint64(1), exported.NextMigrationId)
	require.NoError(t, app.UtssKeeper.InitGenesis(ctx, exported))

	// The next allocation must not collide with the id already in state.
	seq, err := app.UtssKeeper.NextMigrationId.Next(ctx)
	require.NoError(t, err)
	require.Greater(t, seq+1, firstId,
		"an export/import cycle must not re-issue an id that is already taken")
}
