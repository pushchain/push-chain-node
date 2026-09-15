package integrationtest

import (
	"context"
	"math/big"
	"testing"
	"time"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// spyUCallback counts IngestReadRequests calls and forwards to the real keeper.
type spyUCallback struct {
	inner    uexecutortypes.UCallbackKeeper
	calls    int
	receipts []*evmtypes.MsgEthereumTxResponse
}

func (s *spyUCallback) IngestReadRequests(ctx context.Context, receipt *evmtypes.MsgEthereumTxResponse) error {
	s.calls++
	s.receipts = append(s.receipts, receipt)
	if s.inner == nil {
		return nil
	}
	return s.inner.IngestReadRequests(ctx, receipt)
}

// N1: a CEA inbound whose recipient is a contract runs through
// CallExecuteUniversalTx. Derived calls never fire the EVM post-tx hook, so
// without an explicit hand-off a ReadRequested emitted by that contract is
// recorded nowhere and its escrowed budget is stranded.
//
// The keepers are invoked directly: app.UexecutorKeeper is held by value, so a
// spy installed on it is not seen by the msg server's own copy.
func TestReadIngestHandoff_BothDerivedPaths(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))

	spy := &spyUCallback{inner: chainApp.UcallbackKeeper}
	chainApp.UexecutorKeeper.SetUCallbackKeeper(spy)
	uek := chainApp.UexecutorKeeper

	moduleAddr, _ := uek.GetUeModuleAddress(ctx)
	recipient := deployMockRecipientContract(t, chainApp, ctx)

	var txId [32]byte
	copy(txId[:], common.FromHex("0x01"))

	t.Run("CEA contract path hands its receipt to x/ucallback", func(t *testing.T) {
		before := spy.calls

		res, err := uek.CallExecuteUniversalTx(
			ctx, recipient, "eip155:11155111",
			common.FromHex("0x1111111111111111111111111111111111111111"),
			common.FromHex("0xdeadbeef"), big.NewInt(0),
			utils.GetDefaultAddresses().PRC20USDCAddr, txId,
		)
		require.NoError(t, err)
		require.NotNil(t, res)

		require.Equal(t, before+1, spy.calls,
			"CallExecuteUniversalTx must hand its receipt to x/ucallback (N1)")
		require.Same(t, res, spy.receipts[len(spy.receipts)-1],
			"the receipt handed over must be the one the derived call produced")
	})

	t.Run("UEA path still hands its receipt over", func(t *testing.T) {
		before := spy.calls

		deployRes, err := uek.DeployUEAV2(ctx, moduleAddr, &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          utils.GetDefaultAddresses().DefaultTestAddr,
		})
		require.NoError(t, err)
		uea := common.BytesToAddress(deployRes.Ret)

		_, _ = uek.CallUEAExecutePayload(ctx, moduleAddr, uea, &uexecutortypes.UniversalPayload{
			To:                   recipient.Hex(),
			Value:                "0",
			Data:                 "",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "0",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(1),
		}, nil)

		require.Greater(t, spy.calls, before, "the UEA hand-off must be unaffected")
	})

	// The cached-ctx requirement holds by construction: both call sites pass a
	// CacheContext and the hand-off uses that same ctx. The commit/rollback
	// behaviour of that cache is covered by the CEA fee-atomicity tests.
}
