package rpcpool

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/universalClient/config"
)

// mockClientFactory creates mock clients for testing
func mockClientFactory(shouldFail bool) ClientFactory {
	return func(url string) (Client, error) {
		if shouldFail {
			return nil, assert.AnError
		}
		return &mockClient{}, nil
	}
}

func TestNewManager(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   1,
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	tests := []struct {
		name        string
		chainID     string
		urls        []string
		expectedNil bool
	}{
		{
			name:        "valid configuration",
			chainID:     "eip155:1",
			urls:        []string{"http://test1.com", "http://test2.com"},
			expectedNil: false,
		},
		{
			name:        "empty URLs returns nil",
			chainID:     "eip155:1",
			urls:        []string{},
			expectedNil: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(
				tt.chainID,
				tt.urls,
				poolConfig,
				mockClientFactory(false),
				logger,
			)
			
			if tt.expectedNil {
				assert.Nil(t, manager)
			} else {
				assert.NotNil(t, manager)
				assert.Equal(t, tt.chainID, manager.chainID)
				assert.Len(t, manager.endpoints, len(tt.urls))
				assert.NotNil(t, manager.HealthMonitor)
			}
		})
	}
}

func TestManager_Start_Success(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   1,
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	manager := NewManager(
		"eip155:1",
		[]string{"http://test1.com", "http://test2.com"},
		poolConfig,
		mockClientFactory(false),
		logger,
	)
	require.NotNil(t, manager)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	assert.NoError(t, err)
	
	// Verify healthy endpoints
	assert.Equal(t, 2, manager.GetHealthyEndpointCount())
	
	// Clean up
	manager.Stop()
}

func TestManager_Start_InsufficientHealthyEndpoints(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   2, // Require 2 healthy endpoints
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	manager := NewManager(
		"eip155:1",
		[]string{"http://test1.com", "http://test2.com"},
		poolConfig,
		mockClientFactory(true), // All clients fail to initialize
		logger,
	)
	require.NotNil(t, manager)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient healthy endpoints")
}

func TestManager_SelectEndpoint(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   1,
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	manager := NewManager(
		"eip155:1",
		[]string{"http://test1.com", "http://test2.com"},
		poolConfig,
		mockClientFactory(false),
		logger,
	)
	require.NotNil(t, manager)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()
	
	// Test endpoint selection
	endpoint, err := manager.SelectEndpoint()
	assert.NoError(t, err)
	assert.NotNil(t, endpoint)
	assert.Contains(t, []string{"http://test1.com", "http://test2.com"}, endpoint.URL)
	
	// Verify last used time is set
	assert.True(t, time.Since(endpoint.LastUsed) < time.Second)
}

func TestManager_SelectEndpoint_NoHealthyEndpoints(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   1,
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	manager := NewManager(
		"eip155:1",
		[]string{"http://test1.com"},
		poolConfig,
		mockClientFactory(false),
		logger,
	)
	require.NotNil(t, manager)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()
	
	// Mark the only endpoint as excluded
	manager.endpoints[0].UpdateState(StateExcluded)
	
	// Should fail to select endpoint
	endpoint, err := manager.SelectEndpoint()
	assert.Error(t, err)
	assert.Nil(t, endpoint)
	assert.Contains(t, err.Error(), "no healthy endpoints available")
}

func TestManager_UpdateEndpointMetrics(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   1,
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	manager := NewManager(
		"eip155:1",
		[]string{"http://test1.com"},
		poolConfig,
		mockClientFactory(false),
		logger,
	)
	require.NotNil(t, manager)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()
	
	endpoint := manager.endpoints[0]
	latency := 50 * time.Millisecond
	
	// Test successful request
	manager.UpdateEndpointMetrics(endpoint, true, latency, nil)
	assert.Equal(t, uint64(1), endpoint.Metrics.TotalRequests)
	assert.Equal(t, uint64(1), endpoint.Metrics.SuccessfulRequests)
	
	// Test failed request that should lead to exclusion after threshold
	for i := 0; i < 3; i++ { // UnhealthyThreshold is 3
		manager.UpdateEndpointMetrics(endpoint, false, latency, assert.AnError)
	}
	
	// Endpoint should be excluded due to consecutive failures
	assert.Equal(t, StateExcluded, endpoint.GetState())
}

func TestManager_GetEndpointStats(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   1,
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	manager := NewManager(
		"eip155:1",
		[]string{"http://test1.com", "http://test2.com"},
		poolConfig,
		mockClientFactory(false),
		logger,
	)
	require.NotNil(t, manager)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()
	
	stats := manager.GetEndpointStats()
	
	assert.Equal(t, "round-robin", stats.Strategy)
	assert.Equal(t, 2, stats.TotalEndpoints)
	
	endpoints := stats.Endpoints
	assert.Len(t, endpoints, 2)
	
	endpoint1 := endpoints[0]
	assert.Contains(t, []string{"http://test1.com", "http://test2.com"}, endpoint1.URL)
	assert.Equal(t, "healthy", endpoint1.State)
}

func TestManager_Stop(t *testing.T) {
	logger := zerolog.Nop()
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   30 * time.Second,
		UnhealthyThreshold:    3,
		RecoveryInterval:      5 * time.Minute,
		MinHealthyEndpoints:   1,
		RequestTimeout:        10 * time.Second,
		LoadBalancingStrategy: "round-robin",
	}
	
	manager := NewManager(
		"eip155:1",
		[]string{"http://test1.com"},
		poolConfig,
		mockClientFactory(false),
		logger,
	)
	require.NotNil(t, manager)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	
	// Verify client is not closed initially
	client := manager.endpoints[0].GetClient().(*mockClient)
	assert.False(t, client.closed)
	
	// Stop the manager
	manager.Stop()
	
	// Verify client is closed
	assert.True(t, client.closed)
}