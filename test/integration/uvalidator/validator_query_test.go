package integrationtest

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uvalidatorkeepermod "github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// registerUV is a helper that registers a staking validator as a universal
// validator (PENDING_JOIN) with synthetic network info.
func registerUV(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, val stakingtypes.Validator, idx int) {
	t.Helper()
	network := uvalidatortypes.NetworkInfo{
		PeerId:     fmt.Sprintf("peer%d", idx),
		MultiAddrs: []string{fmt.Sprintf("/ip4/127.0.0.%d/tcp/4001", idx)},
	}
	err := chainApp.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, network)
	require.NoError(t, err)
}

// setUVStatus directly writes a validator with the requested status into the
// universal validator set, bypassing lifecycle-transition rules.
func setUVStatus(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, val stakingtypes.Validator, status uvalidatortypes.UVStatus) {
	t.Helper()
	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	require.NoError(t, err)
	uv := uvalidatortypes.UniversalValidator{
		IdentifyInfo: &uvalidatortypes.IdentityInfo{CoreValidatorAddress: val.OperatorAddress},
		LifecycleInfo: &uvalidatortypes.LifecycleInfo{
			CurrentStatus: status,
		},
		NetworkInfo: &uvalidatortypes.NetworkInfo{PeerId: "peer", MultiAddrs: []string{"addr"}},
	}
	require.NoError(t, chainApp.UvalidatorKeeper.UniversalValidatorSet.Set(ctx, valAddr, uv))
}

// setupQueryTest creates an app with numVals staking validators and
// registers all of them as universal validators (PENDING_JOIN).
func setupQueryTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []stakingtypes.Validator) {
	t.Helper()
	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)
	for i, val := range validators {
		registerUV(t, chainApp, ctx, val, i+1)
	}
	return chainApp, ctx, validators
}

// ---------------------------------------------------------------------------
// keeper.GetAllUniversalValidators
// ---------------------------------------------------------------------------

func TestGetAllUniversalValidators(t *testing.T) {
	t.Run("returns all registered validators", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)

		all, err := chainApp.UvalidatorKeeper.GetAllUniversalValidators(ctx)
		require.NoError(t, err)
		require.Len(t, all, len(validators))
	})

	t.Run("returns empty slice when no validators registered", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 2)

		all, err := chainApp.UvalidatorKeeper.GetAllUniversalValidators(ctx)
		require.NoError(t, err)
		require.Empty(t, all)
	})

	t.Run("returned validators carry correct addresses", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)

		all, err := chainApp.UvalidatorKeeper.GetAllUniversalValidators(ctx)
		require.NoError(t, err)

		// Build a set of expected operator addresses.
		expected := make(map[string]struct{}, len(validators))
		for _, v := range validators {
			expected[v.OperatorAddress] = struct{}{}
		}

		for _, uv := range all {
			addr := uv.IdentifyInfo.CoreValidatorAddress
			_, ok := expected[addr]
			require.True(t, ok, "unexpected validator address: %s", addr)
		}
	})
}

// ---------------------------------------------------------------------------
// keeper.GetValidatorsByStatus
// ---------------------------------------------------------------------------

func TestGetValidatorsByStatus(t *testing.T) {
	t.Run("filters PENDING_JOIN validators", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)

		pending, err := chainApp.UvalidatorKeeper.GetValidatorsByStatus(ctx, uvalidatortypes.UVStatus_UV_STATUS_PENDING_JOIN)
		require.NoError(t, err)
		// All 3 were registered via AddUniversalValidator which sets PENDING_JOIN.
		require.Len(t, pending, len(validators))
	})

	t.Run("returns only ACTIVE validators when mixed statuses present", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)

		// Overwrite two validators as ACTIVE and one as PENDING_LEAVE.
		setUVStatus(t, chainApp, ctx, validators[0], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		setUVStatus(t, chainApp, ctx, validators[1], uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		setUVStatus(t, chainApp, ctx, validators[2], uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE)

		active, err := chainApp.UvalidatorKeeper.GetValidatorsByStatus(ctx, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		require.NoError(t, err)
		require.Len(t, active, 2)

		for _, uv := range active {
			require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE, uv.LifecycleInfo.CurrentStatus)
		}
	})

	t.Run("returns empty when no validators match the requested status", func(t *testing.T) {
		chainApp, ctx, _ := setupQueryTest(t, 2)

		// No validator has INACTIVE status.
		inactive, err := chainApp.UvalidatorKeeper.GetValidatorsByStatus(ctx, uvalidatortypes.UVStatus_UV_STATUS_INACTIVE)
		require.NoError(t, err)
		require.Empty(t, inactive)
	})

	t.Run("returns empty when validator set is empty", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		result, err := chainApp.UvalidatorKeeper.GetValidatorsByStatus(ctx, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)
		require.NoError(t, err)
		require.Empty(t, result)
	})
}

// ---------------------------------------------------------------------------
// keeper.GetUniversalValidator
// ---------------------------------------------------------------------------

func TestGetUniversalValidator(t *testing.T) {
	t.Run("found case returns validator and true", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		val := validators[0]
		valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)

		uv, found, err := chainApp.UvalidatorKeeper.GetUniversalValidator(ctx, valAddr)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, val.OperatorAddress, uv.IdentifyInfo.CoreValidatorAddress)
	})

	t.Run("not-found case returns false without error", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		// Use a random address that was never registered.
		nonExistent := sdk.ValAddress([]byte("nonexistent-address-"))
		_, found, err := chainApp.UvalidatorKeeper.GetUniversalValidator(ctx, nonExistent)
		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("retrieved validator has the expected status", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 1)
		val := validators[0]

		// Overwrite with ACTIVE.
		setUVStatus(t, chainApp, ctx, val, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE)

		valAddr, _ := sdk.ValAddressFromBech32(val.OperatorAddress)
		uv, found, err := chainApp.UvalidatorKeeper.GetUniversalValidator(ctx, valAddr)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE, uv.LifecycleInfo.CurrentStatus)
	})
}

// ---------------------------------------------------------------------------
// query_server.Params
// ---------------------------------------------------------------------------

func TestQueryParams(t *testing.T) {
	t.Run("returns module params without error", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		// Initialize params so they exist in state
		defaultParams := uvalidatortypes.DefaultParams()
		require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, defaultParams))

		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		resp, err := querier.Params(ctx, &uvalidatortypes.QueryParamsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Params)
	})

	t.Run("returned admin matches default params", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		defaultParams := uvalidatortypes.DefaultParams()
		require.NoError(t, chainApp.UvalidatorKeeper.Params.Set(ctx, defaultParams))

		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		resp, err := querier.Params(ctx, &uvalidatortypes.QueryParamsRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Params.Admin)
	})
}

// ---------------------------------------------------------------------------
// query_server.AllUniversalValidators
// ---------------------------------------------------------------------------

func TestQueryAllUniversalValidators(t *testing.T) {
	t.Run("returns all registered validators", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		resp, err := querier.AllUniversalValidators(ctx, &uvalidatortypes.QueryUniversalValidatorsSetRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.UniversalValidator, len(validators))
	})

	t.Run("returns empty list when no validators registered", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 2)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		resp, err := querier.AllUniversalValidators(ctx, &uvalidatortypes.QueryUniversalValidatorsSetRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.UniversalValidator)
	})
}

// ---------------------------------------------------------------------------
// query_server.UniversalValidator
// ---------------------------------------------------------------------------

func TestQueryUniversalValidator(t *testing.T) {
	t.Run("returns validator for a known address", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 2)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		target := validators[0]
		resp, err := querier.UniversalValidator(ctx, &uvalidatortypes.QueryUniversalValidatorRequest{
			CoreValidatorAddress: target.OperatorAddress,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, target.OperatorAddress, resp.UniversalValidator.IdentifyInfo.CoreValidatorAddress)
	})

	t.Run("returns NotFound gRPC error for unknown address", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		// A random bech32 validator address that was never registered.
		fakeAddr := sdk.ValAddress([]byte("fake-validator-addr-"))
		_, err := querier.UniversalValidator(ctx, &uvalidatortypes.QueryUniversalValidatorRequest{
			CoreValidatorAddress: fakeAddr.String(),
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("returns InvalidArgument error for empty address", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		_, err := querier.UniversalValidator(ctx, &uvalidatortypes.QueryUniversalValidatorRequest{
			CoreValidatorAddress: "",
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "core validator address is required")
	})

	t.Run("returns InvalidArgument error for malformed address", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		_, err := querier.UniversalValidator(ctx, &uvalidatortypes.QueryUniversalValidatorRequest{
			CoreValidatorAddress: "not_a_valid_bech32",
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid validator address")
	})

	t.Run("returns nil error for nil request", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		_, err := querier.UniversalValidator(ctx, nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "core validator address is required")
	})
}

// ---------------------------------------------------------------------------
// query_server.Ballot
// ---------------------------------------------------------------------------

func TestQueryBallot(t *testing.T) {
	t.Run("returns ballot by ID", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		voters := make([]string, len(validators))
		for i, v := range validators {
			voters[i] = v.OperatorAddress
		}

		ballot, err := chainApp.UvalidatorKeeper.CreateBallot(
			ctx,
			"test-ballot-1",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters,
			2,
			100,
		)
		require.NoError(t, err)

		resp, err := querier.Ballot(ctx, &uvalidatortypes.QueryBallotRequest{Id: ballot.Id})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, ballot.Id, resp.Ballot.Id)
		require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, resp.Ballot.Status)
	})

	t.Run("returns NotFound error for unknown ballot ID", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		_, err := querier.Ballot(ctx, &uvalidatortypes.QueryBallotRequest{Id: "nonexistent-ballot"})
		require.Error(t, err)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("returns InvalidArgument error for nil request", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		_, err := querier.Ballot(ctx, nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid request")
	})
}

// ---------------------------------------------------------------------------
// query_server.AllBallots
// ---------------------------------------------------------------------------

func TestQueryAllBallots(t *testing.T) {
	t.Run("returns all ballots", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		voters := make([]string, len(validators))
		for i, v := range validators {
			voters[i] = v.OperatorAddress
		}

		const numBallots = 3
		for i := 0; i < numBallots; i++ {
			_, err := chainApp.UvalidatorKeeper.CreateBallot(
				ctx,
				fmt.Sprintf("ballot-%d", i),
				uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
				voters,
				2,
				100,
			)
			require.NoError(t, err)
		}

		resp, err := querier.AllBallots(ctx, &uvalidatortypes.QueryBallotsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Ballots, numBallots)
	})

	t.Run("returns empty list when no ballots exist", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		resp, err := querier.AllBallots(ctx, &uvalidatortypes.QueryBallotsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Ballots)
	})

	t.Run("returns InvalidArgument error for nil request", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		_, err := querier.AllBallots(ctx, nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid request")
	})
}

// ---------------------------------------------------------------------------
// query_server.AllActiveBallotIDs (implemented on Keeper directly)
// ---------------------------------------------------------------------------

func TestQueryAllActiveBallotIDs(t *testing.T) {
	t.Run("returns IDs of active ballots", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)

		voters := make([]string, len(validators))
		for i, v := range validators {
			voters[i] = v.OperatorAddress
		}

		const numBallots = 3
		for i := 0; i < numBallots; i++ {
			_, err := chainApp.UvalidatorKeeper.CreateBallot(
				ctx,
				fmt.Sprintf("active-ballot-%d", i),
				uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
				voters,
				2,
				100,
			)
			require.NoError(t, err)
		}

		resp, err := chainApp.UvalidatorKeeper.AllActiveBallotIDs(ctx, &uvalidatortypes.QueryActiveBallotIDsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Ids, numBallots)
	})

	t.Run("returns empty when no active ballots", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		resp, err := chainApp.UvalidatorKeeper.AllActiveBallotIDs(ctx, &uvalidatortypes.QueryActiveBallotIDsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Ids)
	})

	t.Run("finalized ballot is removed from active IDs", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)

		voters := make([]string, len(validators))
		for i, v := range validators {
			voters[i] = v.OperatorAddress
		}

		ballot, err := chainApp.UvalidatorKeeper.CreateBallot(
			ctx,
			"finalize-me",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_TSS_KEY,
			voters,
			2,
			100,
		)
		require.NoError(t, err)

		// Finalize the ballot.
		err = chainApp.UvalidatorKeeper.MarkBallotFinalized(ctx, ballot.Id, uvalidatortypes.BallotStatus_BALLOT_STATUS_PASSED)
		require.NoError(t, err)

		resp, err := chainApp.UvalidatorKeeper.AllActiveBallotIDs(ctx, &uvalidatortypes.QueryActiveBallotIDsRequest{})
		require.NoError(t, err)
		require.NotContains(t, resp.Ids, ballot.Id)
	})

	t.Run("returns InvalidArgument error for nil request", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)

		_, err := chainApp.UvalidatorKeeper.AllActiveBallotIDs(ctx, nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid request")
	})
}

// ---------------------------------------------------------------------------
// query_server.AllActiveBallots
// ---------------------------------------------------------------------------

func TestQueryAllActiveBallots(t *testing.T) {
	t.Run("returns full ballot objects for active ballots", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		voters := make([]string, len(validators))
		for i, v := range validators {
			voters[i] = v.OperatorAddress
		}

		const numBallots = 2
		for i := 0; i < numBallots; i++ {
			_, err := chainApp.UvalidatorKeeper.CreateBallot(
				ctx,
				fmt.Sprintf("active-full-%d", i),
				uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
				voters,
				2,
				100,
			)
			require.NoError(t, err)
		}

		resp, err := querier.AllActiveBallots(ctx, &uvalidatortypes.QueryActiveBallotsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Ballots, numBallots)

		for _, b := range resp.Ballots {
			require.Equal(t, uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, b.Status)
		}
	})

	t.Run("returns empty list when no active ballots", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		resp, err := querier.AllActiveBallots(ctx, &uvalidatortypes.QueryActiveBallotsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Ballots)
	})

	t.Run("expired ballot does not appear in active ballots", func(t *testing.T) {
		chainApp, ctx, validators := setupQueryTest(t, 3)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		voters := make([]string, len(validators))
		for i, v := range validators {
			voters[i] = v.OperatorAddress
		}

		ballot, err := chainApp.UvalidatorKeeper.CreateBallot(
			ctx,
			"expire-me",
			uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX,
			voters,
			2,
			100,
		)
		require.NoError(t, err)

		// Mark it expired.
		err = chainApp.UvalidatorKeeper.MarkBallotExpired(ctx, ballot.Id)
		require.NoError(t, err)

		resp, err := querier.AllActiveBallots(ctx, &uvalidatortypes.QueryActiveBallotsRequest{})
		require.NoError(t, err)
		for _, b := range resp.Ballots {
			require.NotEqual(t, ballot.Id, b.Id, "expired ballot must not appear in active ballots")
		}
	})

	t.Run("returns InvalidArgument error for nil request", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		querier := uvalidatorkeepermod.NewQuerier(chainApp.UvalidatorKeeper)

		_, err := querier.AllActiveBallots(ctx, nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid request")
	})
}

// ---------------------------------------------------------------------------
// msg_server dispatch — UpdateParams authorization check
// ---------------------------------------------------------------------------

func TestMsgServerUpdateParams(t *testing.T) {
	t.Run("rejects call from non-authority address", func(t *testing.T) {
		chainApp, ctx, _, _ := utils.SetAppWithMultipleValidators(t, 1)
		ms := uvalidatorkeepermod.NewMsgServerImpl(chainApp.UvalidatorKeeper)

		newParams := uvalidatortypes.Params{Admin: "push1negskcfqu09j5zvpk7nhvacnwyy2mafffy7r6a"}
		_, err := ms.UpdateParams(ctx, &uvalidatortypes.MsgUpdateParams{
			Authority: "push1notthegovernance000000000000000000000",
			Params:    newParams,
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid authority")
	})
}
