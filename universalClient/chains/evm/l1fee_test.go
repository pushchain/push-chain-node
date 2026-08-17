package evm

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rpcServer serves eth_chainId plus caller-supplied receipt and tx JSON.
func rpcServer(t *testing.T, receiptJSON, txJSON string) *RPCClient {
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
		case strings.Contains(string(body), "eth_getTransactionByHash"):
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + txJSON + `}`))
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

func receiptJSON(txHash string, l1FeeField string) string {
	return `{"transactionHash":"` + txHash + `",` +
		`"blockHash":"0x2222222222222222222222222222222222222222222222222222222222222222",` +
		`"blockNumber":"0x1","transactionIndex":"0x0","cumulativeGasUsed":"0x5208",` +
		`"gasUsed":"0x5208","status":"0x1","contractAddress":null,"logs":[],` +
		`"logsBloom":"0x` + strings.Repeat("0", 512) + `",` + l1FeeField + `"type":"0x0"}`
}

func TestGetL1Fee(t *testing.T) {
	hash := ethcommon.HexToHash("0xabc")

	t.Run("parses OP l1Fee", func(t *testing.T) {
		rc := rpcServer(t, receiptJSON(hash.Hex(), `"l1Fee":"0x5208",`), `null`)
		got, err := rc.GetL1Fee(context.Background(), hash)
		require.NoError(t, err)
		assert.Equal(t, int64(0x5208), got.Int64())
	})

	t.Run("returns 0 when field absent (non-OP)", func(t *testing.T) {
		rc := rpcServer(t, receiptJSON(hash.Hex(), ``), `null`)
		got, err := rc.GetL1Fee(context.Background(), hash)
		require.NoError(t, err)
		assert.Equal(t, int64(0), got.Int64())
	})
}

// A nonzero l1Fee must be added to GasFeeUsed so the core refund (gasFee −
// GasFeeUsed) shrinks by exactly that amount.
func TestGetGasFeeUsed_IncludesL1Fee(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	chainID := big.NewInt(11155111)
	gasPrice := big.NewInt(20_000_000_000)
	to := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	signedTx, err := types.SignTx(
		types.NewTransaction(0, to, big.NewInt(0), 21000, gasPrice, nil),
		types.NewEIP155Signer(chainID), key,
	)
	require.NoError(t, err)
	txJSON, err := signedTx.MarshalJSON()
	require.NoError(t, err)

	execFee := new(big.Int).Mul(big.NewInt(21000), gasPrice) // gasUsed 0x5208 = 21000

	t.Run("OP destination adds l1Fee", func(t *testing.T) {
		rc := rpcServer(t, receiptJSON(signedTx.Hash().Hex(), `"l1Fee":"0x5208",`), string(txJSON))
		tb := &TxBuilder{rpcClient: rc, chainID: "eip155:11155111", chainIDInt: 11155111, logger: zerolog.Nop()}
		got, err := tb.GetGasFeeUsed(context.Background(), signedTx.Hash().Hex())
		require.NoError(t, err)
		want := new(big.Int).Add(execFee, big.NewInt(0x5208))
		assert.Equal(t, want.String(), got)
	})

	t.Run("non-OP destination is execution fee only", func(t *testing.T) {
		rc := rpcServer(t, receiptJSON(signedTx.Hash().Hex(), ``), string(txJSON))
		tb := &TxBuilder{rpcClient: rc, chainID: "eip155:11155111", chainIDInt: 11155111, logger: zerolog.Nop()}
		got, err := tb.GetGasFeeUsed(context.Background(), signedTx.Hash().Hex())
		require.NoError(t, err)
		assert.Equal(t, execFee.String(), got)
	})
}
