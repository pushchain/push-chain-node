package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestHandleChainData(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
		logger: logger,
	}

	t.Run("GET chain data success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/chain-data", nil)
		w := httptest.NewRecorder()

		server.handleChainData(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Check that we have data field
		assert.Contains(t, response, "data")

		// Verify the data is an array
		data, ok := response["data"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, data, 2)
	})

	t.Run("Non-GET method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chain-data", nil)
		w := httptest.NewRecorder()

		server.handleChainData(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		assert.Contains(t, w.Body.String(), "Method not allowed")
	})
}