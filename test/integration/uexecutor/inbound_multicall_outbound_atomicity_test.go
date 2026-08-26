package integrationtest

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// F-2026-18825. A UEA payload that calls UniversalGatewayPC burns PRC20 and
// emits UniversalTxOutbound. ExecutePayloadV2 used to commit that burn via
// writeCache() and only then hand the receipt back, leaving the two inbound
// handlers to attach the outbounds afterwards, outside any cache.
// BuildOutboundsFromReceipt is all-or-nothing, so a multicall carrying one
// invalid leg (unregistered PRC20, disabled chain) discarded every valid
// outbound alongside it — while the burns for all of them stayed committed.
// The handlers then stashed the failure in UniversalTx.RevertError (9 writes /
// 0 reads chain-wide), marked the payload PcTx SUCCESS and returned nil, so
// nothing on chain recorded that anything had gone wrong.
//
// The attach now runs inside ExecutePayloadV2's existing CacheContext, before
// writeCache(): the burn and the OutboundTx / PendingOutbounds rows commit
// together or not at all, and the failure surfaces as a FAILED PcTx.
//
// The vote tx must still succeed either way — the handler runs inside
// MsgVoteInbound, and returning an error there would lose the validator's vote.
// That constraint is why the fix is atomicity rather than error propagation.

// unregisteredPRC20 is a PRC20 address with no TokenConfig registered against
// it, which is what makes the sibling leg of the multicall invalid.
var unregisteredPRC20 = common.HexToAddress("0x0000000000000000000000000000000000000e0f")

// gatewayNonceSlot is UniversalGatewayPC storage slot 2 (its outbound nonce).
// The mock gateway bumps it on every withdraw, so it doubles as a witness for
// whether the payload's EVM state was committed or rolled back.
var gatewayNonceSlot = common.BigToHash(big.NewInt(2))

// ueaMulticallSelector is bytes4(keccak256("UEA_MULTICALL")), the magic prefix
// UEA_EVM._isMulticall() looks for before decoding payload.data as Multicall[].
func ueaMulticallSelector(t *testing.T) []byte {
	t.Helper()
	sel := crypto.Keccak256([]byte("UEA_MULTICALL"))[:4]
	// Guards against the deployed UEA_EVM_BYTECODE drifting away from the
	// selector this test builds payloads with.
	require.Equal(t, "0x2cc2842d", hexutil.Encode(sel), "UEA multicall selector drifted")
	return sel
}

// multicallLeg mirrors the Solidity `Multicall { address to; uint256 value;
// bytes data; }` struct the UEA decodes out of a multicall payload.
type multicallLeg struct {
	To    common.Address
	Value *big.Int
	Data  []byte
}

// encodeUEAMulticall builds payload.data for a UEA multicall: the magic
// selector followed by an ABI-encoded Multicall[].
func encodeUEAMulticall(t *testing.T, legs []multicallLeg) string {
	t.Helper()

	tupleArray, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
	})
	require.NoError(t, err)

	encoded, err := abi.Arguments{{Type: tupleArray}}.Pack(legs)
	require.NoError(t, err)

	return hexutil.Encode(append(ueaMulticallSelector(t), encoded...))
}

// gatewayWithdrawCalldata is the UniversalGatewayPC withdraw call used across
// the outbound tests (see TestInboundInitiatedOutbound), with the burned PRC20
// left as a parameter so a leg can be made invalid. Word 2 of the argument
// block is the token the gateway reports in its UniversalTxOutbound event; the
// happy-path assertions below pin that mapping down.
func gatewayWithdrawCalldataFor(t *testing.T, prc20 common.Address) []byte {
	t.Helper()

	words := []string{
		"0000000000000000000000000000000000000000000000000000000000000020",
		"00000000000000000000000000000000000000000000000000000000000000c0",
		hexutil.Encode(common.LeftPadBytes(prc20.Bytes(), 32))[2:],         // PRC20 to burn
		"00000000000000000000000000000000000000000000000000000000000f4240", // amount: 1000000
		"000000000000000000000000000000000000000000000000000000000007a120",
		"0000000000000000000000000000000000000000000000000000000000000100",
		"0000000000000000000000001234567890abcdef1234567890abcdef12345678",
		"0000000000000000000000000000000000000000000000000000000000000014",
		"1234567890abcdef1234567890abcdef12345678000000000000000000000000",
		"0000000000000000000000000000000000000000000000000000000000000000",
	}

	data, err := hexutil.Decode("0xb3ca1fbc" + strings.Join(words, ""))
	require.NoError(t, err)
	return data
}

// multicallToGateway builds a UEA multicall payload whose legs each burn one of
// the given PRC20s through UniversalGatewayPC.
func multicallToGateway(t *testing.T, prc20s ...common.Address) string {
	t.Helper()

	gateway := utils.GetDefaultAddresses().UniversalGatewayPCAddr
	legs := make([]multicallLeg, 0, len(prc20s))
	for _, prc20 := range prc20s {
		legs = append(legs, multicallLeg{
			To:    gateway,
			Value: big.NewInt(0),
			Data:  gatewayWithdrawCalldataFor(t, prc20),
		})
	}

	return encodeUEAMulticall(t, legs)
}

// setupMulticallOutboundTest registers eip155:11155111 with outbound enabled,
// registers PRC20USDC against it, deploys the UEA for DefaultTestAddr and funds
// it with upc so gas-fee deduction never masks the behaviour under test.
func setupMulticallOutboundTest(
	t *testing.T,
	numVals int,
) (*app.ChainApp, sdk.Context, []string, []stakingtypes.Validator, common.Address) {
	t.Helper()

	chainApp, ctx, _, validators := utils.SetAppWithMultipleValidators(t, numVals)

	testAddress := utils.GetDefaultAddresses().DefaultTestAddr
	prc20Address := utils.GetDefaultAddresses().PRC20USDCAddr
	usdcAddress := utils.GetDefaultAddresses().ExternalUSDCAddr

	chainApp.UregistryKeeper.AddChainConfig(ctx, &uregistrytypes.ChainConfig{
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
			IsOutboundEnabled: true,
		},
	})

	chainApp.UregistryKeeper.AddTokenConfig(ctx, &uregistrytypes.TokenConfig{
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
	})

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

	ueModuleAccAddress, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	receipt, err := chainApp.UexecutorKeeper.DeployUEAV2(ctx, ueModuleAccAddress, &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          testAddress,
	})
	require.NoError(t, err)
	ueaAddr := common.BytesToAddress(receipt.Ret)

	fundCoins := sdk.NewCoins(sdk.NewInt64Coin("upc", 1_000_000_000))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, utils.MintModule, fundCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx, utils.MintModule, sdk.AccAddress(ueaAddr.Bytes()), fundCoins))

	return chainApp, ctx, universalVals, validators, ueaAddr
}

// multicallInbound builds a non-CEA inbound whose payload is the given
// multicall. The UE module is the caller of executeUniversalTx, so UEA_EVM
// skips signature verification and VerificationData is irrelevant here.
func multicallInbound(txHash string, txType uexecutortypes.TxType, amount, payloadData string) *uexecutortypes.Inbound {
	return &uexecutortypes.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      txHash,
		Sender:      utils.GetDefaultAddresses().DefaultTestAddr,
		Recipient:   "",
		Amount:      amount,
		AssetAddr:   utils.GetDefaultAddresses().ExternalUSDCAddr.String(),
		LogIndex:    "1",
		TxType:      txType,
		UniversalPayload: &uexecutortypes.UniversalPayload{
			To:                   utils.GetDefaultAddresses().UniversalGatewayPCAddr.Hex(),
			Value:                "0",
			Data:                 payloadData,
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "0",
			Deadline:             "0",
			VType:                uexecutortypes.VerificationType(1),
		},
		VerificationData: "",
	}
}

// payloadPcTx returns the payload PcTx, which is always the last one recorded.
func payloadPcTx(t *testing.T, utx uexecutortypes.UniversalTx) *uexecutortypes.PCTx {
	t.Helper()
	require.NotEmpty(t, utx.PcTx, "at least one PcTx must be recorded")
	return utx.PcTx[len(utx.PcTx)-1]
}

func requireNoPendingOutbounds(t *testing.T, ctx sdk.Context, chainApp *app.ChainApp, msg string) {
	t.Helper()
	querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
	resp, err := querier.AllPendingOutbounds(ctx, &uexecutortypes.QueryAllPendingOutboundsRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Entries, msg)
}

func TestInboundMulticallOutboundAtomicity(t *testing.T) {
	prc20 := utils.GetDefaultAddresses().PRC20USDCAddr
	gateway := utils.GetDefaultAddresses().UniversalGatewayPCAddr

	// --- the headline case ------------------------------------------------

	t.Run("FUNDS_AND_PAYLOAD one invalid sibling rolls the whole payload back", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddr := setupMulticallOutboundTest(t, 4)

		ueaAcc := sdk.AccAddress(ueaAddr.Bytes())
		upcBefore := chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc")

		// One valid outbound and one unregistered-PRC20 sibling, in that order,
		// so the valid one is already accumulated when the invalid one fails.
		inbound := multicallInbound("0xmulticall-funds-01", uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000",
			multicallToGateway(t, prc20, unregisteredPRC20))
		voteToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		// Nothing the payload did survives — including the burn behind the
		// valid leg, witnessed by the gateway's outbound nonce.
		require.Equal(t, common.Hash{}, chainApp.EVMKeeper.GetState(ctx, gateway, gatewayNonceSlot),
			"the gateway burn must roll back with the failed attach")
		require.Empty(t, utx.OutboundTx, "a partially-valid multicall must not leave a partial OutboundTx")
		requireNoPendingOutbounds(t, ctx, chainApp, "a partially-valid multicall must not leave a PendingOutbounds row")
		require.Equal(t, upcBefore.Amount, chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc").Amount,
			"no gas fee may be collected for a payload that was discarded")

		// The failure is recorded, not swallowed.
		pcTx := payloadPcTx(t, utx)
		require.Equal(t, "FAILED", pcTx.Status, "the payload PcTx must not report SUCCESS")
		require.Contains(t, pcTx.ErrorMsg, "outbound attach failed")
		require.Contains(t, strings.ToLower(pcTx.ErrorMsg), strings.ToLower(unregisteredPRC20.Hex()),
			"the PcTx must name the leg that could not be resolved")
		require.Empty(t, utx.RevertError, "RevertError must no longer be used to swallow attach failures")

		// The deposit happens before the payload cache, so the bridged funds
		// stay credited to the UEA and the user can simply retry.
		require.Equal(t, "SUCCESS", utx.PcTx[0].Status, "the deposit stays committed")
		require.Equal(t, "1000000", prc20BalanceOf(t, chainApp, ctx, ueaAddr).String(),
			"the bridged principal must remain with the UEA")
	})

	t.Run("GAS_AND_PAYLOAD one invalid sibling rolls the whole payload back", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddr := setupMulticallOutboundTest(t, 4)

		ueaAcc := sdk.AccAddress(ueaAddr.Bytes())
		upcBefore := chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc")

		// Amount 0 skips gasAndPayloadDepositAutoSwap, which needs a live
		// Uniswap quoter/router the integration harness does not deploy. The
		// UEA payload branch under test is reached either way.
		inbound := multicallInbound("0xmulticall-gas-01", uexecutortypes.TxType_GAS_AND_PAYLOAD, "0",
			multicallToGateway(t, prc20, unregisteredPRC20))
		voteToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		require.Equal(t, common.Hash{}, chainApp.EVMKeeper.GetState(ctx, gateway, gatewayNonceSlot),
			"the gateway burn must roll back with the failed attach")
		require.Empty(t, utx.OutboundTx, "a partially-valid multicall must not leave a partial OutboundTx")
		requireNoPendingOutbounds(t, ctx, chainApp, "a partially-valid multicall must not leave a PendingOutbounds row")
		require.Equal(t, upcBefore.Amount, chainApp.BankKeeper.GetBalance(ctx, ueaAcc, "upc").Amount,
			"no gas fee may be collected for a payload that was discarded")

		pcTx := payloadPcTx(t, utx)
		require.Equal(t, "FAILED", pcTx.Status, "the payload PcTx must not report SUCCESS")
		require.Contains(t, pcTx.ErrorMsg, "outbound attach failed")
		require.Contains(t, strings.ToLower(pcTx.ErrorMsg), strings.ToLower(unregisteredPRC20.Hex()),
			"the PcTx must name the leg that could not be resolved")
		require.Empty(t, utx.RevertError, "RevertError must no longer be used to swallow attach failures")
	})

	// --- happy path: every leg valid --------------------------------------

	t.Run("FUNDS_AND_PAYLOAD all-valid multicall attaches every outbound", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupMulticallOutboundTest(t, 4)

		inbound := multicallInbound("0xmulticall-funds-02", uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000",
			multicallToGateway(t, prc20, prc20))
		voteToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		pcTx := payloadPcTx(t, utx)
		require.Equal(t, "SUCCESS", pcTx.Status, "payload should succeed: %s", pcTx.ErrorMsg)

		require.Equal(t, common.BigToHash(big.NewInt(2)), chainApp.EVMKeeper.GetState(ctx, gateway, gatewayNonceSlot),
			"both gateway burns must be committed")
		require.Len(t, utx.OutboundTx, 2, "each valid leg must produce an OutboundTx")

		seen := map[string]bool{}
		for _, out := range utx.OutboundTx {
			require.Equal(t, "eip155:11155111", out.DestinationChain)
			require.Equal(t, common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"), common.HexToAddress(out.Recipient))
			require.Equal(t, "1000000", out.Amount)
			require.Equal(t, prc20, common.HexToAddress(out.Prc20AssetAddr))
			require.Equal(t, utils.GetDefaultAddresses().ExternalUSDCAddr, common.HexToAddress(out.ExternalAssetAddr))
			require.Equal(t, uexecutortypes.Status_PENDING, out.OutboundStatus)

			require.False(t, seen[out.Id], "each leg must get its own outbound id")
			seen[out.Id] = true

			entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, out.Id)
			require.NoError(t, err, "every outbound must be indexed in PendingOutbounds")
			require.Equal(t, utxKey, entry.UniversalTxId)
		}
		require.Empty(t, utx.RevertError)
	})

	t.Run("GAS_AND_PAYLOAD all-valid multicall attaches every outbound", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, _ := setupMulticallOutboundTest(t, 4)

		inbound := multicallInbound("0xmulticall-gas-02", uexecutortypes.TxType_GAS_AND_PAYLOAD, "0",
			multicallToGateway(t, prc20, prc20))
		voteToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		pcTx := payloadPcTx(t, utx)
		require.Equal(t, "SUCCESS", pcTx.Status, "payload should succeed: %s", pcTx.ErrorMsg)

		require.Equal(t, common.BigToHash(big.NewInt(2)), chainApp.EVMKeeper.GetState(ctx, gateway, gatewayNonceSlot),
			"both gateway burns must be committed")
		require.Len(t, utx.OutboundTx, 2, "each valid leg must produce an OutboundTx")

		for _, out := range utx.OutboundTx {
			require.Equal(t, uexecutortypes.Status_PENDING, out.OutboundStatus)
			entry, err := chainApp.UexecutorKeeper.PendingOutbounds.Get(ctx, out.Id)
			require.NoError(t, err, "every outbound must be indexed in PendingOutbounds")
			require.Equal(t, utxKey, entry.UniversalTxId)
		}
		require.Empty(t, utx.RevertError)
	})

	// --- regression: payloads that emit no gateway outbound ----------------

	t.Run("payload without a gateway call still succeeds with no outbound rows", func(t *testing.T) {
		chainApp, ctx, vals, coreVals, ueaAddr := setupMulticallOutboundTest(t, 4)

		// A plain PRC20 transfer from the UEA: real EVM work, zero gateway logs.
		inbound := multicallInbound("0xmulticall-noop-01", uexecutortypes.TxType_FUNDS_AND_PAYLOAD, "1000000",
			"0xa9059cbb000000000000000000000000527f3692f5c53cfa83f7689885995606f93b616400000000000000000000000000000000000000000000000000000000000f4240")
		inbound.UniversalPayload.To = utils.GetDefaultAddresses().PRC20USDCAddr.Hex()
		voteToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utxKey := uexecutortypes.GetInboundUniversalTxKey(*inbound)
		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxKey)
		require.NoError(t, err)
		require.True(t, found)

		pcTx := payloadPcTx(t, utx)
		require.Equal(t, "SUCCESS", pcTx.Status, "payload should succeed: %s", pcTx.ErrorMsg)
		require.Empty(t, pcTx.ErrorMsg)

		require.Empty(t, utx.OutboundTx, "a payload that emits no gateway event must not gain an outbound")
		requireNoPendingOutbounds(t, ctx, chainApp, "no spurious PendingOutbounds row")
		require.Empty(t, utx.RevertError)

		// The transfer itself committed, so the cache was written.
		require.Equal(t, "0", prc20BalanceOf(t, chainApp, ctx, ueaAddr).String(),
			"the payload's PRC20 transfer must still be committed")
	})
}

// prc20BalanceOf reads PRC20USDC.balanceOf(holder).
func prc20BalanceOf(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, holder common.Address) *big.Int {
	t.Helper()

	prc20ABI, err := uexecutortypes.ParsePRC20ABI()
	require.NoError(t, err)

	ueModuleAccAddress, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	res, err := chainApp.EVMKeeper.CallEVM(
		ctx,
		prc20ABI,
		ueModuleAccAddress,
		utils.GetDefaultAddresses().PRC20USDCAddr,
		false,
		nil,
		"balanceOf",
		holder,
	)
	require.NoError(t, err)

	values, err := prc20ABI.Unpack("balanceOf", res.Ret)
	require.NoError(t, err)
	require.Len(t, values, 1)

	return values[0].(*big.Int)
}
