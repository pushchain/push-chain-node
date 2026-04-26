package v4_test

import (
	"context"
	"math/big"
	"testing"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/utss/keeper"
	v4 "github.com/pushchain/push-chain-node/x/utss/migrations/v4"
	"github.com/pushchain/push-chain-node/x/utss/types"
)

type stubUValidatorKeeper struct{ types.UValidatorKeeper }

func (stubUValidatorKeeper) IsTombstonedUniversalValidator(context.Context, string) (bool, error) {
	return false, nil
}
func (stubUValidatorKeeper) IsBondedUniversalValidator(context.Context, string) (bool, error) {
	return false, nil
}
func (stubUValidatorKeeper) GetEligibleVoters(context.Context) ([]uvalidatortypes.UniversalValidator, error) {
	return nil, nil
}
func (stubUValidatorKeeper) GetAllUniversalValidators(context.Context) ([]uvalidatortypes.UniversalValidator, error) {
	return nil, nil
}
func (stubUValidatorKeeper) UpdateValidatorStatus(context.Context, sdk.ValAddress, uvalidatortypes.UVStatus) error {
	return nil
}

type stubURegistryKeeper struct{}

func (stubURegistryKeeper) IsChainOutboundEnabled(context.Context, string) (bool, error) {
	return false, nil
}

type stubUExecutorKeeper struct{}

func (stubUExecutorKeeper) HasPendingOutboundsForChain(context.Context, string) (bool, error) {
	return false, nil
}
func (stubUExecutorKeeper) GetGasPriceByChain(sdk.Context, string) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (stubUExecutorKeeper) GetL1GasFeeByChain(sdk.Context, string) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (stubUExecutorKeeper) GetTssFundMigrationGasLimitByChain(sdk.Context, string) (*big.Int, error) {
	return big.NewInt(0), nil
}

func setupKeeper(t *testing.T) (sdk.Context, keeper.Keeper) {
	t.Helper()

	logger := log.NewTestLogger(t)
	encCfg := moduletestutil.MakeTestEncodingConfig()
	types.RegisterInterfaces(encCfg.InterfaceRegistry)

	keys := storetypes.NewKVStoreKeys(types.ModuleName)
	ctx := sdk.NewContext(integration.CreateMultiStore(keys, logger), cmtproto.Header{}, false, logger)

	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k := keeper.NewKeeper(
		encCfg.Codec,
		runtime.NewKVStoreService(keys[types.ModuleName]),
		logger,
		govAddr,
		stubUValidatorKeeper{},
		stubURegistryKeeper{},
		stubUExecutorKeeper{},
	)

	return ctx, k
}

func seedMigrations(t *testing.T, ctx sdk.Context, k keeper.Keeper, records []types.FundMigration) {
	t.Helper()
	for _, m := range records {
		require.NoError(t, k.FundMigrations.Set(ctx, m.Id, m))
	}
}

// TestMigrateFundMigrations_V4_BackfillsEmptyL1GasFee seeds legacy records
// (L1GasFee == "", as produced by pre-v4 proto decoding) and verifies the
// migration normalizes them to "0" while leaving every other field untouched.
func TestMigrateFundMigrations_V4_BackfillsEmptyL1GasFee(t *testing.T) {
	ctx, k := setupKeeper(t)

	legacy := []types.FundMigration{
		{
			Id:               1,
			OldKeyId:         "keygen-key-1",
			OldTssPubkey:     "old-pubkey-1",
			CurrentKeyId:     "keygen-key-2",
			CurrentTssPubkey: "new-pubkey-2",
			Chain:            "eip155:11155111",
			Status:           types.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING,
			InitiatedBlock:   100,
			GasPrice:         "1000000000",
			GasLimit:         21000,
			// L1GasFee deliberately empty — represents pre-v4 stored record.
		},
		{
			Id:               2,
			OldKeyId:         "keygen-key-a",
			OldTssPubkey:     "old-pubkey-a",
			CurrentKeyId:     "keygen-key-b",
			CurrentTssPubkey: "new-pubkey-b",
			Chain:            "eip155:84532",
			Status:           types.FundMigrationStatus_FUND_MIGRATION_STATUS_COMPLETED,
			InitiatedBlock:   200,
			CompletedBlock:   210,
			TxHash:           "0xabc",
			GasPrice:         "2000000000",
			GasLimit:         21000,
		},
	}
	seedMigrations(t, ctx, k, legacy)

	require.NoError(t, v4.MigrateFundMigrationsL1GasFee(ctx, &k))

	for _, old := range legacy {
		got, err := k.FundMigrations.Get(ctx, old.Id)
		require.NoError(t, err)
		require.Equal(t, "0", got.L1GasFee, "L1GasFee should be backfilled to \"0\"")
		require.Equal(t, old.OldKeyId, got.OldKeyId)
		require.Equal(t, old.Chain, got.Chain)
		require.Equal(t, old.Status, got.Status)
		require.Equal(t, old.GasPrice, got.GasPrice)
		require.Equal(t, old.GasLimit, got.GasLimit)
		require.Equal(t, old.TxHash, got.TxHash)
	}
}

// TestMigrateFundMigrations_V4_PreservesNonEmptyL1GasFee verifies that records
// which already carry a non-empty L1GasFee are not overwritten by the migration
// (idempotency + safety for re-runs).
func TestMigrateFundMigrations_V4_PreservesNonEmptyL1GasFee(t *testing.T) {
	ctx, k := setupKeeper(t)

	seeded := types.FundMigration{
		Id:               7,
		OldKeyId:         "keygen-key-x",
		OldTssPubkey:     "old-pubkey-x",
		CurrentKeyId:     "keygen-key-y",
		CurrentTssPubkey: "new-pubkey-y",
		Chain:            "eip155:10",
		Status:           types.FundMigrationStatus_FUND_MIGRATION_STATUS_PENDING,
		GasPrice:         "1500000000",
		GasLimit:         50000,
		L1GasFee:         "12345",
	}
	require.NoError(t, k.FundMigrations.Set(ctx, seeded.Id, seeded))

	require.NoError(t, v4.MigrateFundMigrationsL1GasFee(ctx, &k))

	got, err := k.FundMigrations.Get(ctx, seeded.Id)
	require.NoError(t, err)
	require.Equal(t, "12345", got.L1GasFee, "existing L1GasFee must not be overwritten")
}

// TestMigrateFundMigrations_V4_EmptyStore ensures the migration is a no-op
// when there are no FundMigration records.
func TestMigrateFundMigrations_V4_EmptyStore(t *testing.T) {
	ctx, k := setupKeeper(t)

	require.NoError(t, v4.MigrateFundMigrationsL1GasFee(ctx, &k))

	iter, err := k.FundMigrations.Iterate(ctx, nil)
	require.NoError(t, err)
	defer iter.Close()
	require.False(t, iter.Valid())
}
