package integrationtest

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// F-2026-18195 (defect 2). On the isCEA smart-contract branch the callback may
// itself call UniversalGatewayPC: that burns the PRC20 and emits a
// UniversalTxOutbound log. DerivedEVMCall skips PostTxProcessing, so the EVM
// hook never sees those logs — the handler itself has to attach them. Before
// the fix it returned right after recording the PcTx, leaving burned supply
// with no OutboundTx and no PendingOutbounds row (and a SUCCESS PcTx, so
// neither rescue nor remint were eligible).
//
// The attach now runs inside the same CacheContext as the callback, before
// writeCache(), so the burn and the outbound rows commit together or not at all.

// gatewayCallingRecipientAddr hosts the mock recipient that re-enters
// UniversalGatewayPC during its callback. 0xD0-0xFF is outside the reserved
// system-contract ranges (see x/uregistry/types/constants.go).
var gatewayCallingRecipientAddr = common.HexToAddress("0x00000000000000000000000000000000000000D5")

// gatewayWithdrawCalldata is the ABI-encoded UniversalGatewayPC withdraw call
// used by TestInboundInitiatedOutbound — recipient 0x1234..5678, PRC20 0x..0e06,
// amount 1000000. The test gateway answers it by emitting UniversalTxOutbound.
const gatewayWithdrawCalldata = "b3ca1fbc" +
	"0000000000000000000000000000000000000000000000000000000000000020" +
	"00000000000000000000000000000000000000000000000000000000000000c0" +
	"0000000000000000000000000000000000000000000000000000000000000e06" +
	"00000000000000000000000000000000000000000000000000000000000f4240" +
	"000000000000000000000000000000000000000000000000000000000007a120" +
	"0000000000000000000000000000000000000000000000000000000000000100" +
	"0000000000000000000000001234567890abcdef1234567890abcdef12345678" +
	"0000000000000000000000000000000000000000000000000000000000000014" +
	"1234567890abcdef1234567890abcdef12345678000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000"

// expectedOutboundRecipient / expectedOutboundPRC20 mirror gatewayWithdrawCalldata.
const (
	expectedOutboundRecipient = "0x1234567890abcdef1234567890abcdef12345678"
	expectedOutboundPRC20     = "0x0000000000000000000000000000000000000e06"
	expectedOutboundAmount    = "1000000"
)

// gatewayCallingRecipientCode assembles runtime bytecode for a recipient that,
// on any call:
//
//  1. SSTOREs 1 into slot 0 — a witness that the callback body ran AND committed;
//  2. CALLs UniversalGatewayPC (0x..C1) with gatewayWithdrawCalldata, so the
//     callback emits a UniversalTxOutbound log from the gateway address;
//  3. bubbles a gateway failure up as a REVERT.
//
// Assembly (all self-references are computed, not hard-coded):
//
//	PUSH1 0x01; PUSH1 0x00; SSTORE                     storage[0] = 1
//	PUSH2 len; PUSH2 off; PUSH1 0x00; CODECOPY         mem[0:len] = code[off:off+len]
//	PUSH1 0x00; PUSH1 0x00                             retSize, retOffset
//	PUSH2 len; PUSH1 0x00                              argsSize, argsOffset
//	PUSH1 0x00; PUSH1 0xC1; GAS; CALL                  value, gateway, gas
//	PUSH1 ok; JUMPI                                    taken when CALL succeeded
//	PUSH1 0x00; PUSH1 0x00; REVERT                     gateway call failed
//	JUMPDEST; STOP
//	<gatewayWithdrawCalldata>
func gatewayCallingRecipientCode(t *testing.T) string {
	t.Helper()

	blobLen := len(gatewayWithdrawCalldata) / 2

	// The prologue below is a fixed 39 bytes; the blob is appended right after
	// it, and the success JUMPDEST is its second-to-last byte.
	const prologueLen = 39
	const okJumpDest = prologueLen - 2

	prologue := "6001600055" + // PUSH1 1, PUSH1 0, SSTORE
		fmt.Sprintf("61%04x", blobLen) + // PUSH2 blobLen  (CODECOPY size)
		fmt.Sprintf("61%04x", prologueLen) + // PUSH2 prologueLen (CODECOPY code offset)
		"6000" + // PUSH1 0 (CODECOPY dest offset)
		"39" + // CODECOPY
		"6000" + // PUSH1 0 retSize
		"6000" + // PUSH1 0 retOffset
		fmt.Sprintf("61%04x", blobLen) + // PUSH2 blobLen argsSize
		"6000" + // PUSH1 0 argsOffset
		"6000" + // PUSH1 0 value
		"60c1" + // PUSH1 0xC1 UniversalGatewayPC
		"5a" + // GAS
		"f1" + // CALL
		fmt.Sprintf("60%02x", okJumpDest) + // PUSH1 okJumpDest
		"57" + // JUMPI
		"6000" + // PUSH1 0 revert offset
		"6000" + // PUSH1 0 revert size
		"fd" + // REVERT
		"5b" + // JUMPDEST (okJumpDest)
		"00" // STOP

	require.Equal(t, prologueLen, len(prologue)/2, "prologue length drifted; okJumpDest/CODECOPY offset are stale")

	return prologue + gatewayWithdrawCalldata
}

// deployGatewayCallingRecipient installs the contract above and funds it with
// upc so DeductGasFeesFromReceipt succeeds and execution reaches the attach.
func deployGatewayCallingRecipient(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context) common.Address {
	t.Helper()

	addr := utils.DeployContract(t, chainApp, ctx, gatewayCallingRecipientAddr, gatewayCallingRecipientCode(t))

	fundCoins := sdk.NewCoins(sdk.NewInt64Coin("upc", 1_000_000_000))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx, utils.MintModule, sdk.AccAddress(addr.Bytes()), fundCoins))

	return addr
}

// setupCEAContractOutboundTest mirrors setupInboundCEASmartContractTest but the
// recipient re-enters the gateway, and chain outbound can be disabled to force
// the attach to fail.
func setupCEAContractOutboundTest(
	t *testing.T,
	numVals int,
	outboundEnabled bool,
) (*app.ChainApp, sdk.Context, []string, []stakingtypes.Validator, common.Address) {
	t.Helper()

	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
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

	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
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

	universalVals := make([]string, len(validators))
	for i, val := range validators {
		network := uvalidatortypes.NetworkInfo{PeerId: fmt.Sprintf("temp%d", i+1), MultiAddrs: []string{"temp"}}
		require.NoError(t, chainApp.UvalidatorKeeper.AddUniversalValidator(ctx, val.OperatorAddress, network))
		universalVals[i] = sdk.AccAddress([]byte(fmt.Sprintf("universal-validator-%d", i))).String()
	}

	for i, val := range validators {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)

		coreValAddr := sdk.AccAddress(accAddr)
		uniValAddr := sdk.MustAccAddressFromBech32(universalVals[i])

		auth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteInbound{}))
		exp := ctx.BlockTime().Add(time.Hour)
		require.NoError(t, chainApp.AuthzKeeper.SaveGrant(ctx, uniValAddr, coreValAddr, auth, &exp))
	}

	recipient := deployGatewayCallingRecipient(t, chainApp, ctx)

	return chainApp, ctx, universalVals, validators, recipient
}

// ceaContractInbound builds an isCEA inbound targeting a contract recipient.
func ceaContractInbound(
	txHash string,
	recipient common.Address,
	txType uexecutortypes.TxType,
	amount string,
) *uexecutortypes.Inbound {
	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

	return &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      txHash,
		Sender:      testAddress,
		Recipient:   recipient.String(),
		Amount:      amount,
		AssetAddr:   usdcAddress.String(),
		LogIndex:    "1",
		TxType:      txType,
		UniversalPayload: &uexecutortypes.UniversalPayload{
			To:                   recipient.String(),
			Value:                "0",
			Data:                 "0xdeadbeef",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		},
		VerificationData: "",
		IsCEA:            true,
		RevertInstructions: &uexecutortypes.RevertInstructions{
			FundRecipient: testAddress,
		},
	}
}

func reachInboundQuorum(
	t *testing.T,
	ctx sdk.Context,
	chainApp *app.ChainApp,
	universalVals []string,
	coreVals []stakingtypes.Validator,
	inbound *uexecutortypes.Inbound,
) {
	t.Helper()

	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteInbound(t, ctx, chainApp, universalVals[i], sdk.AccAddress(valAddr).String(), inbound))
	}
}

// lastPcTx returns the executeUniversalTx PcTx, which is always the final one
// recorded on the smart-contract branch.
func lastPcTx(t *testing.T, utx uexecutortypes.UniversalTx) *uexecutortypes.PCTx {
	t.Helper()
	require.NotEmpty(t, utx.PcTx, "at least one PcTx must be recorded")
	return utx.PcTx[len(utx.PcTx)-1]
}

func TestInboundCEAContractCallbackOutbound(t *testing.T) {
	slot := common.Hash{}

	// --- FUNDS_AND_PAYLOAD: the defect and its fix -------------------------

	t.Run("FUNDS_AND_PAYLOAD contract callback gateway burn creates OutboundTx and PendingOutbounds", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, recipient := setupCEAContractOutboundTest(t, 4, true)

		inbound := ceaContractInbound("0xsc-outbound-funds-01", recipient, uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000")
		reachInboundQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		callPcTx := lastPcTx(t, utx)
		require.Equal(t, "SUCCESS", callPcTx.Status, "executeUniversalTx should succeed: %s", callPcTx.ErrorMsg)

		// The callback committed (proves writeCache ran).
		require.Equal(t, common.BigToHash(common.Big1), chainApp.EVMKeeper.GetState(ctx, recipient, slot),
			"recipient slot 0 must be 1 (callback committed)")

		// THE PRIMARY ASSERTION: the nested gateway burn produced an outbound.
		require.Len(t, utx.OutboundTx, 1, "the gateway call inside the callback must produce exactly one OutboundTx")

		out := utx.OutboundTx[0]
		require.Equal(t, "eip155:11155111", out.DestinationChain)
		require.Equal(t, expectedOutboundRecipient, out.Recipient)
		require.Equal(t, expectedOutboundAmount, out.Amount)
		require.Equal(t, expectedOutboundPRC20, out.Prc20AssetAddr)
		require.Equal(t, uexecutortypes.Status_PENDING, out.OutboundStatus)
		require.NotEqual(t, uexecutortypes.TxType_INBOUND_REVERT, out.TxType,
			"the isCEA route must never auto-revert; this outbound comes from the callback")

		// ... and a PendingOutbounds row, so it is actually signed and delivered.
		entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, out.Id)
		require.NoError(t, err, "outbound must be indexed in PendingOutbounds")
		require.Equal(t, out.Id, entry.OutboundId)
		require.Equal(t, utxKey, entry.UniversalTxId)
	})

	t.Run("FUNDS_AND_PAYLOAD attach failure rolls the callback back and records FAILED PcTx", func(t *testing.T) {
		// Outbound disabled for the destination chain → BuildOutboundsFromReceipt
		// errors, which is the attach failure we need to exercise.
		chainApp, ctx, vals, coreVals, recipient := setupCEAContractOutboundTest(t, 4, false)

		recipientAcc := sdk.AccAddress(recipient.Bytes())
		balanceBefore := chainApp.BankKeeper.GetBalance(ctx, recipientAcc, "upc")

		inbound := ceaContractInbound("0xsc-outbound-funds-02", recipient, uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000")
		reachInboundQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		callPcTx := lastPcTx(t, utx)
		require.Equal(t, "FAILED", callPcTx.Status, "attach failure must surface on the PcTx, not be swallowed")
		require.Contains(t, callPcTx.ErrorMsg, "outbound attach failed")
		require.Contains(t, callPcTx.ErrorMsg, "outbound is disabled for chain")

		// The whole callback — including the gateway burn — was rolled back.
		require.Equal(t, common.Hash{}, chainApp.EVMKeeper.GetState(ctx, recipient, slot),
			"recipient slot 0 must stay 0 (callback rolled back with the attach failure)")
		require.Empty(t, utx.OutboundTx, "no outbound may be recorded when the attach failed")

		querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
		resp, err := querier.AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.Entries, "no PendingOutbounds row may survive a rolled-back callback")

		// No gas fee was collected either — the cache holding it was discarded.
		require.Equal(t, balanceBefore.Amount, chainApp.BankKeeper.GetBalance(ctx, recipientAcc, "upc").Amount,
			"no fee may be collected when the cache is discarded")

		// The deposit happens before the cache scope and stays committed, so the
		// principal is still with the recipient the sender nominated.
		require.Equal(t, "SUCCESS", utx.PcTx[0].Status, "deposit is outside the cache scope and stays committed")
	})

	// --- GAS_AND_PAYLOAD: the same branch in the sibling handler -----------
	//
	// Amount is 0 so the handler skips gasAndPayloadDepositAutoSwap, which
	// needs a live Uniswap quoter/router that the integration harness does not
	// deploy. isSmartContract is set from the recipient's code hash regardless
	// of amount, so the contract branch under test is still exercised.

	t.Run("GAS_AND_PAYLOAD contract callback gateway burn creates OutboundTx and PendingOutbounds", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, recipient := setupCEAContractOutboundTest(t, 4, true)

		inbound := ceaContractInbound("0xsc-outbound-gas-01", recipient, uexecutortypes.TxType_GAS_AND_PAYLOAD, "0")
		reachInboundQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		callPcTx := lastPcTx(t, utx)
		require.Equal(t, "SUCCESS", callPcTx.Status, "executeUniversalTx should succeed: %s", callPcTx.ErrorMsg)

		require.Equal(t, common.BigToHash(common.Big1), chainApp.EVMKeeper.GetState(ctx, recipient, slot),
			"recipient slot 0 must be 1 (callback committed)")

		require.Len(t, utx.OutboundTx, 1, "the gateway call inside the callback must produce exactly one OutboundTx")

		out := utx.OutboundTx[0]
		require.Equal(t, "eip155:11155111", out.DestinationChain)
		require.Equal(t, expectedOutboundRecipient, out.Recipient)
		require.Equal(t, expectedOutboundAmount, out.Amount)
		require.Equal(t, expectedOutboundPRC20, out.Prc20AssetAddr)
		require.Equal(t, uexecutortypes.Status_PENDING, out.OutboundStatus)

		entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, out.Id)
		require.NoError(t, err, "outbound must be indexed in PendingOutbounds")
		require.Equal(t, utxKey, entry.UniversalTxId)
	})

	t.Run("GAS_AND_PAYLOAD attach failure rolls the callback back and records FAILED PcTx", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, recipient := setupCEAContractOutboundTest(t, 4, false)

		recipientAcc := sdk.AccAddress(recipient.Bytes())
		balanceBefore := chainApp.BankKeeper.GetBalance(ctx, recipientAcc, "upc")

		inbound := ceaContractInbound("0xsc-outbound-gas-02", recipient, uexecutortypes.TxType_GAS_AND_PAYLOAD, "0")
		reachInboundQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		callPcTx := lastPcTx(t, utx)
		require.Equal(t, "FAILED", callPcTx.Status, "attach failure must surface on the PcTx, not be swallowed")
		require.Contains(t, callPcTx.ErrorMsg, "outbound attach failed")
		require.Contains(t, callPcTx.ErrorMsg, "outbound is disabled for chain")

		require.Equal(t, common.Hash{}, chainApp.EVMKeeper.GetState(ctx, recipient, slot),
			"recipient slot 0 must stay 0 (callback rolled back with the attach failure)")
		require.Empty(t, utx.OutboundTx, "no outbound may be recorded when the attach failed")

		querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
		resp, err := querier.AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.Entries, "no PendingOutbounds row may survive a rolled-back callback")

		require.Equal(t, balanceBefore.Amount, chainApp.BankKeeper.GetBalance(ctx, recipientAcc, "upc").Amount,
			"no fee may be collected when the cache is discarded")
	})

	// --- regression: the UEA branch is untouched --------------------------

	t.Run("UEA branch still attaches its outbound and indexes it", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundInitiatedOutboundTest(t, 4)
		reachInboundQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		require.Len(t, utx.OutboundTx, 1, "the UEA payload's gateway call must still produce exactly one OutboundTx")

		out := utx.OutboundTx[0]
		require.Equal(t, expectedOutboundRecipient, out.Recipient)
		require.Equal(t, expectedOutboundAmount, out.Amount)
		require.Equal(t, uexecutortypes.Status_PENDING, out.OutboundStatus)

		entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, out.Id)
		require.NoError(t, err, "UEA-branch outbound must still be indexed in PendingOutbounds")
		require.Equal(t, utxKey, entry.UniversalTxId)
	})

	// --- regression: callbacks that emit nothing are untouched -------------

	t.Run("contract callback without a gateway call still succeeds with no outbound rows", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupCEAContractOutboundTest(t, 4, true)

		// Plain STOP recipient: the callback runs, emits no logs at all.
		plain := deployMockRecipientContract(t, chainApp, ctx)
		fundCoins := sdk.NewCoins(sdk.NewInt64Coin("upc", 1_000_000_000))
		require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
		require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(
			ctx, utils.MintModule, sdk.AccAddress(plain.Bytes()), fundCoins))

		inbound := ceaContractInbound("0xsc-outbound-noop-01", plain, uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000")
		reachInboundQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		require.Equal(t, "SUCCESS", utx.PcTx[0].Status, "deposit should still succeed")
		callPcTx := lastPcTx(t, utx)
		require.Equal(t, "SUCCESS", callPcTx.Status, "callback should still succeed: %s", callPcTx.ErrorMsg)
		require.Empty(t, callPcTx.ErrorMsg)

		require.Empty(t, utx.OutboundTx, "a callback that emits nothing must not gain an outbound")

		querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
		resp, err := querier.AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.Entries, "no spurious PendingOutbounds row")
	})
}
