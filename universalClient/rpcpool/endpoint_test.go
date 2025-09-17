package rpcpool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockClient implements the Client interface for testing
type mockClient struct {
	shouldFail bool
	closed     bool
}

func (m *mockClient) Ping(ctx context.Context) error {
	if m.shouldFail {
		return errors.New("mock client ping failed")
	}
	return nil
}

func (m *mockClient) Close() error {
	m.closed = true
	return nil
}

func TestNewEndpoint(t *testing.T) {
	endpoint := NewEndpoint("http://test.com")
	
	assert.Equal(t, "http://test.com", endpoint.URL)
	assert.Equal(t, StateHealthy, endpoint.State)
	assert.NotNil(t, endpoint.Metrics)
	assert.Equal(t, 100.0, endpoint.Metrics.HealthScore)
}

func TestEndpoint_SetAndGetClient(t *testing.T) {
	endpoint := NewEndpoint("http://test.com")
	client := &mockClient{}
	
	endpoint.SetClient(client)
	retrievedClient := endpoint.GetClient()
	
	assert.Equal(t, client, retrievedClient)
}

func TestEndpoint_StateManagement(t *testing.T) {
	endpoint := NewEndpoint("http://test.com")
	
	// Initial state
	assert.Equal(t, StateHealthy, endpoint.GetState())
	assert.True(t, endpoint.IsHealthy())
	
	// Change to degraded
	endpoint.UpdateState(StateDegraded)
	assert.Equal(t, StateDegraded, endpoint.GetState())
	assert.True(t, endpoint.IsHealthy()) // degraded is still considered healthy
	
	// Change to excluded
	before := time.Now()
	endpoint.UpdateState(StateExcluded)
	after := time.Now()
	
	assert.Equal(t, StateExcluded, endpoint.GetState())
	assert.False(t, endpoint.IsHealthy())
	assert.True(t, endpoint.ExcludedAt.After(before) || endpoint.ExcludedAt.Equal(before))
	assert.True(t, endpoint.ExcludedAt.Before(after) || endpoint.ExcludedAt.Equal(after))
}

func TestEndpointMetrics_UpdateSuccess(t *testing.T) {
	metrics := &EndpointMetrics{HealthScore: 100.0}
	latency := 50 * time.Millisecond
	
	metrics.UpdateSuccess(latency)
	
	assert.Equal(t, uint64(1), metrics.TotalRequests)
	assert.Equal(t, uint64(1), metrics.SuccessfulRequests)
	assert.Equal(t, uint64(0), metrics.FailedRequests)
	assert.Equal(t, 0, metrics.ConsecutiveFailures)
	assert.Equal(t, latency, metrics.AverageLatency)
	assert.Equal(t, 1.0, metrics.GetSuccessRate())
}

func TestEndpointMetrics_UpdateFailure(t *testing.T) {
	metrics := &EndpointMetrics{HealthScore: 100.0}
	err := errors.New("test error")
	latency := 100 * time.Millisecond
	
	metrics.UpdateFailure(err, latency)
	
	assert.Equal(t, uint64(1), metrics.TotalRequests)
	assert.Equal(t, uint64(0), metrics.SuccessfulRequests)
	assert.Equal(t, uint64(1), metrics.FailedRequests)
	assert.Equal(t, 1, metrics.ConsecutiveFailures)
	assert.Equal(t, err, metrics.LastError)
	assert.Equal(t, 0.0, metrics.GetSuccessRate())
	assert.True(t, metrics.GetHealthScore() < 100.0) // Health score should decrease
}

func TestEndpointMetrics_HealthScoreCalculation(t *testing.T) {
	tests := []struct {
		name            string
		setupFunc       func(*EndpointMetrics)
		expectedMinScore float64
		expectedMaxScore float64
	}{
		{
			name: "perfect health",
			setupFunc: func(m *EndpointMetrics) {
				m.UpdateSuccess(10 * time.Millisecond)
				m.UpdateSuccess(10 * time.Millisecond)
			},
			expectedMinScore: 100.0,
			expectedMaxScore: 100.0,
		},
		{
			name: "mixed results",
			setupFunc: func(m *EndpointMetrics) {
				m.UpdateSuccess(10 * time.Millisecond)
				m.UpdateFailure(errors.New("error"), 10*time.Millisecond)
			},
			expectedMinScore: 40.0, // 50% success rate - 10 points for consecutive failure
			expectedMaxScore: 60.0,
		},
		{
			name: "high latency penalty",
			setupFunc: func(m *EndpointMetrics) {
				m.UpdateSuccess(5 * time.Second) // High latency
			},
			expectedMinScore: 75.0, // 100 - 20 (max latency penalty)
			expectedMaxScore: 85.0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := &EndpointMetrics{HealthScore: 100.0}
			tt.setupFunc(metrics)
			
			score := metrics.GetHealthScore()
			assert.GreaterOrEqual(t, score, tt.expectedMinScore)
			assert.LessOrEqual(t, score, tt.expectedMaxScore)
		})
	}
}

func TestEndpointMetrics_ConsecutiveFailures(t *testing.T) {
	metrics := &EndpointMetrics{HealthScore: 100.0}
	
	// Add some failures
	metrics.UpdateFailure(errors.New("error1"), 0)
	assert.Equal(t, 1, metrics.GetConsecutiveFailures())
	
	metrics.UpdateFailure(errors.New("error2"), 0)
	assert.Equal(t, 2, metrics.GetConsecutiveFailures())
	
	// Success should reset consecutive failures
	metrics.UpdateSuccess(10 * time.Millisecond)
	assert.Equal(t, 0, metrics.GetConsecutiveFailures())
}

func TestEndpointMetrics_ThreadSafety(t *testing.T) {
	metrics := &EndpointMetrics{HealthScore: 100.0}
	
	// Run concurrent operations
	done := make(chan bool, 100)
	
	// Start 50 goroutines doing success updates
	for i := 0; i < 50; i++ {
		go func() {
			metrics.UpdateSuccess(10 * time.Millisecond)
			done <- true
		}()
	}
	
	// Start 50 goroutines doing failure updates
	for i := 0; i < 50; i++ {
		go func() {
			metrics.UpdateFailure(errors.New("test"), 10*time.Millisecond)
			done <- true
		}()
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 100; i++ {
		<-done
	}
	
	// Verify final state is consistent
	assert.Equal(t, uint64(100), metrics.TotalRequests)
	assert.Equal(t, uint64(50), metrics.SuccessfulRequests)
	assert.Equal(t, uint64(50), metrics.FailedRequests)
	assert.Equal(t, 0.5, metrics.GetSuccessRate())
}