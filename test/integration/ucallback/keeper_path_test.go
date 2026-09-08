package integrationtest

import (
	"context"
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	ucallbackkeeper "github.com/pushchain/push-chain-node/x/ucallback/keeper"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// readFixture is one request that exists in BOTH places it has to exist: seeded
// into the contract's storage, and recorded in the keeper. Production gets there by
// ingesting an event; these tests get there directly so the settle and expiry paths
// can be exercised without standing up UniversalCore and a fee schedule.
type readFixture struct {
	id        *big.Int
	idHex     string
	budget    *big.Int
	gasLimit  uint64
	expiry    uint64
	recipient common.Address
	contract  common.Address
}

func newReadFixture(
	t *testing.T, chainApp *app.ChainApp, ctx sdk.Context,
	id int64, budget *big.Int, gasLimit uint64, expiry uint64,
) readFixture {
	t.Helper()
	contract := utils.SetupUniversalCallback(t, chainApp, ctx)

	requestID := big.NewInt(id)
	target := provisionEOA(t, chainApp, ctx, "0x00000000000000000000000000000000000c0FFE")
	recipient := provisionEOA(t, chainApp, ctx, "0x00000000000000000000000000000000000Fee00")

	seedPendingRead(t, chainApp, ctx, contract, requestID, pendingRead{
		callbackTarget:   target,
		callbackSelector: [4]byte{0xaa, 0xbb, 0xcc, 0xdd},
		callbackGasLimit: gasLimit,
		originalFunder:   target,
		expiryHeight:     expiry,
		revertRecipient:  recipient,
		callbackBudget:   budget,
	})
	fund(t, chainApp, ctx, sdk.AccAddress(contract.Bytes()), budget)

	f := readFixture{
		id: requestID, idHex: hexID(requestID), budget: budget,
		gasLimit: gasLimit, expiry: expiry, recipient: recipient, contract: contract,
	}
	require.NoError(t, chainApp.UcallbackKeeper.SetUniversalRead(ctx, f.universalRead(
		ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, nil)))
	return f
}

func (f readFixture) universalRead(
	status ucallbacktypes.UniversalReadStatus, result *ucallbacktypes.ReadResult,
) ucallbacktypes.UniversalRead {
	return ucallbacktypes.UniversalRead{
		Id:     f.idHex,
		Status: status,
		Result: result,
		Request: &ucallbacktypes.ReadRequest{
			RequestId:         f.idHex,
			DestinationChain:  "eip155:11155111",
			ExpiryBlockHeight: f.expiry,
			CallbackBudget:    f.budget.String(),
			CallbackGasLimit:  f.gasLimit,
			RevertRecipient:   f.recipient.Hex(),
			RequestedTxHash:   "0xabc",
		},
	}
}

func okResult() *ucallbacktypes.ReadResult {
	return &ucallbacktypes.ReadResult{
		Status:     ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS,
		ResultData: []byte{0x01, 0x02},
	}
}

// FulfilRead is the whole settle path: affordability gate, the contract call,
// outcome classification, status transition, gas report and burn. The e2e test
// calls CallFulfillExternalCallback directly, so none of the logic wrapping it had
// ever run against a real EVM.
func TestFulfilRead_SettlesAndBurns(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper
	bank := chainApp.BankKeeper

	f := newReadFixture(t, chainApp, ctx, 0x11, big.NewInt(3_000_000_000_000_000), 200_000,
		uint64(ctx.BlockHeight())+1000)

	supplyBefore := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	require.NoError(t, k.FulfilRead(ctx, f.universalRead(
		ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, okResult())))

	got, ok := k.GetUniversalRead(ctx, f.idHex)
	require.True(t, ok)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, got.Status,
		"a successful callback must leave the read FULFILLED; err=%q", got.ErrorMsg)
	require.Empty(t, got.ErrorMsg, "a clean fulfilment records no error")

	// the contract agrees it is finished
	require.Equal(t, uint8(3), // SETTLED
		staticCall(t, chainApp, ctx, loadViewABI(t), f.contract, "statusOf", f.id)[0].(uint8))

	// and value was actually destroyed
	supplyAfter := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount
	require.True(t, supplyAfter.LT(supplyBefore), "fulfilment must burn the consumed budget")

	// both EVM calls are recorded for offchain tracing
	require.Len(t, got.PcTx, 2, "fulfil and report must each leave a PcTx")

	// settled reads leave the in-flight index, so the sweeper will not touch it
	require.NoError(t, chainApp.UcallbackKeeper.SweepExpired(
		ctx.WithBlockHeight(int64(f.expiry)+1)))
	still, _ := k.GetUniversalRead(ctx, f.idHex)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED, still.Status,
		"a settled read must not be re-expired by the sweeper")
}

// A request whose budget cannot cover its declared gas limit must not be executed:
// it stays in flight so the sweeper refunds the funder in full.
func TestFulfilRead_UnaffordableStaysInFlight(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	// 1 wei of budget against a 1M gas limit
	f := newReadFixture(t, chainApp, ctx, 0x12, big.NewInt(1), 1_000_000,
		uint64(ctx.BlockHeight())+1000)

	require.NoError(t, k.FulfilRead(ctx, f.universalRead(
		ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, okResult())))

	got, ok := k.GetUniversalRead(ctx, f.idHex)
	require.True(t, ok)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, got.Status,
		"an unaffordable read must keep its status so expiry still owns it")
	require.Equal(t, ucallbackkeeper.ErrBudgetTooSmall, got.ErrorMsg)
	require.Empty(t, got.PcTx, "nothing may be sent to the contract")

	// the contract must still see it as PENDING, i.e. refundable
	require.Equal(t, uint8(1),
		staticCall(t, chainApp, ctx, loadViewABI(t), f.contract, "statusOf", f.id)[0].(uint8))
}

// The sweep is the path that runs unattended every block, so a failure here is a
// chain-wide event rather than one bad request. It must expire the request on the
// contract, refund the funder, and retire our record.
func TestSweepExpired_RefundsThroughTheRealContract(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper
	bank := chainApp.BankKeeper

	expiry := uint64(ctx.BlockHeight()) + 5
	budget := big.NewInt(2_000_000_000_000_000)
	f := newReadFixture(t, chainApp, ctx, 0x21, budget, 200_000, expiry)

	recipientBefore := bank.GetBalance(ctx,
		sdk.AccAddress(f.recipient.Bytes()), pchaintypes.BaseDenom).Amount
	supplyBefore := bank.GetSupply(ctx, pchaintypes.BaseDenom).Amount

	// before the deadline the sweep must leave it strictly alone
	require.NoError(t, k.SweepExpired(ctx.WithBlockHeight(int64(expiry)-1)))
	early, _ := k.GetUniversalRead(ctx, f.idHex)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, early.Status,
		"a live request must survive the sweep")

	// at the deadline it must be retired
	swept := ctx.WithBlockHeight(int64(expiry))
	require.NoError(t, k.SweepExpired(swept))

	got, ok := k.GetUniversalRead(ctx, f.idHex)
	require.True(t, ok)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, got.Status,
		"err=%q", got.ErrorMsg)

	require.Equal(t, uint8(4), // EXPIRED
		staticCall(t, chainApp, swept, loadViewABI(t), f.contract, "statusOf", f.id)[0].(uint8),
		"the contract must agree the request is expired")

	// expiry refunds the WHOLE budget — nothing is burned on this path
	recipientAfter := bank.GetBalance(swept,
		sdk.AccAddress(f.recipient.Bytes()), pchaintypes.BaseDenom).Amount
	require.Equal(t, sdkmath.NewIntFromBigInt(budget), recipientAfter.Sub(recipientBefore),
		"an expired request must refund the funder in full")
	require.Equal(t, supplyBefore, bank.GetSupply(swept, pchaintypes.BaseDenom).Amount,
		"expiry must not burn anything")

	// idempotent: running again must not double-refund
	require.NoError(t, k.SweepExpired(swept.WithBlockHeight(int64(expiry)+1)))
	require.Equal(t, recipientAfter, bank.GetBalance(swept,
		sdk.AccAddress(f.recipient.Bytes()), pchaintypes.BaseDenom).Amount,
		"a second sweep must be a no-op")
}

// EndBlock is what actually drives the sweep on a live chain. Exercising
// SweepExpired directly would not prove the module is wired into the block cycle.
func TestEndBlock_DrivesTheSweep(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	expiry := uint64(ctx.BlockHeight()) + 3
	f := newReadFixture(t, chainApp, ctx, 0x22, big.NewInt(1_500_000_000_000_000), 200_000, expiry)

	require.NoError(t, chainApp.ModuleManager.Modules[ucallbacktypes.ModuleName].(interface {
		EndBlock(context.Context) error
	}).EndBlock(ctx.WithBlockHeight(int64(expiry))))

	got, _ := k.GetUniversalRead(ctx, f.idHex)
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED, got.Status,
		"EndBlock must retire an overdue read")
}
