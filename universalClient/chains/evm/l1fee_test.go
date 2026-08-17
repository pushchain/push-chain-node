package evm

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
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

// receipt builds a minimal receipt JSON with gasUsed 0x5208 (21000) and
// effectiveGasPrice 0x4a817c800 (20 gwei); l1FeeField is "" for non-OP chains.
func receipt(l1FeeField string) string {
	return `{"transactionHash":"0xabc","blockHash":"0x2222222222222222222222222222222222222222222222222222222222222222",` +
		`"blockNumber":"0x1","transactionIndex":"0x0","cumulativeGasUsed":"0x5208",` +
		`"gasUsed":"0x5208","effectiveGasPrice":"0x4a817c800","status":"0x1","contractAddress":null,` +
		`"logs":[],"logsBloom":"0x` + strings.Repeat("0", 512) + `",` + l1FeeField + `"type":"0x0"}`
}

// A nonzero l1Fee must be added to GasFee so the core refund (gasFee −
// GasFeeUsed) shrinks by exactly that amount. Both values come from one receipt.
func TestReceiptGasFee(t *testing.T) {
	hash := ethcommon.HexToHash("0xabc")
	execFee := new(big.Int).Mul(big.NewInt(21000), big.NewInt(20_000_000_000)) // gasUsed * effectiveGasPrice

	t.Run("OP destination adds l1Fee", func(t *testing.T) {
		rc := receiptRPC(t, receipt(`"l1Fee":"0x5208",`))
		r, err := rc.GetTransactionReceipt(context.Background(), hash)
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.Equal(t, new(big.Int).Add(execFee, big.NewInt(0x5208)), receiptGasFee(r))
	})

	t.Run("non-OP destination is execution fee only", func(t *testing.T) {
		rc := receiptRPC(t, receipt(``))
		r, err := rc.GetTransactionReceipt(context.Background(), hash)
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.Equal(t, execFee, receiptGasFee(r))
	})

	t.Run("missing receipt returns nil", func(t *testing.T) {
		rc := receiptRPC(t, `null`)
		r, err := rc.GetTransactionReceipt(context.Background(), hash)
		require.NoError(t, err)
		assert.Nil(t, r)
	})
}

// GetGasFeeUsed (revert/resolver path) delegates to the same single-call helper.
func TestGetGasFeeUsed_IncludesL1Fee(t *testing.T) {
	execFee := new(big.Int).Mul(big.NewInt(21000), big.NewInt(20_000_000_000))

	rc := receiptRPC(t, receipt(`"l1Fee":"0x5208",`))
	tb := &TxBuilder{rpcClient: rc, chainID: "eip155:11155111", chainIDInt: 11155111, logger: zerolog.Nop()}
	got, err := tb.GetGasFeeUsed(context.Background(), "0xabc")
	require.NoError(t, err)
	assert.Equal(t, new(big.Int).Add(execFee, big.NewInt(0x5208)).String(), got)
}
