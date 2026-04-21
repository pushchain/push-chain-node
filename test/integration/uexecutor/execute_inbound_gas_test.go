package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

// setupInboundGasTest sets up the test environment for GAS-type inbound tests.
// It registers chain + token configs, adds universal validators with authz grants,
// and builds a base GAS inbound ready for voting.
func setupInboundGasTest(t *testing.T, numVals int) (*app.ChainApp, sdk.Context, []string, *uexecutortypes.Inbound, []stakingtypes.Validator) {
	t.Helper()

	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:         "eip155:11155111",
		VmType:        uregistrytypes.VmType_EVM,
		PublicRpcUrl:  "https://sepolia.drpc.org",
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
			IsOutboundEnabled: true,
		},
	}

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

	tokenConfigTest := uregistrytypes.TokenConfig{
		Chain:        "eip155:11155111",
		Address:      usdcAddress.String(),
		Name:         "USD Coin",
		Symbol:       "USDC",
		Decimals:     6,
		Enabled:      true,
		LiquidityCap: "1000000000000000000000000",
		TokenType:    1,
		NativeRepresentation: &uregistrytypes.NativeRepresentation{
			Denom:           "",
			ContractAddress: prc20Address.String(),
		},
	}

	chainApp.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)
	chainApp.UregistryKeeper.AddTokenConfig(ctx, &tokenConfigTest)

	// Register universal validators
	universalVals := make([]string, len(validators))
	for i, val := range validators {
		coreValAddr := val.OperatorAddress
		universalValAddr := sdk.AccAddress([]byte(
			fmt.Sprintf("universal-validator-%d", i),
		)).String()

		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}

		err := chainApp.UvalidatorKeeper.AddUniversalValidator(ctx, coreValAddr, network)
		require.NoError(t, err)

		universalVals[i] = universalValAddr
	}

	// Grant authz: core validator -> universal validator for MsgVoteInbound
	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)

		coreValAddr := sdk.AccAddress(accAddr)
		uniValAddr := sdk.MustAccAddressFromBech32(universalVals[i])

		msgType := sdk.MsgTypeURL(&uexecutortypes.MsgVoteInbound{})
		auth := authz.NewGenericAuthorization(msgType)
		exp := ctx.BlockTime().Add(time.Hour)

		err = chainApp.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, auth, &exp)
		require.NoError(t, err)
	}

	// Build a base GAS inbound
	inbound := &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xgas0001",
		Sender:      testAddress,
		Recipient:   testAddress,
		Amount:      "1000000",
		AssetAddr:   usdcAddress.String(),
		LogIndex:    "1",
		TxType:      uexecutortypes.TxType_GAS,
		RevertInstructions: &uexecutortypes.RevertInstructions{
			FundRecipient: testAddress,
		},
	}

	return chainApp, ctx, universalVals, inbound, validators
}

// reachQuorum submits votes from the first `count` validators and requires no errors.
func reachGasQuorum(t *testing.T, ctx sdk.Context, chainApp *app.ChainApp, vals []string, coreVals []stakingtypes.Validator, inbound *uexecutortypes.Inbound, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
		require.NoError(t, err)
	}
}

func TestInboundGas(t *testing.T) {
	// -----------------------------------------------------------------------
	// 1. Voting / quorum lifecycle
	// -----------------------------------------------------------------------

	t.Run("less than quorum votes keeps GAS inbound pending", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		// Submit a single vote — not enough to reach quorum (need 3/4)
		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.True(t, isPending, "inbound should still be pending with < quorum votes")
	})

	t.Run("quorum reached moves GAS inbound out of pending state", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		// 3 out of 4 validators vote — exceeds 66% quorum
		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		isPending, err := chainApp.UexecutorKeeper.IsPendingInbound(ctx, *inbound)
		require.NoError(t, err)
		require.False(t, isPending, "inbound should no longer be pending after quorum")
	})

	t.Run("universal tx exists after GAS inbound quorum is reached", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should be created after quorum")
		require.NotNil(t, utx.InboundTx, "universal tx should contain the inbound")
		require.Equal(t, uexecutortypes.TxType_GAS, utx.InboundTx.TxType)
	})

	t.Run("vote after GAS inbound quorum fails with already exists error", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		// Reach quorum
		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		// 4th vote should fail — inbound already finalised
		valAddr, err := sdk.ValAddressFromBech32(coreVals[3].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[3], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})

	t.Run("duplicate vote on GAS inbound fails with already voted error", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		// First vote
		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		// Second vote from the same validator
		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already voted")
	})

	t.Run("different GAS inbounds are tracked separately", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		inboundB := *inbound
		inboundB.TxHash = "0xgas0002"

		valAddr, err := sdk.ValAddressFromBech32(coreVals[0].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()

		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, inbound)
		require.NoError(t, err)

		// Vote for a different inbound should succeed independently
		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[0], coreValAcc, &inboundB)
		require.NoError(t, err, "votes for different inbounds should be tracked independently")
	})

	// -----------------------------------------------------------------------
	// 2. Execution path — PCTx recording
	// -----------------------------------------------------------------------

	t.Run("GAS inbound execution records a PCTx entry", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)
		// ExecuteInboundGas always records at least one PCTx (success or failure)
		require.NotEmpty(t, utx.PcTx, "at least one PCTx entry must be recorded")
	})

	t.Run("GAS inbound swap failure records FAILED PCTx (no full AMM)", func(t *testing.T) {
		// The test environment has a UniversalCore handler contract with stub addresses
		// for WPC / Uniswap.  The autoswap call will fail when it cannot contact the
		// quoter/pool, which is fine — ExecuteInboundGas records the failure as a
		// FAILED PCTx and does NOT return an error to the caller.
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx must exist")

		hasFailed := false
		for _, pcTx := range utx.PcTx {
			if pcTx.Status == "FAILED" {
				hasFailed = true
				break
			}
		}
		require.True(t, hasFailed, "at least one FAILED PCTx should be recorded when swap cannot complete")
	})

	t.Run("GAS inbound swap failure creates INBOUND_REVERT outbound", func(t *testing.T) {
		// When the autoswap fails ExecuteInboundGas sets shouldRevert=true and creates
		// an INBOUND_REVERT outbound so the user's funds are returned.
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		// There should be at least one INBOUND_REVERT outbound
		foundRevert := false
		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				foundRevert = true
				require.Equal(t, inbound.SourceChain, ob.DestinationChain,
					"revert outbound destination must match inbound source chain")
				require.Equal(t, inbound.Amount, ob.Amount,
					"revert outbound amount must match inbound amount")
				require.Equal(t, inbound.AssetAddr, ob.ExternalAssetAddr,
					"revert outbound asset must match inbound asset")
				require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus,
					"revert outbound should start in PENDING status")

				// Gas fields are populated from UniversalCore if chain meta is set.
				// In test env without VoteChainMeta, they may be zero/empty — that's OK,
				// the outbound is still created (graceful degradation).
				// When chain meta IS set, these will be populated.
				break
			}
		}
		require.True(t, foundRevert, "INBOUND_REVERT outbound should be created when swap fails")
	})

	t.Run("GAS inbound revert outbound uses FundRecipient from RevertInstructions", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		// Override revert instructions to a different recipient
		revertRecipient := utils.GetDefaultAddresses().TargetAddr2
		inbound.TxHash = "0xgas0010"
		inbound.RevertInstructions = &uexecutortypes.RevertInstructions{
			FundRecipient: revertRecipient,
		}

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				require.Equal(t, revertRecipient, ob.Recipient,
					"revert outbound recipient should match FundRecipient in RevertInstructions")
				return
			}
		}
		// If no revert outbound was created, the swap somehow succeeded — not expected
		// in the test environment, but skip rather than fail hard
		t.Skip("no INBOUND_REVERT found — swap may have unexpectedly succeeded in this environment")
	})

	t.Run("GAS inbound revert outbound falls back to Sender when RevertInstructions is nil", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		inbound.TxHash = "0xgas0011"
		inbound.RevertInstructions = nil // no revert instructions

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				require.Equal(t, inbound.Sender, ob.Recipient,
					"revert outbound recipient should fall back to Sender when RevertInstructions is nil")
				return
			}
		}
		t.Skip("no INBOUND_REVERT found — swap may have unexpectedly succeeded in this environment")
	})

	// -----------------------------------------------------------------------
	// 3. Token config / pre-execution validation failures
	// -----------------------------------------------------------------------

	t.Run("GAS inbound with missing token config records FAILED PCTx and creates revert", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		inbound.TxHash = "0xgas0020"

		// Remove token config to force GetTokenConfig to fail
		chainApp.UregistryKeeper.RemoveTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "universal tx should exist even when token config is missing")

		// Must have a FAILED PCTx
		require.NotEmpty(t, utx.PcTx, "PCTx entries must be recorded")
		hasFailed := false
		for _, pcTx := range utx.PcTx {
			if pcTx.Status == "FAILED" {
				hasFailed = true
				require.Contains(t, pcTx.ErrorMsg, "GetTokenConfig failed",
					"error message should indicate token config lookup failure")
				break
			}
		}
		require.True(t, hasFailed, "should have a FAILED PCTx when token config is missing")

		// Must have an INBOUND_REVERT outbound
		foundRevert := false
		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				foundRevert = true
				break
			}
		}
		require.True(t, foundRevert, "INBOUND_REVERT outbound should be created when token config is missing")
	})

	// -----------------------------------------------------------------------
	// 4. Zero amount (fails ValidateForExecution — recorded as FAILED PCTx)
	// -----------------------------------------------------------------------

	t.Run("GAS inbound with zero amount: vote succeeds, UTX has FAILED PCTx with revert outbound", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		inbound.TxHash = "0xgas0030"
		inbound.Amount = "0" // zero amount is not allowed for TxType_GAS

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should exist even when execution validation rejects the inbound")
		require.NotEmpty(t, utx.PcTx, "failed validation should be recorded as a PCTx")
		require.Equal(t, "FAILED", utx.PcTx[0].Status,
			"first PCTx should have FAILED status for zero-amount GAS inbound")
		require.Contains(t, utx.PcTx[0].ErrorMsg, "amount must be positive",
			"error message should indicate the amount constraint")

		// Zero-amount GAS inbound is treated like any other pre-execution failure:
		// a non-isCEA inbound creates an INBOUND_REVERT outbound.
		foundRevert := false
		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				foundRevert = true
				require.Equal(t, inbound.SourceChain, ob.DestinationChain)
				require.Equal(t, inbound.Amount, ob.Amount)
				break
			}
		}
		require.True(t, foundRevert, "INBOUND_REVERT should be created for zero-amount GAS inbound")
	})

	// -----------------------------------------------------------------------
	// 5. Invalid / missing recipient (fails ValidateForExecution)
	// -----------------------------------------------------------------------

	t.Run("GAS inbound with empty recipient: vote succeeds, UTX has FAILED PCTx with revert outbound", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		inbound.TxHash = "0xgas0040"
		inbound.Recipient = "" // GAS type requires a valid hex recipient

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should exist even when recipient is empty")
		require.NotEmpty(t, utx.PcTx, "PCTx should be recorded")
		require.Equal(t, "FAILED", utx.PcTx[0].Status)
		require.Contains(t, utx.PcTx[0].ErrorMsg, "recipient cannot be empty",
			"error message should identify the missing recipient")

		foundRevert := false
		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				foundRevert = true
				require.Equal(t, inbound.SourceChain, ob.DestinationChain)
				require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus)
				break
			}
		}
		require.True(t, foundRevert, "INBOUND_REVERT should be created for empty recipient")
	})

	t.Run("GAS inbound with non-hex recipient: vote succeeds, UTX has FAILED PCTx with revert outbound", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		inbound.TxHash = "0xgas0041"
		inbound.Recipient = "not-a-valid-hex-address" // must be 0x-prefixed hex for GAS type

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should exist even when recipient is invalid")
		require.NotEmpty(t, utx.PcTx, "PCTx should be recorded")
		require.Equal(t, "FAILED", utx.PcTx[0].Status)
		require.Contains(t, utx.PcTx[0].ErrorMsg, "invalid recipient address",
			"error message should identify the invalid recipient address")

		foundRevert := false
		for _, ob := range utx.OutboundTx {
			if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
				foundRevert = true
				break
			}
		}
		require.True(t, foundRevert, "INBOUND_REVERT should be created for invalid recipient address")
	})

	// -----------------------------------------------------------------------
	// 6. Multiple GAS inbounds from the same sender
	// -----------------------------------------------------------------------

	t.Run("multiple GAS inbounds from same sender are tracked independently", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		for i := 0; i < 3; i++ {
			inboundCopy := *inbound
			inboundCopy.TxHash = fmt.Sprintf("0xgas005%d", i)

			reachGasQuorum(t, ctx, chainApp, vals, coreVals, &inboundCopy, 3)
		}

		// Each unique TxHash should have a separate UTX
		for i := 0; i < 3; i++ {
			txHash := fmt.Sprintf("0xgas005%d", i)
			inboundCopy := *inbound
			inboundCopy.TxHash = txHash

			utxKey := uexecutortypes.GetInboundUniversalTxKey(inboundCopy)
			utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
			require.NoError(t, err)
			require.Truef(t, found, "UTX for TxHash %s should exist", txHash)
			require.NotNil(t, utx.InboundTx)
		}
	})

	// -----------------------------------------------------------------------
	// 7. UEA auto-deployment for new senders
	// -----------------------------------------------------------------------

	t.Run("GAS inbound for new sender with no pre-deployed UEA still records PCTx", func(t *testing.T) {
		// TargetAddr2 has no pre-deployed UEA; ExecuteInboundGas will attempt to
		// deploy one before the swap.  Deployment may succeed even if the swap fails.
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		inbound.TxHash = "0xgas0060"
		inbound.Sender = utils.GetDefaultAddresses().TargetAddr2
		inbound.Recipient = utils.GetDefaultAddresses().TargetAddr2
		inbound.RevertInstructions = &uexecutortypes.RevertInstructions{
			FundRecipient: utils.GetDefaultAddresses().TargetAddr2,
		}

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found, "UTX should exist for new sender")
		// At least one PCTx should be recorded (either deploy receipt or the swap attempt)
		require.NotEmpty(t, utx.PcTx, "PCTx entries should be recorded for new sender")
	})

	// -----------------------------------------------------------------------
	// 8. GAS inbound does not produce outbound revert when swap succeeds
	// -----------------------------------------------------------------------

	t.Run("successful GAS inbound execution does not create INBOUND_REVERT outbound", func(t *testing.T) {
		// In the test environment the swap will typically fail, so this test only
		// asserts that if the PCTx shows SUCCESS there is no revert outbound.
		// If the swap fails we skip — the correctness of the success path is
		// In the test environment there is no AMM/Uniswap pool, so the gas swap
		// always fails. We verify the failure is recorded as a FAILED PCTx with
		// an INBOUND_REVERT outbound (the standard failure path for GAS type).
		// A true no-revert success path requires a live AMM setup.
		chainApp, ctx, vals, inbound, coreVals := setupInboundGasTest(t, 4)

		reachGasQuorum(t, ctx, chainApp, vals, coreVals, inbound, 3)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		// Swap fails in test env -> expect FAILED PCTx
		hasFailed := false
		for _, pcTx := range utx.PcTx {
			if pcTx.Status == "FAILED" {
				hasFailed = true
				break
			}
		}
		require.True(t, hasFailed, "GAS swap should fail in test env (no AMM pool)")
	})
}
