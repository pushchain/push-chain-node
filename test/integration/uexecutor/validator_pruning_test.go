package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// setupValidatorPruningTest sets up the environment for validator pruning tests
// with chain meta voting capabilities. It registers universal validators and
// grants authz for VoteChainMeta.
func setupValidatorPruningTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, []stakingtypes.Validator) {
	t.Helper()

	testApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	chainConfig := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}
	require.NoError(t, testApp.UregistryKeeper.AddChainConfig(ctx, &chainConfig))

	universalVals := make([]string, len(validators))
	for i, val := range validators {
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		require.NoError(t, testApp.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, network))
		universalVals[i] = sdk.AccAddress([]byte(fmt.Sprintf("universal-validator-%d", i))).String()
	}

	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		coreAcc := sdk.AccAddress(accAddr)
		uniAcc := sdk.MustAccAddressFromBech32(universalVals[i])
		auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteChainMeta{}))
		exp := ctx.BlockTime().Add(time.Hour)
		require.NoError(t, testApp.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp))
	}

	return testApp, ctx, universalVals, validators
}

func TestValidatorPruningChainMeta(t *testing.T) {
	chainId := "eip155:11155111"

	t.Run("removing validator triggers status change to pending leave", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupValidatorPruningTest(t, 4)

		// Clear the TSS process so validators can transition
		_ = testApp.UtssKeeper.CurrentTssProcess.Remove(ctx)

		// Promote all validators to ACTIVE so removal transitions to PENDING_LEAVE
		for _, val := range vals {
			valAddr, _ := sdk.ValAddressFromBech32(val.OperatorAddress)
			require.NoError(t, testApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE))
		}

		// All 4 validators vote on chain meta with increasing heights
		coreAccs := make([]string, len(vals))
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100, 1))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 200, 2))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 300, 3))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[3], coreAccs[3], chainId, 400, 4))

		// Verify all 4 votes are recorded
		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, stored.Signers, 4, "should have 4 signer entries before removal")

		// Remove validator[3] -- transitions ACTIVE -> PENDING_LEAVE
		err = testApp.UvalidatorKeeper.RemoveUniversalValidator(ctx, vals[3].OperatorAddress)
		require.NoError(t, err)

		// Verify the validator is now in PENDING_LEAVE status
		valAddr3, _ := sdk.ValAddressFromBech32(vals[3].OperatorAddress)
		uv, found, err := testApp.UvalidatorKeeper.GetUniversalValidator(ctx, valAddr3)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t,
			uvalidatortypes.UVStatus_UV_STATUS_PENDING_LEAVE,
			uv.LifecycleInfo.CurrentStatus,
			"removed validator should be in PENDING_LEAVE status",
		)

		// Chain meta is pruned on validator removal -- removed validator's vote is gone
		storedAfter, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, storedAfter.Signers, 3,
			"chain meta should have 3 signers after removing validator[3]")
	})

	t.Run("removed validator vote becomes stale after time window", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupValidatorPruningTest(t, 3)

		// Clear TSS process and promote to ACTIVE so removal works
		_ = testApp.UtssKeeper.CurrentTssProcess.Remove(ctx)
		for _, val := range vals {
			valAddr, _ := sdk.ValAddressFromBech32(val.OperatorAddress)
			require.NoError(t, testApp.UvalidatorKeeper.UpdateValidatorStatus(ctx, valAddr, uvalidatortypes.UVStatus_UV_STATUS_ACTIVE))
		}

		coreAccs := make([]string, len(vals))
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		// All 3 validators vote
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100, 1))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 300, 2))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 200, 3))

		// Remove validator[2]
		err := testApp.UvalidatorKeeper.RemoveUniversalValidator(ctx, vals[2].OperatorAddress)
		require.NoError(t, err)

		// Advance time past the staleness window (300 seconds)
		ctx = ctx.WithBlockTime(ctx.BlockTime().Add(301 * time.Second))

		// Only val[0] re-votes with a fresh vote. The removed validator's old vote
		// and val[1]'s old vote are now stale.
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 500, 4))

		// The median should now be based only on the fresh vote (val[0]'s 500),
		// effectively excluding the removed validator's stale vote.
		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uint64(4), stored.LastAppliedChainHeight,
			"LastAppliedChainHeight should reflect only fresh votes")
	})

	t.Run("median recomputes correctly when a validator with middle value is stale", func(t *testing.T) {
		testApp, ctx, uvals, vals := setupValidatorPruningTest(t, 3)

		coreAccs := make([]string, len(vals))
		for i := range vals {
			coreVal, _ := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
			coreAccs[i] = sdk.AccAddress(coreVal).String()
		}

		// 3 validators vote different prices:
		// val[0]=100, val[1]=300 (middle), val[2]=500
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100, 1))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[1], coreAccs[1], chainId, 300, 2))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 500, 3))

		// Initial median: sorted [100, 300, 500] -> upper median index 1 -> 300
		stored, _, _ := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		initialMedianPrice := stored.Prices[stored.MedianIndex]
		require.Equal(t, uint64(300), initialMedianPrice, "initial median should be 300")

		// Advance time so all current votes become stale
		ctx = ctx.WithBlockTime(ctx.BlockTime().Add(301 * time.Second))

		// Only val[0] and val[2] re-vote (skipping the middle validator val[1])
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[0], coreAccs[0], chainId, 100, 4))
		require.NoError(t, utils.ExecVoteChainMeta(t, ctx, testApp, uvals[2], coreAccs[2], chainId, 500, 5))

		// Now only 2 fresh votes: [100, 500]
		// Upper median at index len/2 = 1 -> 500
		stored, found, err := testApp.UexecutorKeeper.GetChainMeta(ctx, chainId)
		require.NoError(t, err)
		require.True(t, found)

		// Verify the median was recomputed without the stale middle vote
		require.Equal(t, uint64(5), stored.LastAppliedChainHeight,
			"LastAppliedChainHeight should update to fresh median")
	})
}
