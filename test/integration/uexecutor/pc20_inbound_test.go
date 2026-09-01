package integrationtest

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// pc20InboundWrapper is an external PC20 wrapper address that Push never mapped —
// so getPC20Source (on the un-wired UniversalCore) can't resolve a source for it.
var pc20InboundWrapper = common.HexToAddress("0x000000000000000000000000000000000000face")

// encodePC20UserPayload ABI-encodes a minimal (but decodable) UniversalPayload,
// the "user payload" that rides behind the PC20 selector on a return.
func encodePC20UserPayload(t *testing.T, to common.Address) string {
	t.Helper()
	tupleType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
		{Name: "gasLimit", Type: "uint256"},
		{Name: "maxFeePerGas", Type: "uint256"},
		{Name: "maxPriorityFeePerGas", Type: "uint256"},
		{Name: "nonce", Type: "uint256"},
		{Name: "deadline", Type: "uint256"},
		{Name: "vType", Type: "uint8"},
	})
	require.NoError(t, err)

	type up struct {
		To                   common.Address
		Value                *big.Int
		Data                 []byte
		GasLimit             *big.Int
		MaxFeePerGas         *big.Int
		MaxPriorityFeePerGas *big.Int
		Nonce                *big.Int
		Deadline             *big.Int
		VType                uint8
	}
	packed, err := abi.Arguments{{Type: tupleType}}.Pack(up{
		To:                   to,
		Value:                big.NewInt(0),
		Data:                 []byte{},
		GasLimit:             big.NewInt(21000),
		MaxFeePerGas:         big.NewInt(1000000000),
		MaxPriorityFeePerGas: big.NewInt(200000000),
		Nonce:                big.NewInt(0),
		Deadline:             big.NewInt(9999999999),
		VType:                1,
	})
	require.NoError(t, err)
	return "0x" + hex.EncodeToString(packed)
}

// pc20SelectorPrefixed returns a raw inbound payload with the PC20 selector in
// front of a decodable user payload — the shape the external gateway emits.
func pc20SelectorPrefixed(t *testing.T) string {
	return "0x" + uexecutortypes.PC20Selector + strings.TrimPrefix(
		encodePC20UserPayload(t, common.HexToAddress("0x000000000000000000000000000000000000beef")), "0x")
}

func votePC20InboundToQuorum(t *testing.T, ctx sdk.Context, chainApp *app.ChainApp, vals []string, coreVals []stakingtypes.Validator, inbound *uexecutortypes.Inbound) {
	t.Helper()
	for i := 0; i < 3; i++ {
		valAddr, err := sdk.ValAddressFromBech32(coreVals[i].OperatorAddress)
		require.NoError(t, err)
		require.NoError(t, utils.ExecVoteInbound(t, ctx, chainApp, vals[i], sdk.AccAddress(valAddr).String(), inbound))
	}
}

func hasInboundRevert(utx uexecutortypes.UniversalTx) *uexecutortypes.OutboundTx {
	for _, ob := range utx.OutboundTx {
		if ob.TxType == uexecutortypes.TxType_INBOUND_REVERT {
			return ob
		}
	}
	return nil
}

// TestPC20Inbound_Routing drives PC20 returns (external chain -> Push) end to end
// through the vote/finalize/execute path. The PC20-aware UniversalCore
// (getPC20Source) and VaultPC20 (unlock) are not wired into this harness, so the
// funds step can't complete — but the routing decision (is_pc20 detection + strip,
// unlock vs deposit, revert vs no-revert) is fully observable. The happy round-trip
// (unlock releases funds, stripped payload executes) is gated on those contract
// bytecodes and covered separately once they're deployed.
func TestPC20Inbound_Routing(t *testing.T) {
	t.Run("non-CEA PC20 return routes to unlock and reverts when source is unresolvable", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundInitiatedOutboundTest(t, 4)

		inbound.AssetAddr = pc20InboundWrapper.Hex()
		inbound.IsCEA = false
		inbound.UniversalPayload = nil
		inbound.RawPayload = pc20SelectorPrefixed(t)

		votePC20InboundToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound))
		require.NoError(t, err)
		require.True(t, found)

		require.True(t, utx.InboundTx.IsPc20, "PC20 selector must be detected and flagged")

		rev := hasInboundRevert(utx)
		require.NotNil(t, rev, "an unresolvable PC20 return must produce an INBOUND_REVERT (wrapper re-mint)")
		// The revert re-mints the wrapper: carries the wrapper as external asset and
		// must NOT be flagged is_pc20 (a failed INBOUND_REVERT must never route into
		// handleFailedOutbound's revertExport, which is only for a failed export).
		require.Equal(t, pc20InboundWrapper.Hex(), rev.ExternalAssetAddr)
		require.False(t, rev.IsPc20)
	})

	t.Run("CEA PC20 return that can't resolve source does NOT revert", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, ueaAddr := setupInboundInitiatedOutboundTest(t, 4)

		// CEA inbound: recipient is explicit (a deployed UEA). CEA failures never
		// create an INBOUND_REVERT — the PC20 unlock failure must honour that.
		inbound.AssetAddr = pc20InboundWrapper.Hex()
		inbound.IsCEA = true
		inbound.Recipient = ueaAddr.Hex()
		inbound.UniversalPayload = nil
		inbound.RawPayload = pc20SelectorPrefixed(t)

		votePC20InboundToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound))
		require.NoError(t, err)
		require.True(t, found)

		require.True(t, utx.InboundTx.IsPc20, "PC20 selector must be detected even on a CEA return")
		require.Nil(t, hasInboundRevert(utx), "a CEA PC20 return must NOT create an INBOUND_REVERT")
	})

	t.Run("un-prefixed (PRC20) inbound is not flagged pc20 and does not revert", func(t *testing.T) {
		chainApp, ctx, vals, inbound, coreVals, _ := setupInboundInitiatedOutboundTest(t, 4)

		// A raw payload with no PC20 selector: the tolerant router leaves it alone,
		// is_pc20 stays false, and the funds step takes the PRC20 deposit path
		// (registered token from setup) — diverging from the PC20 return above.
		inbound.UniversalPayload = nil
		inbound.RawPayload = encodePC20UserPayload(t, common.HexToAddress("0x000000000000000000000000000000000000beef"))

		votePC20InboundToQuorum(t, ctx, chainApp, vals, coreVals, inbound)

		utx, found, err := chainApp.UexecutorKeeper.GetUniversalTx(ctx, uexecutortypes.GetInboundUniversalTxKey(*inbound))
		require.NoError(t, err)
		require.True(t, found)

		require.False(t, utx.InboundTx.IsPc20, "an un-prefixed payload must not be flagged pc20")
		require.Nil(t, hasInboundRevert(utx), "PRC20 deposit path should complete without an INBOUND_REVERT")
	})
}
