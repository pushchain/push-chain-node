package integrationtest

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// tombstoneValidator forces the given staking validator into a tombstoned
// state on the slashing module. Mirrors what slashing would do after a
// double-sign infraction.
func tombstoneValidator(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, val stakingtypes.Validator) {
	t.Helper()

	consAddr, err := val.GetConsAddr()
	require.NoError(t, err)

	// SigningInfo must exist before Tombstone is called.
	info := slashingtypes.NewValidatorSigningInfo(consAddr, ctx.BlockHeight(), 0, time.Time{}, false, 0)
	require.NoError(t, chainApp.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, info))
	require.NoError(t, chainApp.SlashingKeeper.Tombstone(ctx, consAddr))
}

// TestGetEligibleVoters_FiltersStrandedValidators is the F-2026-16991
// regression suite: confirms the read-time staking filter prevents stranded
// UVs (unbonded / jailed / tombstoned / removed from staking) from inflating
// the eligible-voter count, which is the denominator used to compute the
// ballot quorum threshold.
func TestGetEligibleVoters_FiltersStrandedValidators(t *testing.T) {
	t.Run("includes ACTIVE+bonded+non-tombstoned validators", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		voters, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, voters, 3, "all three ACTIVE+bonded validators must be eligible")
	})

	t.Run("includes PENDING_JOIN+bonded validators", func(t *testing.T) {
		// setupQueryTest registers all validators in PENDING_JOIN via AddUniversalValidator.
		chainApp, ctx, validators := setupQueryTest(t, 2)

		voters, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, voters, len(validators),
			"PENDING_JOIN validators with bonded staking state must be eligible")
	})

	t.Run("excludes ACTIVE but UNBONDED validators", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		// Unbond the third validator on the base chain. UV row stays ACTIVE.
		unbonded := validators[2]
		unbonded.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, unbonded))

		voters, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, voters, 2, "unbonded validator must be excluded even when UV row is ACTIVE")
	})

	t.Run("excludes ACTIVE but TOMBSTONED validators", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		// Tombstone the first validator.
		tombstoneValidator(t, chainApp, ctx, validators[0])

		voters, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, voters, 2, "tombstoned validator must be excluded even when UV row is ACTIVE")

		// Sanity: the one excluded was the tombstoned one.
		for _, v := range voters {
			require.NotEqual(t, validators[0].OperatorAddress, v.IdentifyInfo.CoreValidatorAddress)
		}
	})

	t.Run("excludes non-ACTIVE/PENDING_JOIN lifecycle statuses", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		setUVStatus(t, chainApp, ctx, validators[1], uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE)
		setUVStatus(t, chainApp, ctx, validators[2], uvalidatortypes.UVStatus_UV_STATUS_INACTIVE)

		voters, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, voters, 1, "only the ACTIVE validator should be eligible")
		require.Equal(t, validators[0].OperatorAddress, voters[0].IdentifyInfo.CoreValidatorAddress)
	})

	t.Run("multiple stranded validators all excluded — the deadlock-prevention case", func(t *testing.T) {
		// 5 validators, all ACTIVE on paper, but 3 are stranded. Without the
		// filter, GetEligibleVoters would return 5 → ballot threshold becomes
		// 4 (>= 2/3 of 5) → only 2 live voters → unreachable → permanent
		// deadlock at the executor layer. With the filter, returns 2 →
		// threshold becomes 2 → still finalizable.
		chainApp, ctx, validators := setupQueryTest(t, 5)
		for _, v := range validators {
			setUVStatus(t, chainApp, ctx, v, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		}

		// Strand 3 of 5: two unbonded, one tombstoned.
		unbondedA := validators[0]
		unbondedA.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, unbondedA))

		unbondedB := validators[1]
		unbondedB.Status = stakingtypes.Unbonded
		require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, unbondedB))

		tombstoneValidator(t, chainApp, ctx, validators[2])

		voters, err := chainApp.UvalidatorKeeper.GetEligibleVoters(ctx)
		require.NoError(t, err)
		require.Len(t, voters, 2,
			"only the 2 still-bonded non-stranded validators should be eligible — "+
				"this is the denominator that prevents ballot quorum deadlock")
	})
}
