package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleHealth(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	t.Run("Health check returns OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		
		server.handleHealth(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "OK", w.Body.String())
	})
}

func TestHandleChainConfigs(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	t.Run("GET chain configs success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/chain-configs", nil)
		w := httptest.NewRecorder()
		
		server.handleChainConfigs(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check that we have data and last_fetched fields
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Verify the data is an array
		data, ok := response["data"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, data, 1)
	})
	
	t.Run("Non-GET method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chain-configs", nil)
		w := httptest.NewRecorder()
		
		server.handleChainConfigs(w, req)
		
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		assert.Contains(t, w.Body.String(), "Method not allowed")
	})
}

func TestHandleTokenConfigs(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	t.Run("GET token configs success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-configs", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfigs(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check that we have data and last_fetched fields
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Verify the data is an array
		data, ok := response["data"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, data, 1)
	})
	
	t.Run("Non-GET method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/token-configs", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfigs(w, req)
		
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		assert.Contains(t, w.Body.String(), "Method not allowed")
	})
}

func TestHandleTokenConfigsByChain(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	t.Run("GET token configs by chain success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-configs-by-chain?chain=eip155:1", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfigsByChain(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check that we have data and last_fetched fields
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Verify the data is an array with one item
		data, ok := response["data"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, data, 1)
	})
	
	t.Run("Missing chain parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-configs-by-chain", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfigsByChain(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		
		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "chain parameter is required", response.Error)
	})
	
	t.Run("Non-GET method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/token-configs-by-chain", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfigsByChain(w, req)
		
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
	
	t.Run("Empty result for unknown chain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-configs-by-chain?chain=unknown:999", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfigsByChain(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Verify the data is an empty array
		data, ok := response["data"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, data, 0)
	})
}

func TestHandleTokenConfig(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}
	
	t.Run("GET specific token config success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-config?chain=eip155:1&address=0xAAA", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfig(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		
		// Check that we have data and last_fetched fields
		assert.Contains(t, response, "data")
		assert.Contains(t, response, "last_fetched")
		
		// Verify the data is an object with correct fields
		data, ok := response["data"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "USDT", data["symbol"])
		assert.Equal(t, "0xAAA", data["address"])
	})
	
	t.Run("Missing chain parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-config?address=0xAAA", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfig(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "chain and address parameters are required", response.Error)
	})
	
	t.Run("Missing address parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-config?chain=eip155:1", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfig(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "chain and address parameters are required", response.Error)
	})
	
	t.Run("Token config not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/token-config?chain=eip155:1&address=0xBBB", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfig(w, req)
		
		assert.Equal(t, http.StatusNotFound, w.Code)
		
		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "token config not found for chain eip155:1 and address 0xBBB", response.Error)
	})
	
	t.Run("Non-GET method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/token-config", nil)
		w := httptest.NewRecorder()
		
		server.handleTokenConfig(w, req)
		
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestQueryResponseSerialization(t *testing.T) {
	t.Run("Serialize QueryResponse", func(t *testing.T) {
		now := time.Now()
		response := QueryResponse{
			Data: map[string]string{
				"test": "data",
			},
			LastFetched: now,
		}
		
		data, err := json.Marshal(response)
		require.NoError(t, err)
		
		var decoded QueryResponse
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)
		
		assert.Equal(t, response.LastFetched.Unix(), decoded.LastFetched.Unix())
	})
	
	t.Run("Serialize ErrorResponse", func(t *testing.T) {
		response := ErrorResponse{
			Error: "test error message",
		}
		
		data, err := json.Marshal(response)
		require.NoError(t, err)
		
		var decoded ErrorResponse
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)
		
		assert.Equal(t, response.Error, decoded.Error)
	})
}