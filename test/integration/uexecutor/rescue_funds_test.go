package integrationtest

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// buildRescueFundsLog constructs a synthetic evmtypes.Log that looks exactly like a
// RescueFundsOnSourceChain event emitted by UniversalGatewayPC. This lets integration
// tests drive AttachRescueOutboundFromReceipt without a real on-chain call.
//
// Event: RescueFundsOnSourceChain(bytes32 indexed universalTxId, address indexed prc20,
//
//	string chainNamespace, address indexed sender, uint8 txType,
//	uint256 gasFee, uint256 gasPrice, uint256 gasLimit)
func buildRescueFundsLog(
	t *testing.T,
	utxId string, // UTX key (64-char hex, no 0x prefix)
	prc20Addr common.Address,
	senderAddr common.Address,
	chainNamespace string,
	gasFee, gasPrice, gasLimit *big.Int,
) *evmtypes.Log {
	t.Helper()

	stringType, _ := abi.NewType("string", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: stringType},  // chainNamespace
		{Type: uint8Type},   // txType (RESCUE_FUNDS = 4)
		{Type: uint256Type}, // gasFee
		{Type: uint256Type}, // gasPrice
		{Type: uint256Type}, // gasLimit
	}
	data, err := args.Pack(chainNamespace, uint8(4), gasFee, gasPrice, gasLimit)
	require.NoError(t, err)

	// UTX ID is stored as a bytes32 topic: "0x" + the 64-char hex UTX key.
	utxIdTopic := "0x" + utxId

	gwPCAddr := utils.GetDefaultAddresses().UniversalGatewayPCAddr

	return &evmtypes.Log{
		Address: gwPCAddr.Hex(),
		Topics: []string{
			uexecutortypes.RescueFundsOnSourceChainEventSig,
			utxIdTopic,
			common.BytesToHash(prc20Addr.Bytes()).Hex(),  // indexed prc20
			common.BytesToHash(senderAddr.Bytes()).Hex(), // indexed sender
		},
		Data:    data,
		Removed: false,
	}
}

// setupRescueFundsTest creates a CEA inbound whose deposit will fail (recipient is not
// a registered UEA), drives it to quorum, and returns the UTX key of the failed UTX.
// The returned UTX has at least one FAILED PCTx and is ready for a rescue outbound.
func setupRescueFundsTest(
	t *testing.T,
	numVals int,
) (
	*app.ChainApp,
	sdk.Context,
	[]string, // universalVals
	string,   // utxId of the failed CEA UTX
	[]stakingtypes.Validator,
) {
	t.Helper()

	// Reuse the CEA environment (validators, chain/token config, authz for inbound voting).
	chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAPayloadTest(t, numVals)

	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	// TargetAddr2 is a plain address — not a deployed UEA — so the deposit will fail.
	nonUEARecipient := utils.GetDefaultAddresses().TargetAddr2

	inbound := &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xrescue01",
		Sender:      testAddress,
		Recipient:   nonUEARecipient,
		Amount:      "1000000",
		AssetAddr:   usdcAddress.String(),
		LogIndex:    "1",
		TxType:      uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
		UniversalPayload: &uexecutortypes.UniversalPayload{
			To:                   nonUEARecipient,
			Value:                "1000000",
			Data:                 "0x",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		},
		IsCEA: true,
		RevertInstructions: &uexecutortypes.RevertInstructions{
			FundRecipient: testAddress,
		},
	}

	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		coreValAcc := sdk.AccAddress(valAddr).String()
		err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], coreValAcc, inbound)
		require.NoError(t, err)
	}

	utxId := uexecutortypes.GetInboundUniversalTxKey(*inbound)
	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found, "UTX must exist after quorum")

	require.NotEmpty(t, utx.PcTx, "setup: at least one PCTx must exist")
	require.Equal(t, "FAILED", utx.PcTx[0].Status, "setup: deposit (first PCTx) must fail for non-UEA recipient")

	return chainApp, ctx, vals, utxId, coreVals
}

// makeRescueReceipt wraps a single RescueFundsOnSourceChain log into a receipt.
func makeRescueReceipt(t *testing.T, txHash string, log *evmtypes.Log) *evmtypes.MsgEthereumTxResponse {
	t.Helper()
	return &evmtypes.MsgEthereumTxResponse{
		Hash: txHash,
		Logs: []*evmtypes.Log{log},
	}
}

func TestRescueFunds(t *testing.T) {
	prc20Addr := utils.GetDefaultAddresses().PRC20USDCAddr
	senderAddr := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)

	t.Run("rescue outbound is attached to original UTX on valid CEA inbound with failed deposit", func(t *testing.T) {
		chainApp, ctx, _, utxId, _ := setupRescueFundsTest(t, 4)

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		receipt := makeRescueReceipt(t, "0xrescuetx01", log)
		pcTx := uexecutortypes.PCTx{TxHash: "0xrescuetx01", Status: "SUCCESS"}

		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, receipt, pcTx)
		require.NoError(t, err)

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.True(t, found)

		rescueObs := findRescueOutbound(utx)
		require.NotNil(t, rescueObs, "RESCUE_FUNDS outbound must be attached")
		require.Equal(t, uexecutortypes.Status_PENDING, rescueObs.OutboundStatus)
		require.Equal(t, uexecutortypes.TxType_RESCUE_FUNDS, rescueObs.TxType)
		require.Equal(t, "eip155:11155111", rescueObs.DestinationChain)
		require.Equal(t, "1000000", rescueObs.Amount)
		require.Equal(t, "111", rescueObs.GasFee)

		// The rescue call must be recorded as a PCTx in the UTX history.
		// UTX already had the failed deposit PCTx; the rescue pcTx is appended after it.
		require.Greater(t, len(utx.PcTx), 1, "rescue PCTx must be appended to UTX history")
		lastPcTx := utx.PcTx[len(utx.PcTx)-1]
		require.Equal(t, "0xrescuetx01", lastPcTx.TxHash)
		require.Equal(t, "SUCCESS", lastPcTx.Status)
	})

	t.Run("rescue outbound recipient defaults to inbound sender when no revert instructions", func(t *testing.T) {
		chainApp, ctx, _, utxId, _ := setupRescueFundsTest(t, 4)

		// Remove revert instructions from the stored UTX
		err := chainApp.UexecutorKeeper.UpdateUniversalTx(ctx, utxId, func(utx *uexecutortypes.UniversalTx) error {
			if utx.InboundTx != nil {
				utx.InboundTx.RevertInstructions = nil
			}
			return nil
		})
		require.NoError(t, err)

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx02", log), uexecutortypes.PCTx{TxHash: "0xrescuetx02", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		rescueOb := findRescueOutbound(utx)
		require.NotNil(t, rescueOb)
		// Falls back to original inbound sender
		require.Equal(t, utils.GetDefaultAddresses().DefaultTestAddr, rescueOb.Recipient)
	})

	t.Run("rescue is rejected for non-CEA inbound with no reverted auto-revert", func(t *testing.T) {
		// Non-CEA FUNDS inbound: minting succeeds, so no INBOUND_REVERT outbound exists.
		// Rescue must be rejected because the auto-revert has not been attempted and reverted.
		chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), inbound)
			require.NoError(t, err)
		}
		utxId := uexecutortypes.GetInboundUniversalTxKey(*inbound)

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx03", log), uexecutortypes.PCTx{TxHash: "0xrescuetx03", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no reverted inbound-revert outbound")
	})

	t.Run("rescue is rejected for non-CEA inbound when auto-revert is PENDING", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), inbound)
			require.NoError(t, err)
		}
		utxId := uexecutortypes.GetInboundUniversalTxKey(*inbound)

		// Manually inject a PENDING INBOUND_REVERT outbound.
		err := chainApp.UexecutorKeeper.UpdateUniversalTx(ctx, utxId, func(utx *uexecutortypes.UniversalTx) error {
			utx.OutboundTx = append(utx.OutboundTx, &uexecutortypes.OutboundTx{
				Id:             "pending-revert-id",
				TxType:         uexecutortypes.TxType_INBOUND_REVERT,
				OutboundStatus: uexecutortypes.Status_PENDING,
			})
			return nil
		})
		require.NoError(t, err)

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx03b", log), uexecutortypes.PCTx{TxHash: "0xrescuetx03b", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no reverted inbound-revert outbound")
	})

	t.Run("rescue succeeds for non-CEA inbound with reverted auto-revert", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals := setupInboundBridgeTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), inbound)
			require.NoError(t, err)
		}
		utxId := uexecutortypes.GetInboundUniversalTxKey(*inbound)

		// Inject a REVERTED INBOUND_REVERT outbound to simulate a failed auto-revert.
		err := chainApp.UexecutorKeeper.UpdateUniversalTx(ctx, utxId, func(utx *uexecutortypes.UniversalTx) error {
			utx.OutboundTx = append(utx.OutboundTx, &uexecutortypes.OutboundTx{
				Id:             "reverted-revert-id",
				TxType:         uexecutortypes.TxType_INBOUND_REVERT,
				OutboundStatus: uexecutortypes.Status_REVERTED,
			})
			return nil
		})
		require.NoError(t, err)

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(222), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx03c", log), uexecutortypes.PCTx{TxHash: "0xrescuetx03c", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		rescueOb := findRescueOutbound(utx)
		require.NotNil(t, rescueOb, "RESCUE_FUNDS outbound must be attached for non-CEA with reverted auto-revert")
		require.Equal(t, uexecutortypes.Status_PENDING, rescueOb.OutboundStatus)
		require.Equal(t, uexecutortypes.TxType_RESCUE_FUNDS, rescueOb.TxType)
		require.Equal(t, "eip155:11155111", rescueOb.DestinationChain)
		require.Equal(t, "222", rescueOb.GasFee)
	})

	t.Run("rescue is rejected when deposit did not fail", func(t *testing.T) {
		// CEA inbound with valid UEA recipient: deposit succeeds (first PCTx = SUCCESS).
		// Even if the payload execution later fails, rescue must be rejected because
		// the funds were already minted onto Push Chain by the successful deposit.
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundCEAPayloadTest(t, 4)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			err = utils.ExecVoteInbound(t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), inbound)
			require.NoError(t, err)
		}
		utxId := uexecutortypes.GetInboundUniversalTxKey(*inbound)

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.True(t, found)
		require.NotEmpty(t, utx.PcTx)
		// Confirm first PCTx (deposit) succeeded — that's the invariant we rely on.
		require.Equal(t, "SUCCESS", utx.PcTx[0].Status, "deposit must have succeeded for this test to be meaningful")

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx04", log), uexecutortypes.PCTx{TxHash: "0xrescuetx04", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "deposit did not fail")
	})

	t.Run("second rescue is rejected when first is PENDING", func(t *testing.T) {
		chainApp, ctx, _, utxId, _ := setupRescueFundsTest(t, 4)

		// First rescue — succeeds
		log1 := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx05a", log1), uexecutortypes.PCTx{TxHash: "0xrescuetx05a", Status: "SUCCESS"})
		require.NoError(t, err)

		// Second rescue — rejected because first is PENDING
		log2 := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx05b", log2), uexecutortypes.PCTx{TxHash: "0xrescuetx05b", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already has an active rescue outbound")
	})

	t.Run("second rescue is rejected when first is OBSERVED", func(t *testing.T) {
		chainApp, ctx, vals, utxId, coreVals := setupRescueFundsTest(t, 4)

		// Grant authz for outbound voting
		for i, val := range coreVals {
			accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(accAddr)
			uniAcc := sdk.MustAccAddressFromBech32(vals[i])
			auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteOutbound{}))
			exp := ctx.BlockTime().Add(time.Hour)
			err = chainApp.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp)
			require.NoError(t, err)
		}

		// Attach first rescue outbound
		log1 := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx06a", log1), uexecutortypes.PCTx{TxHash: "0xrescuetx06a", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		rescueOb := findRescueOutbound(utx)
		require.NotNil(t, rescueOb)

		// Vote to reach quorum with success → status becomes OBSERVED
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteOutbound(t, ctx, chainApp, vals[i], coreAcc, utxId, rescueOb, true, "", rescueOb.GasFee)
			require.NoError(t, err)
		}

		utx, _, err = chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.Equal(t, uexecutortypes.Status_OBSERVED, findRescueOutbound(utx).OutboundStatus)

		// Second rescue rejected because first is OBSERVED
		log2 := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx06b", log2), uexecutortypes.PCTx{TxHash: "0xrescuetx06b", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already has an active rescue outbound")
	})

	t.Run("rescue can be retried after previous rescue is REVERTED", func(t *testing.T) {
		chainApp, ctx, vals, utxId, coreVals := setupRescueFundsTest(t, 4)

		// Grant authz for outbound voting
		for i, val := range coreVals {
			accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(accAddr)
			uniAcc := sdk.MustAccAddressFromBech32(vals[i])
			auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteOutbound{}))
			exp := ctx.BlockTime().Add(time.Hour)
			err = chainApp.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp)
			require.NoError(t, err)
		}

		// First rescue outbound
		log1 := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx07a", log1), uexecutortypes.PCTx{TxHash: "0xrescuetx07a", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		rescueOb := findRescueOutbound(utx)
		require.NotNil(t, rescueOb)

		// Vote to reach quorum with FAILURE → status becomes REVERTED
		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteOutbound(t, ctx, chainApp, vals[i], coreAcc, utxId, rescueOb, false, "rescue failed", rescueOb.GasFee)
			require.NoError(t, err)
		}

		utx, _, err = chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.Equal(t, uexecutortypes.Status_REVERTED, findRescueOutbound(utx).OutboundStatus)

		// Second rescue is now allowed since the first is REVERTED
		log2 := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx07b", log2), uexecutortypes.PCTx{TxHash: "0xrescuetx07b", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err = chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		// Two rescue outbounds exist: first REVERTED, second PENDING
		var rescueObs []*uexecutortypes.OutboundTx
		for _, ob := range utx.OutboundTx {
			if ob != nil && ob.TxType == uexecutortypes.TxType_RESCUE_FUNDS {
				rescueObs = append(rescueObs, ob)
			}
		}
		require.Len(t, rescueObs, 2, "two rescue outbounds expected after retry")
		require.Equal(t, uexecutortypes.Status_REVERTED, rescueObs[0].OutboundStatus)
		require.Equal(t, uexecutortypes.Status_PENDING, rescueObs[1].OutboundStatus)
	})

	t.Run("rescue outbound finalizes to OBSERVED after quorum success votes", func(t *testing.T) {
		chainApp, ctx, vals, utxId, coreVals := setupRescueFundsTest(t, 4)

		// Grant authz for outbound voting
		for i, val := range coreVals {
			accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(accAddr)
			uniAcc := sdk.MustAccAddressFromBech32(vals[i])
			auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteOutbound{}))
			exp := ctx.BlockTime().Add(time.Hour)
			err = chainApp.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp)
			require.NoError(t, err)
		}

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx08", log), uexecutortypes.PCTx{TxHash: "0xrescuetx08", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		rescueOb := findRescueOutbound(utx)
		require.NotNil(t, rescueOb)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteOutbound(t, ctx, chainApp, vals[i], coreAcc, utxId, rescueOb, true, "", rescueOb.GasFee)
			require.NoError(t, err)
		}

		utx, _, err = chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := findRescueOutbound(utx)
		require.NotNil(t, ob)
		require.Equal(t, uexecutortypes.Status_OBSERVED, ob.OutboundStatus)
		require.NotNil(t, ob.ObservedTx)
		require.True(t, ob.ObservedTx.Success)
		// No PC revert expected for RESCUE_FUNDS on success
		require.Nil(t, ob.PcRevertExecution)
	})

	t.Run("failed rescue outbound marks REVERTED with no PC-side revert", func(t *testing.T) {
		chainApp, ctx, vals, utxId, coreVals := setupRescueFundsTest(t, 4)

		// Grant authz for outbound voting
		for i, val := range coreVals {
			accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(accAddr)
			uniAcc := sdk.MustAccAddressFromBech32(vals[i])
			auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteOutbound{}))
			exp := ctx.BlockTime().Add(time.Hour)
			err = chainApp.AuthzKeeper.SaveGrant(ctx, uniAcc, coreAcc, auth, &exp)
			require.NoError(t, err)
		}

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx09", log), uexecutortypes.PCTx{TxHash: "0xrescuetx09", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		rescueOb := findRescueOutbound(utx)
		require.NotNil(t, rescueOb)

		for i := 0; i < 3; i++ {
			valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
			require.NoError(t, err)
			coreAcc := sdk.AccAddress(valAddr).String()
			err = utils.ExecVoteOutbound(t, ctx, chainApp, vals[i], coreAcc, utxId, rescueOb, false, "rescue tx reverted", rescueOb.GasFee)
			require.NoError(t, err)
		}

		utx, _, err = chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)

		ob := findRescueOutbound(utx)
		require.NotNil(t, ob)
		require.Equal(t, uexecutortypes.Status_REVERTED, ob.OutboundStatus)
		// RESCUE_FUNDS failure must NOT trigger a PC-side revert (no funds locked on PC).
		require.Nil(t, ob.PcRevertExecution, "no PC revert expected for a failed rescue outbound")
	})

	t.Run("rescue outbound ID is deterministic from push chain caip, pc tx hash and log index", func(t *testing.T) {
		pushChainCaip := "eip155:2240"
		pcTxHash := "0xrescuetx10"
		logIndex := "0"
		id1 := uexecutortypes.GetRescueFundsOutboundId(pushChainCaip, pcTxHash, logIndex)
		id2 := uexecutortypes.GetRescueFundsOutboundId(pushChainCaip, pcTxHash, logIndex)
		require.Equal(t, id1, id2, "ID must be deterministic")
		require.Len(t, id1, 64, "ID must be a 32-byte hex string")

		// Different inputs produce different IDs
		id3 := uexecutortypes.GetRescueFundsOutboundId(pushChainCaip, "0xother", logIndex)
		require.NotEqual(t, id1, id3)

		// Different push chain caips produce different IDs
		id4 := uexecutortypes.GetRescueFundsOutboundId("eip155:9999", pcTxHash, logIndex)
		require.NotEqual(t, id1, id4)
	})

	t.Run("rescue log from wrong contract address is ignored", func(t *testing.T) {
		chainApp, ctx, _, utxId, _ := setupRescueFundsTest(t, 4)

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		// Override address to a random contract — not UNIVERSAL_GATEWAY_PC
		log.Address = "0x000000000000000000000000000000000000dead"

		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx,
			makeRescueReceipt(t, "0xrescuetx11", log),
			uexecutortypes.PCTx{TxHash: "0xrescuetx11", Status: "SUCCESS"})
		require.NoError(t, err) // silently ignored — no rescue outbound created

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.Nil(t, findRescueOutbound(utx), "wrong-contract log must be ignored")
	})

	t.Run("rescue with unknown universalTxId returns error", func(t *testing.T) {
		chainApp, ctx, _, _, _ := setupRescueFundsTest(t, 4)

		unknownId := hex.EncodeToString(make([]byte, 32)) // 64 zero chars
		log := buildRescueFundsLog(t, unknownId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))

		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx,
			makeRescueReceipt(t, "0xrescuetx12", log),
			uexecutortypes.PCTx{TxHash: "0xrescuetx12", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}

// findRescueOutbound returns the first RESCUE_FUNDS outbound from a UTX, or nil.
func findRescueOutbound(utx uexecutortypes.UniversalTx) *uexecutortypes.OutboundTx {
	for _, ob := range utx.OutboundTx {
		if ob != nil && ob.TxType == uexecutortypes.TxType_RESCUE_FUNDS {
			return ob
		}
	}
	return nil
}

// Ensure the fmt import is used.
var _ = fmt.Sprintf
