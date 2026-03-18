package api

import (
	"fmt"
	"net/http"
	"testing"

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
		server := NewServer(logger, 0)

		err := server.Start()
		require.NoError(t, err)
		defer server.Stop()

		assert.NotEmpty(t, server.Addr())
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
		server := NewServer(logger, 0)

		err := server.Start()
		require.NoError(t, err)
		defer server.Stop()

		resp, err := http.Get(fmt.Sprintf("http://%s/health", server.Addr()))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
	})
}
