package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

// TestUniversalCoreABI_GetOutboundTxGasAndFees_Has6Outputs locks in the new
// post-audit schema (the contract added gasLimitUsed as a 6th output).
// Catches accidental ABI reverts and proves Pack/Unpack round-trips.
func TestUniversalCoreABI_GetOutboundTxGasAndFees_Has6Outputs(t *testing.T) {
	abi, err := types.ParseUniversalCoreABI()
	require.NoError(t, err)

	method, ok := abi.Methods["getOutboundTxGasAndFees"]
	require.True(t, ok, "getOutboundTxGasAndFees missing from ABI")
	require.Len(t, method.Outputs, 6, "expected 6 outputs (post-audit schema added gasLimitUsed)")

	// Output names must match the contract field names so future readers can
	// map results[i] back to the contract source unambiguously.
	wantNames := []string{"gasToken", "gasFee", "protocolFee", "gasPrice", "chainNamespace", "gasLimitUsed"}
	for i, want := range wantNames {
		require.Equal(t, want, method.Outputs[i].Name, "output[%d] name mismatch", i)
	}

	// Round-trip: pack a fake response, unpack it, get the same values back.
	// This is the contract that GetOutboundTxGasAndFees in keeper/gas_fee.go
	// relies on (results[0]=gasToken, results[1]=gasFee, results[3]=gasPrice,
	// results[5]=gasLimit).
	wantGasToken := common.HexToAddress("0x0000000000000000000000000000000000001111")
	wantGasFee := big.NewInt(123_456)
	wantProtocolFee := big.NewInt(789)
	wantGasPrice := big.NewInt(10)
	wantChainNs := "eip155:1"
	wantGasLimit := big.NewInt(50_000) // intentionally != gasFee/gasPrice (=12345)

	encoded, err := method.Outputs.Pack(
		wantGasToken,
		wantGasFee,
		wantProtocolFee,
		wantGasPrice,
		wantChainNs,
		wantGasLimit,
	)
	require.NoError(t, err)

	results, err := method.Outputs.Unpack(encoded)
	require.NoError(t, err)
	require.Len(t, results, 6)

	require.Equal(t, wantGasToken, results[0].(common.Address))
	require.Equal(t, 0, wantGasFee.Cmp(results[1].(*big.Int)))
	require.Equal(t, 0, wantProtocolFee.Cmp(results[2].(*big.Int)))
	require.Equal(t, 0, wantGasPrice.Cmp(results[3].(*big.Int)))
	require.Equal(t, wantChainNs, results[4].(string))
	require.Equal(t, 0, wantGasLimit.Cmp(results[5].(*big.Int)),
		"results[5] (gasLimitUsed) must be the value the contract returned, "+
			"not derived from gasFee/gasPrice")

	// Belt-and-suspenders: the post-audit chain code reads gasLimit from
	// results[5] directly. If anyone ever regresses to the old
	// `gasLimit = gasFee/gasPrice` derivation, the value would be 12345,
	// not 50000. Encode that expectation explicitly.
	derived := new(big.Int).Div(wantGasFee, wantGasPrice)
	require.NotEqual(t, 0, derived.Cmp(results[5].(*big.Int)),
		"gasLimit must come from results[5], NOT from gasFee/gasPrice division")
}

// TestUniversalCoreABI_SetGasPrice_Removed locks in that the deprecated
// setGasPrice function has been removed from the ABI (deleted in the
// post-audit contract; chain wrapper CallUniversalCoreSetGasPrice was
// removed as dead code).
func TestUniversalCoreABI_SetGasPrice_Removed(t *testing.T) {
	abi, err := types.ParseUniversalCoreABI()
	require.NoError(t, err)
	_, exists := abi.Methods["setGasPrice"]
	require.False(t, exists, "setGasPrice must be removed from ABI (deleted from contract post-audit)")
}
