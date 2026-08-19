package integrationtest

import (
	"math/big"
	"os"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// Storage slots of UniversalCallback, from
// `forge inspect UniversalCallback storageLayout`.
//
// Seeding storage is how a test reaches a PENDING request without standing up
// UniversalCore, a fee schedule and a callback-target contract just to call
// requestExternalReadSelf. seedPendingRead reads the result back through the
// contract's own getters, so if any of this drifts the test fails loudly rather
// than quietly exercising the wrong state.
const (
	slotStatus        = 2 // mapping(uint256 => RequestStatus)
	slotPending       = 3 // mapping(uint256 => PendingRead)
	slotTotalEscrowed = 6 // uint256
)

// PendingRead packs into 4 slots:
//
//	+0  callbackTarget (20) | callbackSelector (4) | callbackGasLimit (8)
//	+1  originalFunder (20) | expiryHeight (8)
//	+2  revertRecipient (20)
//	+3  callbackBudget (32)
type pendingRead struct {
	callbackTarget   common.Address
	callbackSelector [4]byte
	callbackGasLimit uint64
	originalFunder   common.Address
	expiryHeight     uint64
	revertRecipient  common.Address
	callbackBudget   *big.Int
}

// mappingSlot is keccak256(pad32(key) ++ pad32(slot)), Solidity's layout for
// mapping(uint256 => _).
func mappingSlot(key *big.Int, slot int64) common.Hash {
	var buf []byte
	buf = append(buf, common.BigToHash(key).Bytes()...)
	buf = append(buf, common.BigToHash(big.NewInt(slot)).Bytes()...)
	return crypto.Keccak256Hash(buf)
}

func slotPlus(base common.Hash, n int64) common.Hash {
	return common.BigToHash(new(big.Int).Add(base.Big(), big.NewInt(n)))
}

// packed builds a 32-byte word from little-end-first fields, matching how Solidity
// packs a struct slot: the first-declared field occupies the low-order bytes.
func packed(parts ...[]byte) common.Hash {
	var w [32]byte
	off := 0
	for _, p := range parts {
		copy(w[32-off-len(p):32-off], p)
		off += len(p)
	}
	return w
}

func u64bytes(v uint64) []byte {
	return common.BigToHash(new(big.Int).SetUint64(v)).Bytes()[24:]
}

func seedPendingRead(
	t *testing.T, chainApp *app.ChainApp, ctx sdk.Context,
	contract common.Address, requestID *big.Int, p pendingRead,
) {
	t.Helper()
	k := chainApp.EVMKeeper

	k.SetState(ctx, contract, mappingSlot(requestID, slotStatus),
		common.BigToHash(big.NewInt(1)).Bytes()) // PENDING

	base := mappingSlot(requestID, slotPending)
	k.SetState(ctx, contract, base, packed(
		p.callbackTarget.Bytes(), p.callbackSelector[:], u64bytes(p.callbackGasLimit)).Bytes())
	k.SetState(ctx, contract, slotPlus(base, 1), packed(
		p.originalFunder.Bytes(), u64bytes(p.expiryHeight)).Bytes())
	k.SetState(ctx, contract, slotPlus(base, 2), packed(
		p.revertRecipient.Bytes()).Bytes())
	k.SetState(ctx, contract, slotPlus(base, 3), common.BigToHash(p.callbackBudget).Bytes())

	// escrow must cover the budget or reportCallbackGas underflows on `-=`
	k.SetState(ctx, contract, common.BigToHash(big.NewInt(slotTotalEscrowed)),
		common.BigToHash(p.callbackBudget).Bytes())

	assertSeedReadBack(t, chainApp, ctx, contract, requestID, p)
}

// assertSeedReadBack proves the slot arithmetic above by asking the contract what
// it thinks it holds. Without this the whole test could pass against zeroed state.
func assertSeedReadBack(
	t *testing.T, chainApp *app.ChainApp, ctx sdk.Context,
	contract common.Address, requestID *big.Int, want pendingRead,
) {
	t.Helper()
	viewABI := loadViewABI(t)

	status := staticCall(t, chainApp, ctx, viewABI, contract, "statusOf", requestID)
	require.Equal(t, uint8(1), status[0].(uint8), "seeded status must read back as PENDING")

	got := staticCall(t, chainApp, ctx, viewABI, contract, "getPendingRead", requestID)
	out := got[0].(struct {
		CallbackTarget   common.Address `json:"callbackTarget"`
		CallbackSelector [4]byte        `json:"callbackSelector"`
		CallbackGasLimit uint64         `json:"callbackGasLimit"`
		OriginalFunder   common.Address `json:"originalFunder"`
		ExpiryHeight     uint64         `json:"expiryHeight"`
		RevertRecipient  common.Address `json:"revertRecipient"`
		CallbackBudget   *big.Int       `json:"callbackBudget"`
	})
	require.Equal(t, want.callbackTarget, out.CallbackTarget, "callbackTarget slot")
	require.Equal(t, want.callbackSelector, out.CallbackSelector, "callbackSelector slot")
	require.Equal(t, want.callbackGasLimit, out.CallbackGasLimit, "callbackGasLimit slot")
	require.Equal(t, want.originalFunder, out.OriginalFunder, "originalFunder slot")
	require.Equal(t, want.expiryHeight, out.ExpiryHeight, "expiryHeight slot")
	require.Equal(t, want.revertRecipient, out.RevertRecipient, "revertRecipient slot")
	require.Zero(t, want.callbackBudget.Cmp(out.CallbackBudget), "callbackBudget slot")
}

// staticCall runs a view function and unpacks its outputs.
func staticCall(
	t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, parsed abi.ABI,
	contract common.Address, method string, args ...interface{},
) []interface{} {
	t.Helper()
	data, err := parsed.Pack(method, args...)
	require.NoError(t, err)

	caller := common.HexToAddress("0x000000000000000000000000000000000000bEEF")
	acc := chainApp.AccountKeeper.NewAccountWithAddress(ctx, sdk.AccAddress(caller.Bytes()))
	chainApp.AccountKeeper.SetAccount(ctx, acc)

	res, err := chainApp.EVMKeeper.DerivedEVMCallWithData(
		ctx, caller, &contract, data,
		false /* commit: a view must not persist */, false, false,
		big.NewInt(0), big.NewInt(500_000), nil,
	)
	require.NoError(t, err, "%s reverted: %v", method, res)
	out, err := parsed.Unpack(method, res.Ret)
	require.NoError(t, err)
	return out
}

// The happy path, end to end, against the real deployed contract: a PENDING
// request is fulfilled, settled, and its consumed budget destroyed.
//
// Every other test in this package makes a call that reverts, so until this one
// existed nothing had ever executed fulfillExternalCallback or reportCallbackGas
// successfully — the settle flow was verified only by reading Solidity.
func TestLifecycle_FulfilSettleBurn_AgainstRealContract(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	contract := utils.SetupUniversalCallback(t, chainApp, ctx)
	k := chainApp.UcallbackKeeper
	bank := chainApp.BankKeeper

	requestID := big.NewInt(0x5eed)
	budget := big.NewInt(3_000_000_000_000_000)

	// An EOA target is enough: fulfillExternalCallback only needs the low-level call
	// to return true, and a call to a codeless address does. What runs inside the
	// callback is the app's business, not ours.
	target := provisionEOA(t, chainApp, ctx, "0x00000000000000000000000000000000000c0FFE")
	recipient := provisionEOA(t, chainApp, ctx, "0x00000000000000000000000000000000000Fee00")

	seedPendingRead(t, chainApp, ctx, contract, requestID, pendingRead{
		callbackTarget:   target,
		callbackSelector: [4]byte{0xaa, 0xbb, 0xcc, 0xdd},
		callbackGasLimit: 200_000,
		originalFunder:   target,
		expiryHeight:     uint64(ctx.BlockHeight()) + 1000,
		revertRecipient:  recipient,
		callbackBudget:   budget,
	})

	// the escrow the request is backed by must actually exist on the contract
	fund(t, chainApp, ctx, sdk.AccAddress(contract.Bytes()), budget)

	supplyBefore := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	recipientBefore := bank.GetBalance(ctx, sdk.AccAddress(recipient.Bytes()), pchaintypes.BaseDenom).Amount

	// --- fulfil, through our own keeper path -----------------------------------
	res, err := k.CallFulfillExternalCallback(ctx, hexID(requestID), &ucallbacktypes.ReadResult{
		Status:     ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS,
		ResultData: []byte{0x01, 0x02},
	})
	require.NoError(t, err, "fulfillExternalCallback must succeed against the real contract")
	require.Empty(t, res.VmError)

	viewABI := loadViewABI(t)
	require.Equal(t, uint8(2), // EXECUTED
		staticCall(t, chainApp, ctx, viewABI, contract, "statusOf", requestID)[0].(uint8),
		"a fulfilled request must sit in EXECUTED awaiting the gas report")

	// --- settle ----------------------------------------------------------------
	cost, err := k.CallbackCost(ctx, res.GasUsed)
	require.NoError(t, err)
	if cost.Cmp(budget) > 0 {
		cost = budget
	}

	repRes, err := k.CallReportCallbackGas(ctx, hexID(requestID), cost)
	require.NoError(t, err, "reportCallbackGas must succeed")
	require.Empty(t, repRes.VmError)

	// The contract returns what it actually clamped the report to. Our own figure is
	// recomputed rather than read back, which is only safe while the two clamps
	// agree -- so assert they do. If the contract ever changes what it retains, this
	// is what catches it.
	burned := new(big.Int).SetBytes(repRes.Ret)
	// Guard against a vacuous pass: a zero base fee would make cost, burned and the
	// supply delta all zero, and every assertion below would hold trivially.
	require.Positive(t, burned.Sign(), "the callback must have cost something to burn")
	require.Less(t, burned.Cmp(budget), 1, "burn cannot exceed the escrowed budget")
	require.Zero(t, burned.Cmp(cost),
		"our burn figure must equal the contract's `burned`: got %s, contract %s",
		cost, burned)

	require.Equal(t, uint8(3), // SETTLED
		staticCall(t, chainApp, ctx, viewABI, contract, "statusOf", requestID)[0].(uint8))

	// the unspent remainder must have gone back to the revert recipient
	refund := new(big.Int).Sub(budget, burned)
	recipientAfter := bank.GetBalance(ctx, sdk.AccAddress(recipient.Bytes()), pchaintypes.BaseDenom).Amount
	require.Equal(t, sdkmath.NewIntFromBigInt(refund), recipientAfter.Sub(recipientBefore),
		"the unburned budget must be refunded to revertRecipient")

	// --- burn ------------------------------------------------------------------
	require.NoError(t, k.TakeAndBurn(ctx, burned))

	supplyAfter := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	require.Equal(t, sdkmath.NewIntFromBigInt(burned), supplyBefore.Sub(supplyAfter),
		"total supply must fall by exactly what the contract said was burned")

	// and the contract must be left holding nothing for this request
	require.True(t,
		bank.GetBalance(ctx, sdk.AccAddress(contract.Bytes()), pchaintypes.BaseDenom).Amount.IsZero(),
		"refund + burn must together drain the request's escrow")
}

func hexID(id *big.Int) string { return common.BigToHash(id).Hex() }

func fund(t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, to sdk.AccAddress, amt *big.Int) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, sdkmath.NewIntFromBigInt(amt)))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, evmtypes.ModuleName, coins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx, evmtypes.ModuleName, to, coins))
}

// loadViewABI parses the read-only functions used to inspect contract state.
//
// Lifted from the compiled artifact rather than hand-written: getPendingRead
// returns a 7-field packed struct, and transcribing that by hand is how the
// ReadRequested fragment ended up with its fields in the wrong order.
//
//	push-chain-core-contracts, out/UniversalCallback.sol/UniversalCallback.json
func loadViewABI(t *testing.T) abi.ABI {
	t.Helper()
	raw, err := os.ReadFile("testdata/universal_callback_views.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(strings.NewReader(string(raw)))
	require.NoError(t, err)
	return parsed
}
