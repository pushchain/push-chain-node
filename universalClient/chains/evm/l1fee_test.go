package evm

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// receiptRPC serves eth_chainId plus a single receipt for any receipt lookup.
func receiptRPC(t *testing.T, receiptJSON string) *RPCClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		switch {
		case strings.Contains(string(body), "eth_chainId"):
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0xaa36a7"}`)) // 11155111
		case strings.Contains(string(body), "eth_getTransactionReceipt"):
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + receiptJSON + `}`))
		default:
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`))
		}
	}))
	t.Cleanup(server.Close)

	rc, err := NewRPCClient([]string{server.URL}, 11155111, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { rc.Close() })
	return rc
}

// receipt builds a minimal receipt JSON. gasUsed 0x5208 (21000),
// effectiveGasPrice 0x4a817c800 (20 gwei) unless withEffPrice is false;
// l1FeeField is "" for non-OP chains.
func receipt(l1FeeField string, withEffPrice bool) string {
	eff := ""
	if withEffPrice {
		eff = `"effectiveGasPrice":"0x4a817c800",`
	}
	return `{"transactionHash":"0xabc","blockHash":"0x2222222222222222222222222222222222222222222222222222222222222222",` +
		`"blockNumber":"0x1","transactionIndex":"0x0","cumulativeGasUsed":"0x5208",` +
		`"gasUsed":"0x5208",` + eff + `"status":"0x1","contractAddress":null,` +
		`"logs":[],"logsBloom":"0x` + strings.Repeat("0", 512) + `",` + l1FeeField + `"type":"0x0"}`
}

// A nonzero l1Fee must be added to GasFeeUsed so the core refund (gasFee −
// GasFeeUsed) shrinks by exactly that amount; both come from one receipt read.
func TestGetGasFeeUsed(t *testing.T) {
	execFee := new(big.Int).Mul(big.NewInt(21000), big.NewInt(20_000_000_000)) // gasUsed * effectiveGasPrice
	tb := func(rc *RPCClient) *TxBuilder {
		return &TxBuilder{rpcClient: rc, chainID: "eip155:11155111", chainIDInt: 11155111, logger: zerolog.Nop()}
	}

	t.Run("OP destination adds l1Fee", func(t *testing.T) {
		got, err := tb(receiptRPC(t, receipt(`"l1Fee":"0x5208",`, true))).GetGasFeeUsed(context.Background(), "0xabc")
		require.NoError(t, err)
		assert.Equal(t, new(big.Int).Add(execFee, big.NewInt(0x5208)).String(), got)
	})

	t.Run("non-OP destination is execution fee only", func(t *testing.T) {
		got, err := tb(receiptRPC(t, receipt(``, true))).GetGasFeeUsed(context.Background(), "0xabc")
		require.NoError(t, err)
		assert.Equal(t, execFee.String(), got)
	})

	// Errors rather than "0": a zero fee here would make core refund the full
	// gasFee, the same over-refund this fix removes. Callers retry instead.
	t.Run("missing effectiveGasPrice errors", func(t *testing.T) {
		_, err := tb(receiptRPC(t, receipt(`"l1Fee":"0x5208",`, false))).GetGasFeeUsed(context.Background(), "0xabc")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing effectiveGasPrice")
	})

	t.Run("missing receipt errors", func(t *testing.T) {
		_, err := tb(receiptRPC(t, `null`)).GetGasFeeUsed(context.Background(), "0xabc")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "receipt not found")
	})
}

// GetTransactionReceipt surfaces effectiveGasPrice (nil when absent) and l1Fee.
func TestGetTransactionReceipt_Fields(t *testing.T) {
	hash := ethcommon.HexToHash("0xabc")

	t.Run("effectiveGasPrice and l1Fee parsed", func(t *testing.T) {
		r, err := receiptRPC(t, receipt(`"l1Fee":"0x5208",`, true)).GetTransactionReceipt(context.Background(), hash)
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.Equal(t, int64(20_000_000_000), r.EffectiveGasPrice.Int64())
		assert.Equal(t, int64(0x5208), r.L1Fee.Int64())
	})

	t.Run("nil effectiveGasPrice when absent", func(t *testing.T) {
		r, err := receiptRPC(t, receipt(``, false)).GetTransactionReceipt(context.Background(), hash)
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.Nil(t, r.EffectiveGasPrice)
		assert.Equal(t, int64(0), r.L1Fee.Int64())
	})
}

// TestLive_GasFeeUsed exercises the real fetch + fee computation against public
// RPCs for two known txs (one non-OP, one OP). Skipped by default; run with:
//
//	RUN_LIVE_RPC_TESTS=1 go test ./universalClient/chains/evm/ -run TestLive_GasFeeUsed -v
func TestLive_GasFeeUsed(t *testing.T) {
	if os.Getenv("RUN_LIVE_RPC_TESTS") != "1" {
		t.Skip("set RUN_LIVE_RPC_TESTS=1 to run live RPC test")
	}

	cases := []struct {
		name    string
		rpcURL  string
		chainID int64
		txHash  string
		wantFee string // gasUsed*effectiveGasPrice + l1Fee
		wantL1  string
	}{
		{
			name:    "Ethereum Sepolia (non-OP, l1Fee=0)",
			rpcURL:  "https://ethereum-sepolia-rpc.publicnode.com",
			chainID: 11155111,
			txHash:  "0x489fb72d961e9bd69983fdaa52f0c9113705330f2e4bf4ac3fc46e1fb2977f08",
			wantFee: "170830319373250",
			wantL1:  "0",
		},
		{
			name:    "Base Sepolia (OP, nonzero l1Fee)",
			rpcURL:  "https://sepolia.base.org",
			chainID: 84532,
			txHash:  "0x7b961e5cfbb6f8ddced1a0694773290ddb0d32caaaf8494f850d7ad07ddc0c30",
			wantFee: "1032488370864",
			wantL1:  "14015970864",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, err := NewRPCClient([]string{tc.rpcURL}, tc.chainID, zerolog.Nop())
			require.NoError(t, err)
			defer rc.Close()

			receipt, err := rc.GetTransactionReceipt(context.Background(), ethcommon.HexToHash(tc.txHash))
			require.NoError(t, err)
			require.NotNil(t, receipt, "tx not found on chain")
			require.NotNil(t, receipt.EffectiveGasPrice, "receipt missing effectiveGasPrice")

			fee := gasFeeUsed(receipt.GasUsed, receipt.EffectiveGasPrice, receipt.L1Fee)
			t.Logf("gasUsed=%d effectiveGasPrice=%s l1Fee=%s => GasFeeUsed=%s",
				receipt.GasUsed, receipt.EffectiveGasPrice, receipt.L1Fee, fee)

			assert.Equal(t, tc.wantL1, receipt.L1Fee.String(), "l1Fee")
			assert.Equal(t, tc.wantFee, fee.String(), "GasFeeUsed")

			// Full path through the TxBuilder entrypoint.
			tb := &TxBuilder{rpcClient: rc, chainIDInt: tc.chainID, logger: zerolog.Nop()}
			got, err := tb.GetGasFeeUsed(context.Background(), tc.txHash)
			require.NoError(t, err)
			assert.Equal(t, tc.wantFee, got)
		})
	}
}
