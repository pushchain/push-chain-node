package integrationtest

import (
	"fmt"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	"github.com/pushchain/push-chain-node/test/utils"
)

// expireGasLimit and reportGasLimit are fixed, not estimated: _refund swallows a
// failed push, so the call returns cleanly whether or not the refund landed and
// estimation converges on a limit where it never does. A too-low limit strands the
// refund in the admin-sweepable pool instead of reverting.
//
// This measures what the two paths actually cost and fails if a constant stops
// covering one. Run with -v for the table.
const (
	measuredExpireGasLimit = 150_000 // keep in sync with keeper.expireGasLimit
	measuredReportGasLimit = 150_000 // keep in sync with keeper.reportGasLimit
)

// recipientKind is a revertRecipient shape and the runtime code behind it.
var recipientKinds = []struct {
	name string
	code string // runtime bytecode, hex without 0x; empty = codeless EOA
}{
	// Cheapest possible: a codeless address, value transfer only.
	{"eoa", ""},
	// Accepts value, does nothing: STOP.
	{"contract-noop", "00"},
	// Bookkeeping in receive(): PUSH1 1, PUSH1 0, SSTORE, STOP. A cold SSTORE is
	// 22.1k, which is the shape a real app's receive() tends to have.
	{"contract-sstore", "600160005500"},
}

func recipientAddr(i int) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x00000000000000000000000000000000000Fee%02d", i))
}

func provisionRecipient(
	t *testing.T, chainApp *app.ChainApp, ctx sdk.Context, i int, code string,
) common.Address {
	t.Helper()
	addr := recipientAddr(i)
	if code == "" {
		return provisionEOA(t, chainApp, ctx, addr.Hex())
	}
	return utils.DeployContract(t, chainApp, ctx, addr, code)
}

// seedRead writes a request at the given status. status 1 = PENDING (what
// expireExternalRead requires), 2 = EXECUTED (what reportCallbackGas requires).
func seedRead(
	t *testing.T, chainApp *app.ChainApp, ctx sdk.Context,
	contract common.Address, requestID *big.Int, p pendingRead, status int64,
) {
	t.Helper()
	seedPendingRead(t, chainApp, ctx, contract, requestID, p)
	if status != 1 {
		chainApp.EVMKeeper.SetState(ctx, contract, mappingSlot(requestID, slotStatus),
			common.BigToHash(big.NewInt(status)).Bytes())
	}
}

func TestSettleGas_AgainstRealContract(t *testing.T) {
	type row struct {
		path, recipient string
		gasUsed, limit  uint64
	}
	var rows []row

	for i, rk := range recipientKinds {
		for _, path := range []string{"reportCallbackGas", "expireExternalRead"} {
			chainApp, ctx, _ := utils.SetAppWithValidators(t)
			contract := utils.SetupUniversalCallback(t, chainApp, ctx)
			k := chainApp.UcallbackKeeper

			requestID := big.NewInt(int64(0x7e500 + i*8))
			budget := big.NewInt(3_000_000_000_000_000)
			target := provisionEOA(t, chainApp, ctx, "0x00000000000000000000000000000000000c0FFE")
			recipient := provisionRecipient(t, chainApp, ctx, i, rk.code)

			// expire needs PENDING and a deadline already behind us; report needs
			// EXECUTED and its deadline is irrelevant.
			status := int64(2)
			expiry := uint64(ctx.BlockHeight()) + 1000
			if path == "expireExternalRead" {
				status = 1
				expiry = 1
			}

			seedRead(t, chainApp, ctx, contract, requestID, pendingRead{
				callbackTarget:   target,
				callbackSelector: [4]byte{0xaa, 0xbb, 0xcc, 0xdd},
				callbackGasLimit: 200_000,
				originalFunder:   target,
				expiryHeight:     expiry,
				revertRecipient:  recipient,
				callbackBudget:   budget,
			}, status)
			fund(t, chainApp, ctx, sdk.AccAddress(contract.Bytes()), budget)

			var (
				res   *evmtypes.MsgEthereumTxResponse
				err   error
				limit uint64
			)
			if path == "reportCallbackGas" {
				res, err = k.CallReportCallbackGas(ctx, hexID(requestID), big.NewInt(1_000_000))
				limit = measuredReportGasLimit
			} else {
				res, err = k.CallExpireExternalRead(ctx, hexID(requestID))
				limit = measuredExpireGasLimit
			}
			require.NoError(t, err, "%s [%s]", path, rk.name)
			require.Empty(t, res.VmError, "%s [%s] must not revert", path, rk.name)
			rows = append(rows, row{path, rk.name, res.GasUsed, limit})
		}
	}

	t.Log("path                 recipient          gasUsed    limit   headroom")
	for _, r := range rows {
		t.Logf("%-20s %-18s %8d %8d %+9d", r.path, r.recipient, r.gasUsed, r.limit,
			int64(r.limit)-int64(r.gasUsed))
		require.Less(t, r.gasUsed, r.limit,
			"%s with a %s recipient costs %d gas but the limit is %d -- raise the constant",
			r.path, r.recipient, r.gasUsed, r.limit)
	}
}
