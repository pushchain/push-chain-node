package integrationtest

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"
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
	chainutils "github.com/pushchain/push-chain-node/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
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

// Asset of the stuck deposit built by setupRescueFundsTest: a pETH-style 18-decimal
// token, registered in uregistry but whose PRC20 has no deployed contract — so the
// deposit fails while the asset still resolves through the registry.
//
// The 18-decimal choice is deliberate: the default token registered by the CEA setup is
// 6-decimal USDC, so the two form the 18 → 6 pair where a cross-asset rescue amplifies.
// 1e18 raw of an 18-decimal token is one whole token; reinterpreted as 6-decimal USDC the
// same raw integer is a claim on 10^12 whole USDC.
var (
	rescueOriginalAsset  = common.HexToAddress("0x000000000000000000000000000000000000DEAD")
	rescueOriginalPRC20  = common.HexToAddress("0x0000000000000000000000000000000000000eE1")
	rescueOriginalAmount = "1000000000000000000" // 1e18 raw = 1 whole 18-decimal token
)

// setupRescueFundsTest creates a CEA inbound whose deposit will fail (its PRC20 has no
// deployed contract), drives it to quorum, and returns the UTX key of the failed UTX.
// The returned UTX has at least one FAILED PCTx and is ready for a rescue outbound.
func setupRescueFundsTest(
	t *testing.T,
	numVals int,
) (
	*app.ChainApp,
	sdk.Context,
	[]string, // universalVals
	string, // utxId of the failed CEA UTX
	[]stakingtypes.Validator,
) {
	t.Helper()

	// Reuse the CEA environment (validators, chain/token config, authz for inbound voting).
	chainApp, ctx, vals, _, coreVals, _ := setupInboundCEAPayloadTest(t, numVals)

	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	recipient := utils.GetDefaultAddresses().TargetAddr2

	// Register the stuck asset. A rescue derives its asset from the original inbound, so
	// the original asset must resolve; the deposit still fails because rescueOriginalPRC20
	// has no contract deployed at it, leaving the funds stuck on the source chain — which
	// is exactly the situation rescue exists for.
	require.NoError(t, chainApp.UregistryKeeper.AddTokenConfig(ctx, &uregistrytypes.TokenConfig{
		Chain:        "eip155:11155111",
		Address:      rescueOriginalAsset.String(),
		Name:         "Push Ether",
		Symbol:       "pETH",
		Decimals:     18,
		Enabled:      true,
		LiquidityCap: "1000000000000000000000000",
		TokenType:    1,
		NativeRepresentation: &uregistrytypes.NativeRepresentation{
			ContractAddress: rescueOriginalPRC20.String(),
		},
	}))

	inbound := &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0xrescue01",
		Sender:      testAddress,
		Recipient:   recipient,
		Amount:      rescueOriginalAmount,
		AssetAddr:   rescueOriginalAsset.String(),
		LogIndex:    "1",
		TxType:      uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
		UniversalPayload: &uexecutortypes.UniversalPayload{
			To:                   recipient,
			Value:                rescueOriginalAmount,
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
	require.Equal(t, "FAILED", utx.PcTx[0].Status, "setup: deposit must fail so the funds stay stuck")

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
	// A rescue event must name the PRC20 registered for the stuck inbound's OWN asset, so
	// each subtest uses the PRC20 belonging to whichever setup it built its inbound from:
	// setupRescueFundsTest stakes the 18-decimal pETH, the bridge/CEA-payload setups the
	// 6-decimal USDC.
	prc20Addr := rescueOriginalPRC20
	usdcPRC20Addr := utils.GetDefaultAddresses().PRC20USDCAddr
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
		require.Equal(t, rescueOriginalAmount, rescueObs.Amount)
		require.Equal(t, "111", rescueObs.GasFee)

		// The asset is the original inbound's, derived from the registry — never the
		// caller's. Amount and asset must describe the same deposit.
		require.Equal(t, rescueOriginalAsset.String(), rescueObs.ExternalAssetAddr,
			"rescue must carry the original inbound's external asset")
		require.Equal(t, rescueOriginalPRC20.String(), rescueObs.Prc20AssetAddr,
			"rescue must carry the PRC20 registered for the original asset")

		// The rescue call must be recorded as a PCTx in the UTX history.
		// UTX already had the failed deposit PCTx; the rescue pcTx is appended after it.
		require.Greater(t, len(utx.PcTx), 1, "rescue PCTx must be appended to UTX history")
		lastPcTx := utx.PcTx[len(utx.PcTx)-1]
		require.Equal(t, "0xrescuetx01", lastPcTx.TxHash)
		require.Equal(t, "SUCCESS", lastPcTx.Status)
	})

	// --- F-2026-18177: the rescue asset is derived, never supplied ----------------

	t.Run("rescue naming a different registered PRC20 than the original asset is rejected", func(t *testing.T) {
		// The headline case. The stuck deposit is 1e18 raw of an 18-decimal token; the
		// rescue event names 6-decimal USDC, which is registered on the same chain and so
		// passes every "is this a real PRC20" check. The amount always comes from the
		// original inbound, so honouring the caller's PRC20 would emit an outbound paying
		// 1e18 base units of USDC — 10^12 whole USDC — for a stuck deposit of one pETH.
		chainApp, ctx, _, utxId, _ := setupRescueFundsTest(t, 4)

		pendingBefore := pendingOutboundIds(t, ctx, chainApp)

		log := buildRescueFundsLog(t, utxId, usdcPRC20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx,
			makeRescueReceipt(t, "0xrescuetx13", log),
			uexecutortypes.PCTx{TxHash: "0xrescuetx13", Status: "SUCCESS"})

		// State is asserted before the error: if the derivation regresses, the call
		// succeeds and the substituted outbound shows up here, rather than the subtest
		// aborting on the require.Error below and never reaching these checks.
		utx, found, getErr := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, getErr)
		require.True(t, found)
		require.Nil(t, findRescueOutbound(utx),
			"no rescue outbound may be created when the event names another asset")
		require.Equal(t, pendingBefore, pendingOutboundIds(t, ctx, chainApp),
			"a rejected cross-asset rescue must not add a PendingOutbounds row")

		require.Error(t, err)
		require.Contains(t, err.Error(), "does not match")
	})

	t.Run("rescue for an original asset with no registered token config is rejected", func(t *testing.T) {
		chainApp, ctx, _, utxId, _ := setupRescueFundsTest(t, 4)

		// Point the stored inbound at an asset uregistry knows nothing about. An
		// unregistered original asset must be an error, not an opening to substitute
		// whichever asset the caller happens to name.
		unregistered := common.HexToAddress("0x000000000000000000000000000000000000BEEF")
		require.NoError(t, chainApp.UexecutorKeeper.UpdateUniversalTx(ctx, utxId,
			func(utx *uexecutortypes.UniversalTx) error {
				utx.InboundTx.AssetAddr = unregistered.String()
				return nil
			}))

		pendingBefore := pendingOutboundIds(t, ctx, chainApp)

		log := buildRescueFundsLog(t, utxId, prc20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx,
			makeRescueReceipt(t, "0xrescuetx14", log),
			uexecutortypes.PCTx{TxHash: "0xrescuetx14", Status: "SUCCESS"})

		utx, _, getErr := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, getErr)
		require.Nil(t, findRescueOutbound(utx),
			"no rescue outbound may be created for an unregistered original asset")
		require.Equal(t, pendingBefore, pendingOutboundIds(t, ctx, chainApp),
			"a rejected rescue must not add a PendingOutbounds row")

		require.Error(t, err)
		require.Contains(t, err.Error(), "no token config registered for original asset")
	})

	t.Run("rescue PRC20 comparison ignores address casing", func(t *testing.T) {
		// The event's PRC20 is always EIP-55 checksummed by the log decoder, while the
		// registry stores whatever an admin registered. Comparing canonically means a
		// lowercase registry entry is still the same PRC20.
		chainApp, ctx, _, utxId, _ := setupRescueFundsTest(t, 4)

		require.NoError(t, chainApp.UregistryKeeper.UpdateTokenConfig(ctx, &uregistrytypes.TokenConfig{
			Chain:        "eip155:11155111",
			Address:      rescueOriginalAsset.String(),
			Name:         "Push Ether",
			Symbol:       "pETH",
			Decimals:     18,
			Enabled:      true,
			LiquidityCap: "1000000000000000000000000",
			TokenType:    1,
			NativeRepresentation: &uregistrytypes.NativeRepresentation{
				ContractAddress: strings.ToLower(rescueOriginalPRC20.String()),
			},
		}))

		log := buildRescueFundsLog(t, utxId, rescueOriginalPRC20, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx,
			makeRescueReceipt(t, "0xrescuetx15", log),
			uexecutortypes.PCTx{TxHash: "0xrescuetx15", Status: "SUCCESS"})
		require.NoError(t, err)

		utx, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		rescueOb := findRescueOutbound(utx)
		require.NotNil(t, rescueOb)
		require.Equal(t, strings.ToLower(rescueOriginalPRC20.String()), rescueOb.Prc20AssetAddr,
			"the registry's spelling of the PRC20 is what lands on the outbound")
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
		require.Equal(t, chainutils.LenientCanonicalizeEVMAddress(utils.GetDefaultAddresses().DefaultTestAddr), rescueOb.Recipient)
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

		log := buildRescueFundsLog(t, utxId, usdcPRC20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx03", log), uexecutortypes.PCTx{TxHash: "0xrescuetx03", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no reverted or aborted inbound-revert outbound")
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

		log := buildRescueFundsLog(t, utxId, usdcPRC20Addr, senderAddr,
			"eip155", big.NewInt(111), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err = chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(ctx, makeRescueReceipt(t, "0xrescuetx03b", log), uexecutortypes.PCTx{TxHash: "0xrescuetx03b", Status: "SUCCESS"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no reverted or aborted inbound-revert outbound")
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

		log := buildRescueFundsLog(t, utxId, usdcPRC20Addr, senderAddr,
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

		log := buildRescueFundsLog(t, utxId, usdcPRC20Addr, senderAddr,
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

// pendingOutboundIds returns the sorted outbound IDs currently in the PendingOutbounds
// index, so a test can assert that a rejected rescue left the index untouched.
func pendingOutboundIds(t *testing.T, ctx sdk.Context, chainApp *app.ChainApp) []string {
	t.Helper()
	querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
	resp, err := querier.AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		ids = append(ids, e.OutboundId)
	}
	sort.Strings(ids)
	return ids
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

// TestRescueFunds_PC20 covers the rescue escape hatch for a stuck PC20 return.
// A PC20 return burns the wrapper on the external chain; if the on-Push unlock
// fails and the auto INBOUND_REVERT (wrapper re-mint) also fails (REVERTED), the
// funds are stuck and rescue re-attempts the re-mint. The chain must build the
// rescue outbound carrying the WRAPPER as its token (the external Vault.rescueFunds
// detects a PC20 wrapper and re-mints it), WITHOUT the PRC20 token-config lookup
// that the PRC20 path uses — that lookup would fail for a wrapper and, before this
// branch, fatally reverted the whole tx.
func TestRescueFunds_PC20(t *testing.T) {
	t.Run("PC20 return rescue builds with the wrapper as token and skips the PRC20 lookup", func(t *testing.T) {
		chainApp, ctx, _, _, _ := setupInboundBridgeTest(t, 4)

		// A PC20 wrapper that is deliberately NOT registered as a PRC20 token — the
		// PC20 rescue must not need a token config for it.
		wrapper := common.HexToAddress("0x000000000000000000000000000000000000FACE")
		sender := common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)

		inbound := &uexecutortypes.Inbound{
			SourceChain:        "eip155:11155111",
			TxHash:             "0xpc20rescue01",
			Sender:             sender.Hex(),
			Amount:             "1000000",
			AssetAddr:          wrapper.Hex(), // the external wrapper
			LogIndex:           "1",
			TxType:             uexecutortypes.TxType_FUNDS_AND_PAYLOAD,
			IsPc20:             true,
			IsCEA:              false,
			RevertInstructions: &uexecutortypes.RevertInstructions{FundRecipient: sender.Hex()},
		}
		utxId := uexecutortypes.GetInboundUniversalTxKey(*inbound)

		// Stuck state: the auto INBOUND_REVERT (re-mint) reached REVERTED, which makes
		// a non-CEA inbound eligible for rescue.
		utx := uexecutortypes.UniversalTx{
			Id:        utxId,
			InboundTx: inbound,
			OutboundTx: []*uexecutortypes.OutboundTx{
				{
					Id:             "pc20-reverted-revert",
					TxType:         uexecutortypes.TxType_INBOUND_REVERT,
					OutboundStatus: uexecutortypes.Status_REVERTED,
				},
			},
		}
		require.NoError(t, chainApp.UexecutorKeeper.CreateUniversalTx(ctx, utxId, utx))

		// The chain detects PC20 via the original inbound's is_pc20, not the rescue
		// event, so the event's indexed address is irrelevant here (pass the wrapper).
		log := buildRescueFundsLog(t, utxId, wrapper, sender,
			"eip155", big.NewInt(333), big.NewInt(1_000_000_000), big.NewInt(200_000))
		err := chainApp.UexecutorKeeper.AttachRescueOutboundFromReceipt(
			ctx, makeRescueReceipt(t, "0xpc20rescuetx01", log),
			uexecutortypes.PCTx{TxHash: "0xpc20rescuetx01", Status: "SUCCESS"},
		)
		require.NoError(t, err, "PC20 rescue must not fatally fail on the missing PRC20 token config")

		got, _, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		rescueOb := findRescueOutbound(got)
		require.NotNil(t, rescueOb, "RESCUE_FUNDS outbound must be attached")
		require.Equal(t, uexecutortypes.TxType_RESCUE_FUNDS, rescueOb.TxType)
		require.Equal(t, uexecutortypes.Status_PENDING, rescueOb.OutboundStatus)
		require.Equal(t, wrapper.Hex(), rescueOb.ExternalAssetAddr, "rescue carries the wrapper as its token (Vault re-mints it)")
		require.Empty(t, rescueOb.Prc20AssetAddr, "a PC20 rescue has no PRC20 asset")
		require.False(t, rescueOb.IsPc20, "rescue outbound is not flagged is_pc20 — the Vault detects PC20 from the token")
		require.Equal(t, "eip155:11155111", rescueOb.DestinationChain)
		require.Equal(t, "333", rescueOb.GasFee)
	})
}
