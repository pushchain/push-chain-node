package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/uread"
)

// fakeHeader is a minimal valid block header JSON accepted by types.Header.
func fakeHeader(number uint64) map[string]any {
	zeroHash := "0x0000000000000000000000000000000000000000000000000000000000000000"
	return map[string]any{
		"parentHash":       zeroHash,
		"sha3Uncles":       zeroHash,
		"miner":            "0x0000000000000000000000000000000000000000",
		"stateRoot":        zeroHash,
		"transactionsRoot": zeroHash,
		"receiptsRoot":     zeroHash,
		"logsBloom":        "0x" + fmt.Sprintf("%0512x", 0),
		"difficulty":       "0x0",
		"number":           fmt.Sprintf("0x%x", number),
		"gasLimit":         "0x0",
		"gasUsed":          "0x0",
		"timestamp":        "0x0",
		"extraData":        "0x",
		"mixHash":          zeroHash,
		"nonce":            "0x0000000000000000",
	}
}

type rpcFault struct {
	code    int
	message string
}

// newReadTestClient spins up a JSON-RPC server answering from results/faults
// keyed by method name, and returns a Client wired to it.
func newReadTestClient(t *testing.T, results map[string]any, faults map[string]rpcFault) *Client {
	t.Helper()

	// every read is gated on the chain tip; default to a comfortably deep chain
	// unless the test overrides eth_blockNumber
	if results != nil {
		if _, ok := results["eth_blockNumber"]; !ok {
			if _, ok := faults["eth_blockNumber"]; !ok {
				results["eth_blockNumber"] = "0x1000"
			}
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		resp := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID)}
		if fault, ok := faults[req.Method]; ok {
			resp["error"] = map[string]any{"code": fault.code, "message": fault.message}
		} else if result, ok := results[req.Method]; ok {
			resp["result"] = result
		} else {
			t.Errorf("unexpected RPC method %s", req.Method)
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(srv.Close)

	ethClient, err := ethclient.Dial(srv.URL)
	require.NoError(t, err)
	t.Cleanup(ethClient.Close)

	return &Client{
		logger:    zerolog.Nop(),
		rpcClient: &RPCClient{clients: []*ethclient.Client{ethClient}, logger: zerolog.Nop()},
	}
}

func evmReadRequest(t *testing.T, queryType uint8, blockNumber uint64, payload []byte) *uread.ReadRequest {
	t.Helper()
	return &uread.ReadRequest{
		RequestID:              "0xreq1",
		DestinationChain:       "eip155:11155111",
		Query:                  packEvmEnvelope(t, queryType, 0, blockNumber, payload),
		MinConfirmations:       1,
		DestinationBlockHeight: 100,
	}
}

func TestExecuteRead_AccountBalance(t *testing.T) {
	target := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	payload, err := addressArgs.Pack(target)
	require.NoError(t, err)

	client := newReadTestClient(t, map[string]any{
		"eth_getBlockByNumber": fakeHeader(100),
		"eth_getBalance":       "0xf4240", // 1_000_000
	}, nil)

	result, err := client.ExecuteRead(context.Background(), evmReadRequest(t, uint8(evmQueryAccountBalance), 0, payload))
	require.NoError(t, err)
	assert.Equal(t, uread.ReadStatusSuccess, result.Status)
	assert.Equal(t, big.NewInt(1_000_000), new(big.Int).SetBytes(result.ResultData))
	assert.Equal(t, uint64(100), result.ObservedBlockHeight)
	assert.Len(t, result.ObservedBlockHash, 32)
}

func TestExecuteRead_ERC20Balance(t *testing.T) {
	token := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	owner := ethcommon.HexToAddress("0x3333333333333333333333333333333333333333")
	payload, err := addressPairArgs.Pack(token, owner)
	require.NoError(t, err)

	client := newReadTestClient(t, map[string]any{
		"eth_getBlockByNumber": fakeHeader(100),
		"eth_call":             "0x" + fmt.Sprintf("%064x", 42),
	}, nil)

	result, err := client.ExecuteRead(context.Background(), evmReadRequest(t, uint8(evmQueryERC20Balance), 0, payload))
	require.NoError(t, err)
	assert.Equal(t, uread.ReadStatusSuccess, result.Status)
	assert.Equal(t, big.NewInt(42), new(big.Int).SetBytes(result.ResultData))
}

func TestExecuteRead_ContractCall(t *testing.T) {
	target := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	payload, err := addressBytesArgs.Pack(target, []byte{0xde, 0xad})
	require.NoError(t, err)

	t.Run("returns raw returndata", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"eth_getBlockByNumber": fakeHeader(100),
			"eth_call":             "0xcafebabe",
		}, nil)

		result, err := client.ExecuteRead(context.Background(), evmReadRequest(t, uint8(evmQueryContractCall), 0, payload))
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusSuccess, result.Status)
		assert.Equal(t, []byte{0xca, 0xfe, 0xba, 0xbe}, result.ResultData)
	})

	t.Run("revert is a votable ERROR observation", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"eth_getBlockByNumber": fakeHeader(100),
		}, map[string]rpcFault{
			"eth_call": {code: 3, message: "execution reverted"},
		})

		result, err := client.ExecuteRead(context.Background(), evmReadRequest(t, uint8(evmQueryContractCall), 0, payload))
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
		assert.Empty(t, result.ResultData)
	})
}

func TestExecuteRead_StorageSlot(t *testing.T) {
	target := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	payload, err := addressBytes32Args.Pack(target, [32]byte{0x01})
	require.NoError(t, err)

	client := newReadTestClient(t, map[string]any{
		"eth_getBlockByNumber": fakeHeader(100),
		"eth_getStorageAt":     "0x" + fmt.Sprintf("%064x", 7),
	}, nil)

	result, err := client.ExecuteRead(context.Background(), evmReadRequest(t, uint8(evmQueryStorageSlot), 0, payload))
	require.NoError(t, err)
	assert.Equal(t, uread.ReadStatusSuccess, result.Status)
	require.Len(t, result.ResultData, 32)
	assert.Equal(t, big.NewInt(7), new(big.Int).SetBytes(result.ResultData))
}

func TestExecuteRead_InvalidEnvelope(t *testing.T) {
	client := newReadTestClient(t, nil, nil)

	result, err := client.ExecuteRead(context.Background(), &uread.ReadRequest{
		RequestID:              "0xreq1",
		Query:                  []byte{0x01, 0x02},
		DestinationBlockHeight: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, uread.ReadStatusError, result.Status)
}

func TestExecuteRead_RPCFailureIsTransient(t *testing.T) {
	target := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	payload, err := addressArgs.Pack(target)
	require.NoError(t, err)

	client := newReadTestClient(t, map[string]any{}, map[string]rpcFault{
		"eth_getBlockByNumber": {code: -32000, message: "node is syncing"},
	})

	result, err := client.ExecuteRead(context.Background(), evmReadRequest(t, uint8(evmQueryAccountBalance), 0, payload))
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestExecuteRead_EnvelopeBlockNumberUsedWhenNotPinned(t *testing.T) {
	target := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	payload, err := addressArgs.Pack(target)
	require.NoError(t, err)

	client := newReadTestClient(t, map[string]any{
		"eth_getBlockByNumber": fakeHeader(55),
		"eth_getBalance":       "0x1",
	}, nil)

	req := evmReadRequest(t, uint8(evmQueryAccountBalance), 55, payload)
	req.DestinationBlockHeight = 0 // client-provided height in the envelope

	result, err := client.ExecuteRead(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, uint64(55), result.ObservedBlockHeight)
}

func TestExecuteRead_MissingHeightIsVotableError(t *testing.T) {
	target := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	payload, err := addressArgs.Pack(target)
	require.NoError(t, err)

	client := newReadTestClient(t, nil, nil)

	req := evmReadRequest(t, uint8(evmQueryAccountBalance), 0, payload)
	req.DestinationBlockHeight = 0

	result, err := client.ExecuteRead(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, uread.ReadStatusError, result.Status)
}

func TestExecuteRead_ConfirmationGate(t *testing.T) {
	target := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	payload, err := addressArgs.Pack(target)
	require.NoError(t, err)

	t.Run("height not deep enough is transient", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"eth_blockNumber": "0x64", // 100
		}, nil)

		req := evmReadRequest(t, uint8(evmQueryAccountBalance), 0, payload)
		req.DestinationBlockHeight = 100
		req.MinConfirmations = 5 // needs chain at >= 105

		result, err := client.ExecuteRead(context.Background(), req)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("executes once deep enough", func(t *testing.T) {
		client := newReadTestClient(t, map[string]any{
			"eth_blockNumber":      "0x69", // 105
			"eth_getBlockByNumber": fakeHeader(100),
			"eth_getBalance":       "0x1",
		}, nil)

		req := evmReadRequest(t, uint8(evmQueryAccountBalance), 0, payload)
		req.DestinationBlockHeight = 100
		req.MinConfirmations = 5

		result, err := client.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uint64(100), result.ObservedBlockHeight)
	})
}
