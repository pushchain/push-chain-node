package svm

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gagliardetto/solana-go"
	solrpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// accountInfoResult builds a getAccountInfo result with base64 data.
func accountInfoResult(slot uint64, owner solana.PublicKey, data []byte) map[string]any {
	return map[string]any{
		"context": map[string]any{"slot": slot},
		"value": map[string]any{
			"data":       []any{base64.StdEncoding.EncodeToString(data), "base64"},
			"executable": false,
			"lamports":   1,
			"owner":      owner.String(),
			"rentEpoch":  0,
		},
	}
}

// newReadTestClient spins up a JSON-RPC server answering from results keyed by
// method name, and returns a Client wired to it.
func newReadTestClient(t *testing.T, results map[string]any) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		resp := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID)}
		if result, ok := results[req.Method]; ok {
			resp["result"] = result
		} else {
			t.Errorf("unexpected RPC method %s", req.Method)
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(srv.Close)

	return &Client{
		logger:    zerolog.Nop(),
		rpcClient: &RPCClient{clients: []*solrpc.Client{solrpc.New(srv.URL)}, logger: zerolog.Nop()},
	}
}

func svmReadRequest(t *testing.T, queryType uint8, minSlot uint64, owner []byte) *ucallbacktypes.ReadRequest {
	t.Helper()
	query, err := svmEnvelopeArgs.Pack(rawSvmEnvelope{
		QueryType: queryType,
		SlotRef: struct {
			MinSlot uint64
		}{minSlot},
	})
	require.NoError(t, err)
	return &ucallbacktypes.ReadRequest{
		RequestId:        "0xreq1",
		DestinationChain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		Owner:            owner,
		Query:            query,
	}
}

func testAccount() solana.PublicKey {
	return solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
}

func TestExecuteRead_LamportBalance(t *testing.T) {
	account := testAccount()

	t.Run("success", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"getBalance": map[string]any{
				"context": map[string]any{"slot": 900},
				"value":   5_000_000,
			},
		})

		result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQueryLamportBalance), 800, account.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS, result.Status)
		assert.Equal(t, big.NewInt(5_000_000), new(big.Int).SetBytes(result.ResultData))
		assert.Equal(t, uint64(900), result.ObservedBlockHeight)
	})

	t.Run("observed slot below min slot is transient", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"getBalance": map[string]any{
				"context": map[string]any{"slot": 700},
				"value":   5_000_000,
			},
		})

		result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQueryLamportBalance), 800, account.Bytes()))
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestExecuteRead_SPLTokenAccount(t *testing.T) {
	account := testAccount()

	tokenAccountData := func(amount uint64) []byte {
		data := make([]byte, 165)
		binary.LittleEndian.PutUint64(data[splTokenAmountOffset:], amount)
		return data
	}

	t.Run("success", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"getAccountInfo": accountInfoResult(900, solana.TokenProgramID, tokenAccountData(777)),
		})

		result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQuerySPLTokenAccount), 800, account.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS, result.Status)
		assert.Equal(t, big.NewInt(777), new(big.Int).SetBytes(result.ResultData))
		assert.Equal(t, uint64(900), result.ObservedBlockHeight)
	})

	t.Run("non token-program owner is a votable ERROR", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"getAccountInfo": accountInfoResult(900, solana.SystemProgramID, tokenAccountData(777)),
		})

		result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQuerySPLTokenAccount), 0, account.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_ERROR, result.Status)
		assert.Equal(t, ucallbacktypes.ReadErrorCode_READ_ERROR_INVALID_RESULT, result.ErrorCode)
	})

	t.Run("truncated account data is a votable ERROR", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"getAccountInfo": accountInfoResult(900, solana.TokenProgramID, make([]byte, 10)),
		})

		result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQuerySPLTokenAccount), 0, account.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_ERROR, result.Status)
		assert.Equal(t, ucallbacktypes.ReadErrorCode_READ_ERROR_INVALID_RESULT, result.ErrorCode)
	})

	t.Run("missing account is a votable ERROR", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"getAccountInfo": map[string]any{
				"context": map[string]any{"slot": 900},
				"value":   nil,
			},
		})

		result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQuerySPLTokenAccount), 0, account.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_ERROR, result.Status)
		assert.Equal(t, ucallbacktypes.ReadErrorCode_READ_ERROR_NOT_FOUND, result.ErrorCode)
	})
}

func TestExecuteRead_RawAccountData(t *testing.T) {
	account := testAccount()
	raw := []byte{0x01, 0x02, 0x03}

	client := newReadTestClient(t, map[string]any{
		"getAccountInfo": accountInfoResult(900, solana.SystemProgramID, raw),
	})

	result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQueryRawAccountData), 0, account.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS, result.Status)
	assert.Equal(t, raw, result.ResultData)
	assert.Equal(t, uint64(900), result.ObservedBlockHeight)
}

func TestExecuteRead_InvalidInputs(t *testing.T) {
	account := testAccount()

	t.Run("invalid envelope is a votable ERROR", func(t *testing.T) {
		client := newReadTestClient(t, nil)

		result, err := client.ExecuteRead(context.Background(), &ucallbacktypes.ReadRequest{
			RequestId: "0xreq1",
			Owner:     account.Bytes(),
			Query:     []byte{0x01},
		})
		require.NoError(t, err)
		assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_ERROR, result.Status)
	})

	t.Run("owner not 32 bytes is a votable ERROR", func(t *testing.T) {
		client := newReadTestClient(t, nil)

		result, err := client.ExecuteRead(context.Background(), svmReadRequest(t, uint8(solanaQueryLamportBalance), 0, []byte{0x01, 0x02}))
		require.NoError(t, err)
		assert.Equal(t, ucallbacktypes.ReadStatus_READ_STATUS_ERROR, result.Status)
	})
}
