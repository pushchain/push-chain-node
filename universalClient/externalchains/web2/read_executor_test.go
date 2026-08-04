package web2

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/uread"
)

func packEnvelope(t *testing.T, env rawWeb2Envelope) []byte {
	t.Helper()
	data, err := web2EnvelopeArgs.Pack(env)
	require.NoError(t, err)
	return data
}

func extractSpec(path string, valueType extractValueType, decimals uint8) rawWeb2Extract {
	return rawWeb2Extract{Path: path, ValueType: uint8(valueType), Mode: uint8(modeIdentical), Decimals: decimals}
}

func newTestExecutor(t *testing.T, handler http.HandlerFunc) (*Executor, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	e := NewExecutor(zerolog.Nop())
	e.allowInsecureURL = true // httptest serves plain http
	return e, srv.URL
}

func jsonHandler(t *testing.T, wantMethod string, response any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, wantMethod, r.Method)
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}
}

func web2Request(t *testing.T, env rawWeb2Envelope) *uread.ReadRequest {
	t.Helper()
	return &uread.ReadRequest{
		RequestID:        "0xreq1",
		DestinationChain: "web2:https",
		Query:            packEnvelope(t, env),
	}
}

func TestExecuteRead_GetIdenticalFields(t *testing.T) {
	e, url := newTestExecutor(t, jsonHandler(t, http.MethodGet, map[string]any{
		"status": "FINAL",
		"winner": "TeamA",
		"score":  map[string]any{"a": 3, "b": 1},
	}))

	req := web2Request(t, rawWeb2Envelope{
		Method: uint8(web2MethodGet),
		Url:    url,
		Extract: []rawWeb2Extract{
			extractSpec("$.status", valueTypeString, 0),
			extractSpec("$.winner", valueTypeString, 0),
			extractSpec("$.score.a", valueTypeUint256, 0),
		},
	})

	result, err := e.ExecuteRead(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, uread.ReadStatusSuccess, result.Status)
	assert.Zero(t, result.ObservedBlockHeight)

	stringTy, _ := abi.NewType("string", "", nil)
	uintTy, _ := abi.NewType("uint256", "", nil)
	args := abi.Arguments{{Type: stringTy}, {Type: stringTy}, {Type: uintTy}}
	vals, err := args.Unpack(result.ResultData)
	require.NoError(t, err)
	assert.Equal(t, "FINAL", vals[0])
	assert.Equal(t, "TeamA", vals[1])
	assert.Equal(t, big.NewInt(3), vals[2])
}

func TestExecuteRead_DecimalScaling(t *testing.T) {
	e, url := newTestExecutor(t, jsonHandler(t, http.MethodGet, map[string]any{
		"price": 3512.4471,
	}))

	req := web2Request(t, rawWeb2Envelope{
		Method:  uint8(web2MethodGet),
		Url:     url,
		Extract: []rawWeb2Extract{extractSpec("$.price", valueTypeUint256, 8)},
	})

	result, err := e.ExecuteRead(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, uread.ReadStatusSuccess, result.Status)

	assert.Equal(t, big.NewInt(351244710000), new(big.Int).SetBytes(result.ResultData))
}

func TestExecuteRead_PostBody(t *testing.T) {
	e, url := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "{ token { decimals } }", body["query"])
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"token": map[string]any{"decimals": 18}},
		}))
	})

	req := web2Request(t, rawWeb2Envelope{
		Method:  uint8(web2MethodPost),
		Url:     url,
		Headers: []byte(`{"content-type":"application/json"}`),
		Body:    []byte(`{"query":"{ token { decimals } }"}`),
		Extract: []rawWeb2Extract{extractSpec("$.data.token.decimals", valueTypeUint256, 0)},
	})

	result, err := e.ExecuteRead(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, uread.ReadStatusSuccess, result.Status)
	assert.Equal(t, big.NewInt(18), new(big.Int).SetBytes(result.ResultData))
}

func TestExecuteRead_ArrayIndexPath(t *testing.T) {
	e, url := newTestExecutor(t, jsonHandler(t, http.MethodGet, map[string]any{
		"items": []any{map[string]any{"ok": true}, map[string]any{"ok": false}},
	}))

	req := web2Request(t, rawWeb2Envelope{
		Method:  uint8(web2MethodGet),
		Url:     url,
		Extract: []rawWeb2Extract{extractSpec("$.items[1].ok", valueTypeBool, 0)},
	})

	result, err := e.ExecuteRead(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, uread.ReadStatusSuccess, result.Status)

	boolTy, _ := abi.NewType("bool", "", nil)
	vals, err := abi.Arguments{{Type: boolTy}}.Unpack(result.ResultData)
	require.NoError(t, err)
	assert.Equal(t, false, vals[0])
}

func TestExecuteRead_VotableErrors(t *testing.T) {
	t.Run("invalid envelope", func(t *testing.T) {
		e := NewExecutor(zerolog.Nop())
		result, err := e.ExecuteRead(context.Background(), &uread.ReadRequest{Query: []byte{0x01}})
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})

	t.Run("non-https url", func(t *testing.T) {
		e := NewExecutor(zerolog.Nop())
		req := web2Request(t, rawWeb2Envelope{
			Method:  uint8(web2MethodGet),
			Url:     "http://insecure.example.com",
			Extract: []rawWeb2Extract{extractSpec("$.x", valueTypeString, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})

	t.Run("non-identical mode not supported", func(t *testing.T) {
		e := NewExecutor(zerolog.Nop())
		req := web2Request(t, rawWeb2Envelope{
			Method: uint8(web2MethodGet),
			Url:    "https://api.example.com",
			Extract: []rawWeb2Extract{
				{Path: "$.price", ValueType: uint8(valueTypeUint256), Mode: 1, Decimals: 8},
			},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})

	t.Run("GET with body", func(t *testing.T) {
		e := NewExecutor(zerolog.Nop())
		req := web2Request(t, rawWeb2Envelope{
			Method:  uint8(web2MethodGet),
			Url:     "https://api.example.com",
			Body:    []byte("nope"),
			Extract: []rawWeb2Extract{extractSpec("$.x", valueTypeString, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})

	t.Run("missing path", func(t *testing.T) {
		e, url := newTestExecutor(t, jsonHandler(t, http.MethodGet, map[string]any{"a": 1}))
		req := web2Request(t, rawWeb2Envelope{
			Method:  uint8(web2MethodGet),
			Url:     url,
			Extract: []rawWeb2Extract{extractSpec("$.missing", valueTypeUint256, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})

	t.Run("type mismatch", func(t *testing.T) {
		e, url := newTestExecutor(t, jsonHandler(t, http.MethodGet, map[string]any{"a": "text"}))
		req := web2Request(t, rawWeb2Envelope{
			Method:  uint8(web2MethodGet),
			Url:     url,
			Extract: []rawWeb2Extract{extractSpec("$.a", valueTypeBool, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})

	t.Run("non-JSON response", func(t *testing.T) {
		e, url := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>not json</html>"))
		})
		req := web2Request(t, rawWeb2Envelope{
			Method:  uint8(web2MethodGet),
			Url:     url,
			Extract: []rawWeb2Extract{extractSpec("$.a", valueTypeString, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})

	t.Run("404 status", func(t *testing.T) {
		e, url := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		req := web2Request(t, rawWeb2Envelope{
			Method:  uint8(web2MethodGet),
			Url:     url,
			Extract: []rawWeb2Extract{extractSpec("$.a", valueTypeString, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, uread.ReadStatusError, result.Status)
	})
}

func TestExecuteRead_TransientErrors(t *testing.T) {
	t.Run("500 status", func(t *testing.T) {
		e, url := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		req := web2Request(t, rawWeb2Envelope{
			Method:  uint8(web2MethodGet),
			Url:     url,
			Extract: []rawWeb2Extract{extractSpec("$.a", valueTypeString, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("unreachable endpoint", func(t *testing.T) {
		e := NewExecutor(zerolog.Nop())
		e.allowInsecureURL = true
		req := web2Request(t, rawWeb2Envelope{
			Method:    uint8(web2MethodGet),
			Url:       "http://127.0.0.1:1",
			TimeoutMs: 500,
			Extract:   []rawWeb2Extract{extractSpec("$.a", valueTypeString, 0)},
		})
		result, err := e.ExecuteRead(context.Background(), req)
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestScaledInteger(t *testing.T) {
	cases := []struct {
		in       string
		decimals uint8
		want     string
	}{
		{"3512.4471", 8, "351244710000"},
		{"100", 0, "100"},
		{"0.5", 2, "50"},
		{"1.999", 0, "1"},   // truncates
		{"-2.5", 1, "-25"},
	}
	for _, tc := range cases {
		got, err := scaledInteger(json.Number(tc.in), tc.decimals)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got.String(), tc.in)
	}

	_, err := scaledInteger(json.Number("not-a-number"), 0)
	assert.Error(t, err)
	_, err = scaledInteger(true, 0)
	assert.Error(t, err)
}

func TestDecodeWeb2QueryEnvelope_Invalid(t *testing.T) {
	t.Run("garbage bytes", func(t *testing.T) {
		_, err := decodeWeb2QueryEnvelope([]byte{0x01, 0x02})
		assert.Error(t, err)
	})

	t.Run("no extract entries", func(t *testing.T) {
		data, err := web2EnvelopeArgs.Pack(rawWeb2Envelope{Method: 0, Url: "https://x"})
		require.NoError(t, err)
		_, err = decodeWeb2QueryEnvelope(data)
		assert.Error(t, err)
	})

	t.Run("unknown method", func(t *testing.T) {
		data, err := web2EnvelopeArgs.Pack(rawWeb2Envelope{
			Method:  9,
			Url:     "https://x",
			Extract: []rawWeb2Extract{{Path: "$.a"}},
		})
		require.NoError(t, err)
		_, err = decodeWeb2QueryEnvelope(data)
		assert.Error(t, err)
	})
}
