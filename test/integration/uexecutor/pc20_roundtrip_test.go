package integrationtest

import (
	"math/big"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	chainutils "github.com/pushchain/push-chain-node/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

const erc20ABI = `[
  {"type":"function","name":"mint","stateMutability":"nonpayable","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[]},
  {"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"a","type":"address"}],"outputs":[{"name":"","type":"uint256"}]}]`

const vaultRecordLockABI = `[
  {"type":"function","name":"recordLock","stateMutability":"nonpayable","inputs":[{"name":"token","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[]}]`

// evmCommit runs a state-changing contract call from the UE module (gates are stripped
// in the test bytecodes), asserting it does not revert.
func evmCommit(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, contract common.Address, abiJSON, method string, args ...interface{}) {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	require.NoError(t, err)
	from, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	nonce, err := chainApp.UexecutorKeeper.GetModuleAccountNonce(ctx)
	require.NoError(t, err)
	_, err = chainApp.UexecutorKeeper.IncrementModuleAccountNonce(ctx)
	require.NoError(t, err)
	resp, err := chainApp.EVMKeeper.DerivedEVMCall(ctx, parsed, from, contract, big.NewInt(0), nil, true, false, true, &nonce, method, args...)
	require.NoError(t, err)
	require.False(t, resp.Failed(), "%s reverted: %s", method, resp.VmError)
}

// erc20BalanceOf reads an ERC20 balance via a view call.
func erc20BalanceOf(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, token, who common.Address) *big.Int {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(erc20ABI))
	require.NoError(t, err)
	from, _ := chainApp.UexecutorKeeper.GetUeModuleAddress(ctx)
	receipt, err := chainApp.EVMKeeper.CallEVM(ctx, chainApp.EVMKeeper.NewStateDB(ctx), parsed, from, token, false, false, nil, "balanceOf", who)
	require.NoError(t, err)
	out, err := parsed.Methods["balanceOf"].Outputs.Unpack(receipt.Ret)
	require.NoError(t, err)
	return out[0].(*big.Int)
}

// fundVaultForPC20 deploys the MockERC20 source token, mints `amount` to the vault, and
// records the lock — so VaultPC20.unlock/revertExport can actually transfer it out.
func fundVaultForPC20(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, vault, source common.Address, amount *big.Int) {
	t.Helper()
	utils.DeployContract(t, chainApp, ctx, source, utils.MOCK_ERC20_BYTECODE)
	evmCommit(t, chainApp, ctx, source, erc20ABI, "mint", vault, amount)
	evmCommit(t, chainApp, ctx, vault, vaultRecordLockABI, "recordLock", source, amount)
}

// These are TRUE round-trip integration tests: they deploy the REAL contracts
// (UniversalCore with setWrapperDeployed/getPC20Source, VaultPC20 with unlock, and
// a MockERC20 source token — access-control modifiers stripped so the chain's
// module-sender call is accepted) and assert the PC20 success paths that previously
// only reverted in the harness: setWrapperDeployed actually records the wrapper↔source
// mapping, getPC20Source actually resolves it, and VaultPC20.unlock actually releases
// the locked token.

const pc20VaultAddr = "0x00000000000000000000000000000000000000C2"

// deployUniversalCorePC20 SetCodes the real UniversalCore (PC20 methods) over 0xC0.
func deployUniversalCorePC20(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context) {
	t.Helper()
	coreAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)
	utils.DeployContract(t, chainApp, ctx, coreAddr, utils.UNIVERSAL_CORE_PC20_BYTECODE)
}

// registerAndDeployVaultPC20 registers VAULT_PC20 in SYSTEM_CONTRACTS test-only (with
// cleanup, so production stays fail-fast and other tests still see it unregistered)
// and SetCodes the real VaultPC20 there.
func registerAndDeployVaultPC20(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context) common.Address {
	t.Helper()
	vault := common.HexToAddress(pc20VaultAddr)
	uregistrytypes.SYSTEM_CONTRACTS["VAULT_PC20"] = uregistrytypes.ContractAddresses{Address: vault.Hex()}
	t.Cleanup(func() { delete(uregistrytypes.SYSTEM_CONTRACTS, "VAULT_PC20") })
	utils.DeployContract(t, chainApp, ctx, vault, utils.VAULT_PC20_TEST_BYTECODE)
	return vault
}

// TestPC20Roundtrip_OutboundRecordsMapping proves a successful PC20 export settlement
// actually writes the wrapper↔source mapping into the real UniversalCore — verified by
// reading it back through getPC20Source (the exact call the return path uses).
func TestPC20Roundtrip_OutboundRecordsMapping(t *testing.T) {
	chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)
	deployUniversalCorePC20(t, chainApp, ctx)

	mutateSeededOutboundToPC20(t, chainApp, ctx, utxId, ob, uexecutortypes.TxType_FUNDS_AND_PAYLOAD)

	// EVM wrapper on the destination chain (native hex), reported in the observation.
	const wrapper = "0x00000000000000000000000000000000DeaDBeef"
	castPC20SettlementVotes(t, ctx, chainApp, vals, coreVals, utxId, ob, true, wrapper)

	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found)
	got := utx.OutboundTx[0]

	// Settlement finalized and the wrapper was backfilled as the external asset.
	require.Equal(t, uexecutortypes.Status_OBSERVED, got.OutboundStatus)
	require.Equal(t, wrapper, got.ExternalAssetAddr)

	// The mapping was really recorded: getPC20Source(wrapper) resolves to the source.
	wrapperB32, err := chainutils.AddressToBytes32(got.DestinationChain, wrapper)
	require.NoError(t, err)
	source, known, err := chainApp.UexecutorKeeper.CallUniversalCoreGetPC20Source(ctx, wrapperB32, got.DestinationChain)
	require.NoError(t, err)
	require.True(t, known, "setWrapperDeployed must have recorded the wrapper->source mapping")
	require.Equal(t, pc20SourceAsset, source, "resolved source must equal the exported PC20 token")
}

// TestPC20Roundtrip_InboundUnlockReleases proves a PC20 return actually resolves the
// source via getPC20Source and releases the locked token via VaultPC20.unlock — the
// full inbound success path, end to end against the real contracts.
func TestPC20Roundtrip_InboundUnlockReleases(t *testing.T) {
	chainApp, ctx, vals, inbound, coreVals, ueaAddr := setupInboundInitiatedOutboundTest(t, 4)
	deployUniversalCorePC20(t, chainApp, ctx)
	vault := registerAndDeployVaultPC20(t, chainApp, ctx)

	const wrapper = "0x00000000000000000000000000000000cafeBabe"
	amount, ok := new(big.Int).SetString(inbound.Amount, 10)
	require.True(t, ok)
	fundVaultForPC20(t, chainApp, ctx, vault, pc20SourceAsset, amount)

	// Seed the wrapper->source mapping as a prior export would have.
	wrapperB32, err := chainutils.AddressToBytes32(inbound.SourceChain, wrapper)
	require.NoError(t, err)
	_, err = chainApp.UexecutorKeeper.CallUniversalCoreSetWrapperDeployed(ctx, pc20SourceAsset, inbound.SourceChain, wrapperB32)
	require.NoError(t, err)

	// Turn the seeded inbound into a PC20 return.
	inbound.AssetAddr = wrapper
	inbound.IsCEA = false
	inbound.UniversalPayload = nil
	inbound.RawPayload = pc20SelectorPrefixed(t)

	preUEA := erc20BalanceOf(t, chainApp, ctx, pc20SourceAsset, ueaAddr)

	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteInbound(t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), inbound))
	}

	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound))
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, utx.InboundTx.IsPc20, "return must be flagged PC20")
	require.Nil(t, hasInboundRevert(utx), "a successful unlock must not produce an INBOUND_REVERT")

	postUEA := erc20BalanceOf(t, chainApp, ctx, pc20SourceAsset, ueaAddr)
	require.Equal(t, amount, new(big.Int).Sub(postUEA, preUEA), "VaultPC20.unlock must release the source token to the UEA")
}

// TestPC20Roundtrip_FailedSettlementReleasesViaRevertExport proves a failed PC20 export
// settlement really releases the locked native via VaultPC20.revertExport -> REVERTED
// (not ABORTED), with the funds actually transferred to the revert recipient.
func TestPC20Roundtrip_FailedSettlementReleasesViaRevertExport(t *testing.T) {
	chainApp, ctx, vals, utxId, ob, coreVals := setupOutboundVotingTest(t, 4)
	deployUniversalCorePC20(t, chainApp, ctx)
	vault := registerAndDeployVaultPC20(t, chainApp, ctx)
	mutateSeededOutboundToPC20(t, chainApp, ctx, utxId, ob, uexecutortypes.TxType_FUNDS_AND_PAYLOAD)

	amount, ok := new(big.Int).SetString(ob.Amount, 10)
	require.True(t, ok)
	fundVaultForPC20(t, chainApp, ctx, vault, pc20SourceAsset, amount)

	recipient := common.HexToAddress(ob.Sender)
	if ob.RevertInstructions != nil && ob.RevertInstructions.FundRecipient != "" {
		recipient = common.HexToAddress(ob.RevertInstructions.FundRecipient)
	}
	pre := erc20BalanceOf(t, chainApp, ctx, pc20SourceAsset, recipient)

	castPC20SettlementVotes(t, ctx, chainApp, vals, coreVals, utxId, ob, false, "")

	utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, utxId)
	require.NoError(t, err)
	require.True(t, found)
	got := utx.OutboundTx[0]
	require.Equal(t, uexecutortypes.Status_REVERTED, got.OutboundStatus, "revertExport succeeded -> REVERTED, not ABORTED")
	require.NotNil(t, got.PcRevertExecution)
	require.Equal(t, "SUCCESS", got.PcRevertExecution.Status)

	post := erc20BalanceOf(t, chainApp, ctx, pc20SourceAsset, recipient)
	require.Equal(t, amount, new(big.Int).Sub(post, pre), "revertExport must transfer the locked amount to the revert recipient")
}

// TestPC20Roundtrip_UnlockReplayGuard proves the VaultPC20 isExecuted[subTxId] replay
// guard holds through the chain's CallVaultPC20Unlock: the same subTxId twice releases
// funds only once.
func TestPC20Roundtrip_UnlockReplayGuard(t *testing.T) {
	chainApp, ctx := pc20AppWithOutbound(t, true)
	deployUniversalCorePC20(t, chainApp, ctx)
	vault := registerAndDeployVaultPC20(t, chainApp, ctx)

	amount := big.NewInt(1000)
	fundVaultForPC20(t, chainApp, ctx, vault, pc20SourceAsset, new(big.Int).Mul(amount, big.NewInt(2)))

	recipient := common.HexToAddress("0x000000000000000000000000000000000000bEEF")
	subTxId := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ab")

	resp1, err := chainApp.UexecutorKeeper.CallVaultPC20Unlock(ctx, subTxId, pc20SourceAsset, recipient, amount)
	require.NoError(t, err)
	require.False(t, resp1.Failed(), "first unlock reverted: %s", resp1.VmError)
	require.Equal(t, amount, erc20BalanceOf(t, chainApp, ctx, pc20SourceAsset, recipient))

	resp2, err := chainApp.UexecutorKeeper.CallVaultPC20Unlock(ctx, subTxId, pc20SourceAsset, recipient, amount)
	if err == nil {
		require.True(t, resp2.Failed(), "replayed unlock must revert (isExecuted guard)")
	}
	require.Equal(t, amount, erc20BalanceOf(t, chainApp, ctx, pc20SourceAsset, recipient), "replay must not transfer again")
}
