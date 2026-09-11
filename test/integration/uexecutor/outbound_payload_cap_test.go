package integrationtest

import (
	"math/big"
	"strings"
	"testing"

	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// gatewayOutboundLog builds a UniversalTxOutbound log carrying payloadBytes,
// matching what DecodeUniversalTxOutboundFromLog expects.
func gatewayOutboundLog(t *testing.T, prc20 common.Address, payloadBytes []byte) *evmtypes.Log {
	t.Helper()

	strT, _ := abi.NewType("string", "", nil)
	bytesT, _ := abi.NewType("bytes", "", nil)
	u256T, _ := abi.NewType("uint256", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	u8T, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{
		{Type: strT}, {Type: bytesT}, {Type: u256T}, {Type: addrT}, {Type: u256T},
		{Type: u256T}, {Type: bytesT}, {Type: u256T}, {Type: addrT}, {Type: u8T}, {Type: u256T},
	}
	target := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	data, err := args.Pack(
		"eip155:11155111", target.Bytes(), big.NewInt(1000000),
		common.Address{}, big.NewInt(500000), big.NewInt(21000),
		payloadBytes, big.NewInt(0), target, uint8(1), big.NewInt(1),
	)
	require.NoError(t, err)

	return &evmtypes.Log{
		Address: strings.ToLower(utils.GetDefaultAddresses().UniversalGatewayPCAddr.Hex()),
		Topics: []string{
			uexecutortypes.UniversalTxOutboundEventSig,
			common.HexToHash("0x01").Hex(),
			common.HexToHash("0x02").Hex(),
			common.BytesToHash(prc20.Bytes()).Hex(),
		},
		Data:  data,
		Index: 0,
	}
}

// F-2026-18146: the gateway payload is attacker-controlled and lands in state,
// so BuildOutboundsFromReceipt must reject an oversized one at admission.
func TestBuildOutboundsFromReceipt_RejectsOversizedPayload(t *testing.T) {
	prc20 := utils.GetDefaultAddresses().PRC20USDCAddr
	chainApp, ctx, _, _, _ := setupMulticallOutboundTest(t, 4)

	t.Run("a payload within the cap is accepted", func(t *testing.T) {
		receipt := &evmtypes.MsgEthereumTxResponse{
			Hash: "0xabc",
			Logs: []*evmtypes.Log{gatewayOutboundLog(t, prc20, []byte{0xde, 0xad, 0xbe, 0xef})},
		}
		outbounds, err := chainApp.UexecutorKeeper.BuildOutboundsFromReceipt(ctx, "utx-ok", receipt)
		require.NoError(t, err)
		require.Len(t, outbounds, 1)
	})

	t.Run("an oversized payload is rejected", func(t *testing.T) {
		// Hex-encoded, so half the cap in bytes is exactly at it; one more byte is over.
		oversized := make([]byte, uexecutortypes.MaxOutboundPayloadBytes/2)
		receipt := &evmtypes.MsgEthereumTxResponse{
			Hash: "0xabc",
			Logs: []*evmtypes.Log{gatewayOutboundLog(t, prc20, oversized)},
		}
		outbounds, err := chainApp.UexecutorKeeper.BuildOutboundsFromReceipt(ctx, "utx-big", receipt)
		require.Error(t, err)
		require.Contains(t, err.Error(), "payload too large")
		require.Empty(t, outbounds, "no outbound may be built from an oversized payload")
	})
}
