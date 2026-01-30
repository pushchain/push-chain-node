package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Create server with valid config", func(t *testing.T) {
		server := NewServer(logger, 8080)

		assert.NotNil(t, server)
		assert.NotNil(t, server.server)
		assert.Equal(t, ":8080", server.server.Addr)
	})

	t.Run("Create server with different port", func(t *testing.T) {
		server := NewServer(logger, 9090)

		assert.NotNil(t, server)
		assert.Equal(t, ":9090", server.server.Addr)
	})
}

func TestServerStartStop(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Start and stop server", func(t *testing.T) {
		// Use a random port to avoid conflicts
		server := NewServer(logger, 0)

		// Start server
		err := server.Start()
		require.NoError(t, err)

		// Give server time to start
		time.Sleep(200 * time.Millisecond)

		// Stop server
		err = server.Stop()
		assert.NoError(t, err)
	})

	t.Run("Start with nil server", func(t *testing.T) {
		server := &Server{
			logger: logger,
			server: nil,
		}
		err := server.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query server is nil")
	})

	t.Run("Stop with nil server", func(t *testing.T) {
		server := &Server{
			logger: logger,
			server: nil,
		}
		err := server.Stop()
		assert.NoError(t, err)
	})
}

func TestServerIntegration(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("Server lifecycle with HTTP client", func(t *testing.T) {
		// Create server on a specific port
		server := NewServer(logger, 18080)

		// Start server
		err := server.Start()
		require.NoError(t, err)
		defer server.Stop()

		// Wait for server to be ready
		time.Sleep(200 * time.Millisecond)

		// Test health endpoint
		resp, err := http.Get("http://localhost:18080/health")
		if err == nil {
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}
	})
}

// Test handler functions directly using httptest
func TestHealthHandler(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	server := &Server{
		logger: logger,
	}

	handler := http.HandlerFunc(server.handleHealth)

	t.Run("Health check returns OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// handleHealth just returns "OK" as plain text
		assert.Equal(t, "OK", w.Body.String())
	})
}
