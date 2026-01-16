package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockChainClient is a mock implementation of ChainClient for testing
type MockChainClient struct {
	started   bool
	stopped   bool
	healthy   bool
	startErr  error
	stopErr   error
}

func (m *MockChainClient) Start(ctx context.Context) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	return nil
}

func (m *MockChainClient) Stop() error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stopped = true
	return nil
}

func (m *MockChainClient) IsHealthy() bool {
	return m.healthy
}

func TestChainClientInterface(t *testing.T) {
	t.Run("mock implements ChainClient", func(t *testing.T) {
		var client ChainClient = &MockChainClient{}
		assert.NotNil(t, client)
	})

	t.Run("Start method", func(t *testing.T) {
		client := &MockChainClient{}
		err := client.Start(context.Background())
		assert.NoError(t, err)
		assert.True(t, client.started)
	})

	t.Run("Stop method", func(t *testing.T) {
		client := &MockChainClient{}
		err := client.Stop()
		assert.NoError(t, err)
		assert.True(t, client.stopped)
	})

	t.Run("IsHealthy method", func(t *testing.T) {
		client := &MockChainClient{healthy: true}
		assert.True(t, client.IsHealthy())

		unhealthyClient := &MockChainClient{healthy: false}
		assert.False(t, unhealthyClient.IsHealthy())
	})
}
