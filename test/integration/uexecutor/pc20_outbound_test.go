package integrationtest

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	authz "github.com/cosmos/cosmos-sdk/x/authz"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

const pc20DestChain = "eip155:11155111"

// pc20SourceAsset stands in for a Push-native ERC20 that a PC20 export locks in
// VaultPC20. It is deliberately NOT registered as a PRC20 token config — the
// whole point of the PC20 path is that it must not require one.
var pc20SourceAsset = common.HexToAddress("0x000000000000000000000000000000000000c0de")

// pc20Payload returns a payload that starts with the PC20 selector, which is how
// the outbound builder recognises an export and skips the PRC20 registry lookup.
func pc20Payload() []byte {
	// selector ("PC20") + one 32-byte word of trailing call data for realism.
	return append(common.FromHex("0x"+uexecutortypes.PC20Selector), make([]byte, 32)...)
}

// pc20AppWithOutbound returns an app whose destChain has outbound enabled/disabled
// as requested. Callers add token configs only when they want the PRC20 path to
// resolve.
func pc20AppWithOutbound(t *testing.T, outboundEnabled bool) (*app.ChainApp, sdk.Context) {
	t.Helper()
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	require.NoError(t, chainApp.UregistryKeeper.AddChainConfig(ctx, &uregistrytypes.ChainConfig{
		Chain:          pc20DestChain,
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: outboundEnabled,
		},
	}))
	return chainApp, ctx
}

// pc20OutboundLog builds one synthetic UniversalTxOutbound log for a given tx id,
// source token, tx type and payload.
func pc20OutboundLog(t *testing.T, txIDHex string, token common.Address, txType uint8, payload []byte) *ethtypes.Log {
	t.Helper()
	recipient := common.HexToAddress("0x527f3692f5c53cfa83f7689885995606f93b6164")
	data, err := encodeUniversalTxOutboundData(
		pc20DestChain, recipient.Bytes(), big.NewInt(1000000),
		common.Address{}, big.NewInt(111), big.NewInt(21000),
		payload, big.NewInt(0),
		common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr),
		txType, big.NewInt(1000000000),
	)
	require.NoError(t, err)
	return &ethtypes.Log{
		Address: common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_GATEWAY_PC"].Address),
		Topics: []common.Hash{
			common.HexToHash(uexecutortypes.UniversalTxOutboundEventSig),
			common.HexToHash(txIDHex),
			common.HexToHash("0x000000000000000000000000" + utils.GetDefaultAddresses().DefaultTestAddr[2:]),
			common.HexToHash("0x000000000000000000000000" + token.Hex()[2:]),
		},
		Data:    data,
		Removed: false,
	}
}

func pc20Receipt(txHash string, logs ...*ethtypes.Log) *ethtypes.Receipt {
	return &ethtypes.Receipt{TxHash: common.HexToHash(txHash), GasUsed: 50000, Logs: logs}
}

func pc20Sender() common.Address {
	return common.HexToAddress(utils.GetDefaultAddresses().DefaultTestAddr)
}

func pc20OnlyOutbound(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context) *uexecutortypes.OutboundTx {
	t.Helper()
	querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
	resp, err := querier.AllUniversalTx(sdk.WrapSDKContext(ctx),
		&uexecutortypes.QueryAllUniversalTxRequest{Pagination: &query.PageRequest{}})
	require.NoError(t, err)
	require.Len(t, resp.UniversalTxs, 1)
	require.Len(t, resp.UniversalTxs[0].OutboundTx, 1)
	return resp.UniversalTxs[0].OutboundTx[0]
}

// ---------------------------------------------------------------------------
// End-to-end — a real gateway call creates the outbound
//
// The most faithful test: deploy a PC20-aware gateway at the UNIVERSAL_GATEWAY_PC
// address, make a REAL EVM call into its sendUniversalTxOutbound with a PC20
// payload, and run the REAL UniversalTxOutbound log it emits through the REAL hook.
// Nothing about the event is hand-built — the gateway bytecode produces it — so
// this covers the whole export path: gateway payload routing → event emission →
// chain decode → PC20 outbound creation.
//
// MOCK_GATEWAY_PC_PC20_BYTECODE is a minimal contract that mirrors the real
// UniversalGatewayPC's payload-based PC20 detection and its exact
// UniversalTxOutbound event, minus the vault/core/burn machinery that cannot run
// standalone in this harness.
// ---------------------------------------------------------------------------

// mockGatewayABI is the single entry point of the PC20 test gateway — the same
// sendUniversalTxOutbound request shape as the real UniversalGatewayPC.
const mockGatewayABI = `[{"type":"function","name":"sendUniversalTxOutbound","stateMutability":"payable","inputs":[{"name":"req","type":"tuple","components":[` +
	`{"name":"recipient","type":"bytes"},` +
	`{"name":"token","type":"address"},` +
	`{"name":"amount","type":"uint256"},` +
	`{"name":"gasLimit","type":"uint256"},` +
	`{"name":"gasPrice","type":"uint256"},` +
	`{"name":"maxPCForGas","type":"uint256"},` +
	`{"name":"payload","type":"bytes"},` +
	`{"name":"revertRecipient","type":"address"}` +
	`]}],"outputs":[]}]`

// ugpcRequest mirrors UniversalOutboundTxRequest for ABI packing.
type ugpcRequest struct {
	Recipient       []byte         `abi:"recipient"`
	Token           common.Address `abi:"token"`
	Amount          *big.Int       `abi:"amount"`
	GasLimit        *big.Int       `abi:"gasLimit"`
	GasPrice        *big.Int       `abi:"gasPrice"`
	MaxPCForGas     *big.Int       `abi:"maxPCForGas"`
	Payload         []byte         `abi:"payload"`
	RevertRecipient common.Address `abi:"revertRecipient"`
}

func TestPC20Export_RealGatewayCall(t *testing.T) {
	chainApp, ctx := pc20AppWithOutbound(t, true)

	// Deploy the PC20-aware gateway at the UNIVERSAL_GATEWAY_PC address — the only
	// address whose logs the hook trusts.
	gatewayAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_GATEWAY_PC"].Address)
	utils.DeployContract(t, chainApp, ctx, gatewayAddr, utils.MOCK_GATEWAY_PC_PC20_BYTECODE)

	gwABI, err := abi.JSON(strings.NewReader(mockGatewayABI))
	require.NoError(t, err)

	// A PC20 export request: the payload is prefixed with the PC20 selector, and the
	// token is an asset that has NO PRC20 token config (as a real PC20 asset would).
	req := ugpcRequest{
		Recipient:       common.FromHex("0x1234567890abcdef1234567890abcdef12345678"),
		Token:           pc20SourceAsset,
		Amount:          big.NewInt(1000000),
		GasLimit:        big.NewInt(21000),
		GasPrice:        big.NewInt(0),
		MaxPCForGas:     big.NewInt(0),
		Payload:         pc20Payload(),
		RevertRecipient: common.HexToAddress("0x000000000000000000000000000000000000dead"),
	}

	from, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)

	// Manage the module nonce exactly as the keeper's own contract calls do.
	nonce, err := chainApp.UexecutorKeeper.GetModuleAccountNonce(ctx)
	require.NoError(t, err)
	_, err = chainApp.UexecutorKeeper.IncrementModuleAccountNonce(ctx)
	require.NoError(t, err)

	// Real EVM execution of the gateway (module sender, commit) — emits the log.
	resp, err := chainApp.EVMKeeper.DerivedEVMCall(
		ctx, gwABI, from, gatewayAddr,
		big.NewInt(0), nil, // value, gasLimit
		true, false, true, &nonce, // commit, gasless, isModuleSender, manualNonce
		"sendUniversalTxOutbound", req,
	)
	require.NoError(t, err)
	require.False(t, resp.Failed(), "gateway call reverted: %s", resp.VmError)
	require.NotEmpty(t, resp.Logs, "gateway must emit a UniversalTxOutbound log")

	// Feed the REAL emitted logs through the REAL EVM hook (internal DerivedEVMCall
	// does not run PostTxProcessing itself).
	receipt := &ethtypes.Receipt{
		TxHash:  common.HexToHash("0xpc20realgatewaycall"),
		GasUsed: resp.GasUsed,
		Logs:    evmtypes.LogsToEthereum(resp.Logs),
	}
	hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)
	require.NoError(t, hooks.PostTxProcessing(ctx, from, core.Message{}, receipt))

	// The chain created a PC20 outbound purely from the gateway-emitted event.
	ob := pc20OnlyOutbound(t, chainApp, ctx)
	require.True(t, ob.IsPc20, "an export via a real gateway call must be PC20")
	require.Equal(t, pc20SourceAsset.Hex(), ob.Pc20ContractAddress)
	require.Empty(t, ob.ExternalAssetAddr, "PC20 needs no token config")
	require.Equal(t, uexecutortypes.TxType_FUNDS_AND_PAYLOAD, ob.TxType, "gateway sets FUNDS_AND_PAYLOAD for a PC20 payload")
	require.Equal(t, "1000000", ob.Amount)
	require.Equal(t, pc20DestChain, ob.DestinationChain)
	require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus)
}

// ---------------------------------------------------------------------------
// Outbound creation — the load-bearing fix
//
// A PC20 export carries a source token that has no PRC20 token config. Before
// the fix, BuildOutboundsFromReceipt called GetTokenConfigByPRC20 for every
// funds outbound; that lookup erroring propagated out of the EVM PostTxProcessing
// hook and reverted the whole tx (including the vault lock). These drive real
// synthetic events through the real hook.
// ---------------------------------------------------------------------------

func TestPC20Export_OutboundCreation(t *testing.T) {
	t.Run("PC20 export builds an outbound with no token config and does not revert", func(t *testing.T) {
		chainApp, ctx := pc20AppWithOutbound(t, true)

		// txType 3 == FUNDS_AND_PAYLOAD (Solidity enum → proto), PC20-selector payload.
		receipt := pc20Receipt("0xpc20export01", pc20OutboundLog(t, "0x01", pc20SourceAsset, 3, pc20Payload()))
		hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)

		err := hooks.PostTxProcessing(ctx, pc20Sender(), core.Message{}, receipt)
		require.NoError(t, err, "PC20 export must not revert despite having no PRC20 token config")

		ob := pc20OnlyOutbound(t, chainApp, ctx)
		require.True(t, ob.IsPc20, "outbound must be flagged as PC20")
		require.Equal(t, pc20SourceAsset.Hex(), ob.Pc20ContractAddress, "source token carried in pc20_contract_address")
		require.Empty(t, ob.ExternalAssetAddr, "no registry lookup, so external_asset_addr stays empty")
		require.Empty(t, ob.Prc20AssetAddr, "PC20 export has no PRC20 counterpart")
		require.Equal(t, uexecutortypes.TxType_FUNDS_AND_PAYLOAD, ob.TxType)
		require.Equal(t, "1000000", ob.Amount)
		require.Equal(t, pc20DestChain, ob.DestinationChain)
		require.Equal(t, uexecutortypes.Status_PENDING, ob.OutboundStatus)
	})

	t.Run("PC20 export emits outbound_created carrying the PC20 fields", func(t *testing.T) {
		chainApp, ctx := pc20AppWithOutbound(t, true)

		receipt := pc20Receipt("0xpc20export03", pc20OutboundLog(t, "0x03", pc20SourceAsset, 3, pc20Payload()))
		hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)
		require.NoError(t, hooks.PostTxProcessing(ctx, pc20Sender(), core.Message{}, receipt))

		// A relayer settles a PC20 export straight off this event, so it must carry
		// is_pc20 + the source token (asset_addr is empty for PC20).
		var attrs map[string]string
		for _, ev := range ctx.EventManager().Events() {
			if ev.Type != uexecutortypes.EventTypeOutboundCreated {
				continue
			}
			m := map[string]string{}
			for _, a := range ev.Attributes {
				m[a.Key] = a.Value
			}
			if m["is_pc20"] == "true" {
				attrs = m
				break
			}
		}
		require.NotNil(t, attrs, "expected an outbound_created event flagged is_pc20")
		require.Equal(t, pc20SourceAsset.Hex(), attrs["pc20_contract_address"])
		require.Empty(t, attrs["asset_addr"])
	})

	t.Run("one receipt with both a PC20 and a PRC20 log routes each independently", func(t *testing.T) {
		chainApp, ctx := pc20AppWithOutbound(t, true)

		// Register a token config so the PRC20 log resolves; the PC20 log stays
		// unregistered. Both events ride the same tx, so they attach to one UTX.
		usdcAddr := utils.GetDefaultAddresses().ExternalUSDCAddr
		prc20Addr := utils.GetDefaultAddresses().PRC20USDCAddr
		require.NoError(t, chainApp.UregistryKeeper.AddTokenConfig(ctx, &uregistrytypes.TokenConfig{
			Chain: pc20DestChain, Address: usdcAddr.String(), Name: "USD Coin", Symbol: "USDC",
			Decimals: 6, Enabled: true, LiquidityCap: "1000000000000000000000000", TokenType: 1,
			NativeRepresentation: &uregistrytypes.NativeRepresentation{ContractAddress: prc20Addr.String()},
		}))

		receipt := pc20Receipt("0xmixed01",
			pc20OutboundLog(t, "0x0a", pc20SourceAsset, 3, pc20Payload()), // PC20 export
			pc20OutboundLog(t, "0x0b", prc20Addr, 2, []byte{}),            // PRC20 funds
		)
		hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)
		require.NoError(t, hooks.PostTxProcessing(ctx, pc20Sender(), core.Message{}, receipt))

		querier := uexecutorkeeper.NewQuerier(chainApp.UexecutorKeeper)
		resp, err := querier.AllUniversalTx(sdk.WrapSDKContext(ctx),
			&uexecutortypes.QueryAllUniversalTxRequest{Pagination: &query.PageRequest{}})
		require.NoError(t, err)
		require.Len(t, resp.UniversalTxs, 1)
		require.Len(t, resp.UniversalTxs[0].OutboundTx, 2, "both outbounds attach to the single UTX")

		var pc20, prc20 *uexecutortypes.OutboundTx
		for _, ob := range resp.UniversalTxs[0].OutboundTx {
			if ob.IsPc20 {
				pc20 = ob
			} else {
				prc20 = ob
			}
		}
		require.NotNil(t, pc20, "PC20 outbound present")
		require.NotNil(t, prc20, "PRC20 outbound present")

		require.Equal(t, pc20SourceAsset.Hex(), pc20.Pc20ContractAddress)
		require.Empty(t, pc20.ExternalAssetAddr)

		require.Equal(t, usdcAddr.String(), prc20.ExternalAssetAddr, "PRC20 outbound resolves external asset via registry")
		require.Equal(t, prc20Addr.Hex(), prc20.Prc20AssetAddr)
		require.False(t, prc20.IsPc20)
	})

	t.Run("PC20 export to a disabled outbound chain is rejected", func(t *testing.T) {
		chainApp, ctx := pc20AppWithOutbound(t, false)

		// The enable gate runs before PC20 routing, so a PC20 export must not sneak
		// past a disabled chain just because it skips the token lookup.
		receipt := pc20Receipt("0xpc20disabled", pc20OutboundLog(t, "0x04", pc20SourceAsset, 3, pc20Payload()))
		hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)

		err := hooks.PostTxProcessing(ctx, pc20Sender(), core.Message{}, receipt)
		require.Error(t, err)
		require.Contains(t, err.Error(), "outbound is disabled")
	})
}

// ---------------------------------------------------------------------------
// Payload routing — PC20 vs PRC20 discrimination
//
// The only thing that distinguishes a PC20 export from a PRC20 outbound is the
// 4-byte selector prefix on the event payload. Routing must be exact and must
// never panic on a short or malformed payload; anything that isn't a clean PC20
// selector falls back to the PRC20 path. With no token config registered, a
// PC20-routed event succeeds (lookup skipped) while a PRC20-routed event errors
// on the missing config — so the hook's success/failure *is* the routing verdict.
// ---------------------------------------------------------------------------

func TestPC20Export_PayloadRouting(t *testing.T) {
	cases := []struct {
		name     string
		payload  string // 0x-hex, becomes the raw event payload bytes
		wantPC20 bool
	}{
		{"selector only (4 bytes, no body) routes to PC20", "0x" + uexecutortypes.PC20Selector, true},
		{"selector plus body routes to PC20", "0x" + uexecutortypes.PC20Selector + strings.Repeat("00", 32), true},
		{"empty payload routes to PRC20", "0x", false},
		{"payload shorter than the selector routes to PRC20 (must not panic)", "0x504332", false},
		{"near-miss selector (last nibble differs) routes to PRC20", "0x50433231", false},
		{"selector not at the start routes to PRC20", "0xff" + uexecutortypes.PC20Selector, false},
	}

	for i, tc := range cases {
		i, tc := i, tc
		t.Run(tc.name, func(t *testing.T) {
			chainApp, ctx := pc20AppWithOutbound(t, true)

			// txType is FUNDS_AND_PAYLOAD throughout: routing is decided purely by
			// the payload selector, and the PRC20 path's token lookup is not gated
			// on tx type — so the payload alone determines success vs error here.
			receipt := pc20Receipt(fmt.Sprintf("0xroute%02d", i),
				pc20OutboundLog(t, fmt.Sprintf("0x%02x", 0x20+i), pc20SourceAsset, 3, common.FromHex(tc.payload)))
			hooks := uexecutorkeeper.NewEVMHooks(chainApp.UexecutorKeeper)

			err := hooks.PostTxProcessing(ctx, pc20Sender(), core.Message{}, receipt)

			if tc.wantPC20 {
				require.NoError(t, err, "PC20-routed payload must not need a token config")
				ob := pc20OnlyOutbound(t, chainApp, ctx)
				require.True(t, ob.IsPc20)
				require.Equal(t, pc20SourceAsset.Hex(), ob.Pc20ContractAddress)
				require.Empty(t, ob.ExternalAssetAddr)
			} else {
				require.Error(t, err, "PRC20-routed payload must require a token config it does not have")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Settlement routing
//
// On a failed vote a PC20 export must release the locked native via
// VaultPC20.revertExport — never via the PRC20 re-mint path. VAULT_PC20 is not a
// registered system contract in the test app, so the release fails fast with a
// clear guard error: the outbound ends ABORTED carrying the VaultPC20 diagnostic,
// while an unmodified PRC20 outbound reverts via a deployed contract and ends
// REVERTED. On a successful vote, recording the wrapper mapping is best-effort,
// so a setWrapperDeployed revert must not block settlement.
// ---------------------------------------------------------------------------

// execVoteOutboundWithWrapper mirrors utils.ExecVoteOutbound but lets the caller
// attach a PC20 wrapper address to the observation, exercising the success-path
// setWrapperDeployed call.
func execVoteOutboundWithWrapper(
	t *testing.T,
	ctx sdk.Context,
	chainApp *app.ChainApp,
	universalAddr, coreValAddr, utxId string,
	outbound *uexecutortypes.OutboundTx,
	success bool,
	wrapper string,
) error {
	t.Helper()
	msg := &uexecutortypes.MsgVoteOutbound{
		Signer: coreValAddr,
		TxId:   outbound.Id,
		UtxId:  utxId,
		ObservedTx: &uexecutortypes.OutboundObservation{
			Success:            success,
			TxHash:             fmt.Sprintf("0xobserved-%s", outbound.Id),
			BlockHeight:        1,
			GasFeeUsed:         outbound.GasFee,
			Pc20WrapperAddress: wrapper,
		},
	}
	execMsg := authz.NewMsgExec(sdk.MustAccAddressFromBech32(universalAddr), []sdk.Msg{msg})
	_, err := chainApp.AuthzKeeper.Exec(ctx, &execMsg)
	return err
}

// mutateSeededOutboundToPC20 rewrites the outbound seeded by
// setupOutboundVotingTest into a PC20 export (with the given tx type) in place and
// persists it, so the existing validator/authz voting machinery drives PC20
// settlement.
func mutateSeededOutboundToPC20(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, utxId string, ob *uexecutortypes.OutboundTx, txType uexecutortypes.TxType) {
	t.Helper()
	ob.IsPc20 = true
	ob.Pc20ContractAddress = pc20SourceAsset.Hex()
	ob.Prc20AssetAddr = ""
	ob.ExternalAssetAddr = ""
	ob.TxType = txType
	require.NoError(t, chainApp.UexecutorKeeper.UpdateOutbound(ctx, utxId, *ob))
}

// castPC20SettlementVotes grants outbound-vote authz to every validator pair and
// casts 3 votes (quorum for the 4-validator setup) with the given success flag
// and optional wrapper address.
func castPC20SettlementVotes(
	t *testing.T,
	ctx sdk.Context,
	chainApp *app.ChainApp,
	vals []string,
	coreVals []stakingtypes.Validator,
	utxId string,
	ob *uexecutortypes.OutboundTx,
	success bool,
	wrapper string,
) {
	t.Helper()
	for i, val := range coreVals {
		accAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
		require.NoError(t, err)
		exp := ctx.BlockTime().Add(time.Hour)
		require.NoError(t, chainApp.AuthzKeeper.SaveGrant(
			ctx,
			sdk.MustAccAddressFromBech32(vals[i]),
			sdk.AccAddress(accAddr),
			authz.NewGenericAuthorization(sdk.MsgTypeURL(&uexecutortypes.MsgVoteOutbound{})),
			&exp,
		))
	}
	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, execVoteOutboundWithWrapper(
			t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), utxId, ob, success, wrapper,
		))
	}
}

func TestPC20Export_FailedSettlement(t *testing.T) {
	t.Run("PC20 failed vote releases via revertExport and aborts on missing VaultPC20", func(t *testing.T) {
		chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)
		mutateSeededOutboundToPC20(t, chainApp, ctx, utxId, ob, uexecutortypes.TxType_FUNDS_AND_PAYLOAD)

		castPC20SettlementVotes(t, ctx, chainApp, vals, coreVals, utxId, ob, false, "")

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.True(t, found)
		got := utx.OutboundTx[0]

		// revertExport fails fast because VAULT_PC20 is not registered → ABORTED,
		// with the VaultPC20 guard surfaced in both the abort reason and the
		// recorded revert execution. Proof it took the PC20 release path.
		require.Equal(t, uexecutortypes.Status_ABORTED, got.OutboundStatus)
		require.Contains(t, got.AbortReason, "VAULT_PC20 system contract is not registered")
		require.NotNil(t, got.PcRevertExecution)
		require.Equal(t, "FAILED", got.PcRevertExecution.Status)
		require.Contains(t, got.PcRevertExecution.ErrorMsg, "VAULT_PC20 system contract is not registered")
	})

	t.Run("PC20 export releases on failure even with a non-funds tx type", func(t *testing.T) {
		chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)
		// TxType_PAYLOAD sits outside the funds-revert gate; the is_pc20 clause is
		// what still forces a release. Without that hardening this would skip the
		// revert entirely and end REVERTED with no PcRevertExecution.
		mutateSeededOutboundToPC20(t, chainApp, ctx, utxId, ob, uexecutortypes.TxType_PAYLOAD)

		castPC20SettlementVotes(t, ctx, chainApp, vals, coreVals, utxId, ob, false, "")

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.True(t, found)
		got := utx.OutboundTx[0]

		require.Equal(t, uexecutortypes.Status_ABORTED, got.OutboundStatus, "locked native must never be stranded on a tx-type gate")
		require.NotNil(t, got.PcRevertExecution, "release was attempted despite the non-funds tx type")
		require.Contains(t, got.PcRevertExecution.ErrorMsg, "VAULT_PC20 system contract is not registered")
	})

	t.Run("PRC20 failed vote reverts via re-mint (contrast: not aborted)", func(t *testing.T) {
		// Left unmodified, the seeded outbound is a PRC20 outbound. Its re-mint path
		// reaches a deployed PRC20 contract, so a failed vote REVERTS cleanly —
		// diverging from the PC20 export above, which aborts.
		chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)

		castPC20SettlementVotes(t, ctx, chainApp, vals, coreVals, utxId, ob, false, "")

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.True(t, found)
		got := utx.OutboundTx[0]

		require.False(t, got.IsPc20)
		require.Equal(t, uexecutortypes.Status_REVERTED, got.OutboundStatus)
		require.NotEqual(t, uexecutortypes.Status_ABORTED, got.OutboundStatus)
	})
}

func TestPC20Export_SuccessfulSettlement(t *testing.T) {
	t.Run("successful vote completes even when best-effort setWrapperDeployed reverts", func(t *testing.T) {
		chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)
		mutateSeededOutboundToPC20(t, chainApp, ctx, utxId, ob, uexecutortypes.TxType_FUNDS_AND_PAYLOAD)

		// A wrapper address drives flipPC20WrapperDeployed → setWrapperDeployed on
		// UniversalCore (0xC0). The test app deploys handler bytecode there without
		// that method, so the call reverts; because the mapping write is
		// best-effort, settlement must still finalize as OBSERVED.
		castPC20SettlementVotes(t, ctx, chainApp, vals, coreVals, utxId, ob, true, "0x00000000000000000000000000000000deadbeef")

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.True(t, found)
		got := utx.OutboundTx[0]

		require.Equal(t, uexecutortypes.Status_OBSERVED, got.OutboundStatus)
		require.NotNil(t, got.ObservedTx)
		require.True(t, got.ObservedTx.Success)
	})

	t.Run("repeat export with no new wrapper settles cleanly (setWrapperDeployed skipped)", func(t *testing.T) {
		chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)
		mutateSeededOutboundToPC20(t, chainApp, ctx, utxId, ob, uexecutortypes.TxType_FUNDS_AND_PAYLOAD)

		// No wrapper in the observation (the wrapper already existed on the
		// destination from a prior export) → flipPC20WrapperDeployed is a no-op.
		// Settlement still finalizes as OBSERVED.
		castPC20SettlementVotes(t, ctx, chainApp, vals, coreVals, utxId, ob, true, "")

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
		require.NoError(t, err)
		require.True(t, found)
		got := utx.OutboundTx[0]

		require.Equal(t, uexecutortypes.Status_OBSERVED, got.OutboundStatus)
		require.NotNil(t, got.ObservedTx)
		require.True(t, got.ObservedTx.Success)
	})
}
