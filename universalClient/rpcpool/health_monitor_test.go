package rpcpool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockHealthChecker is a mock implementation of HealthChecker
type MockHealthChecker struct {
	mock.Mock
}

func (m *MockHealthChecker) CheckHealth(ctx context.Context, client Client) error {
	args := m.Called(ctx, client)
	return args.Error(0)
}

// MockManager is a mock implementation of Manager for testing
type MockManager struct {
	endpoints []*Endpoint
	chainID   string
	selector  *EndpointSelector
}

func (m *MockManager) GetEndpoints() []*Endpoint {
	return m.endpoints
}

func (m *MockManager) UpdateEndpointMetrics(endpoint *Endpoint, success bool, latency time.Duration, err error) {
	// Update the endpoint metrics
	if success {
		endpoint.Metrics.UpdateSuccess(latency)
	} else {
		endpoint.Metrics.UpdateFailure(err, latency)
	}
}

// MockClient represents a mock RPC client for testing
type MockClient struct {
	healthy bool
}

func (m *MockClient) Ping(ctx context.Context) error {
	if m.healthy {
		return nil
	}
	return errors.New("unhealthy")
}

func (m *MockClient) Close() error {
	return nil
}

func setupTestHealthMonitor(t *testing.T) (*HealthMonitor, *Manager, *config.RPCPoolConfig) {
	cfg := &config.RPCPoolConfig{
		HealthCheckIntervalSeconds: 1,
		RequestTimeoutSeconds:      1,
		RecoveryIntervalSeconds:    2,
	}
	
	manager := &MockManager{
		chainID:   "test-chain",
		endpoints: []*Endpoint{},
		selector:  NewEndpointSelector(StrategyRoundRobin),
	}
	
	// We need to create the actual Manager for NewHealthMonitor
	realManager := &Manager{
		chainID:   "test-chain",
		endpoints: manager.endpoints,
		selector:  manager.selector,
		logger:    zerolog.Nop(),
		config: &config.RPCPoolConfig{
			UnhealthyThreshold: 3,
		},
	}
	
	monitor := NewHealthMonitor(realManager, cfg, zerolog.Nop())
	
	return monitor, realManager, cfg
}

func TestNewHealthMonitor(t *testing.T) {
	monitor, manager, cfg := setupTestHealthMonitor(t)
	
	assert.NotNil(t, monitor)
	assert.Equal(t, manager, monitor.manager)
	assert.Equal(t, cfg, monitor.config)
	assert.NotNil(t, monitor.stopCh)
}

func TestHealthMonitor_SetHealthChecker(t *testing.T) {
	monitor, _, _ := setupTestHealthMonitor(t)
	
	checker := &MockHealthChecker{}
	monitor.SetHealthChecker(checker)
	
	assert.Equal(t, checker, monitor.healthChecker)
}

func TestHealthMonitor_StartStop(t *testing.T) {
	monitor, _, _ := setupTestHealthMonitor(t)
	
	ctx := context.Background()
	var wg sync.WaitGroup
	
	// Start the monitor
	wg.Add(1)
	go monitor.Start(ctx, &wg)
	
	// Give it time to start
	time.Sleep(100 * time.Millisecond)
	
	// Stop the monitor
	monitor.Stop()
	
	// Wait for goroutine to finish
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Monitor did not stop in time")
	}
}

func TestHealthMonitor_performHealthChecks(t *testing.T) {
	monitor, manager, _ := setupTestHealthMonitor(t)
	
	// Create test endpoints
	endpoint1 := &Endpoint{
		URL:       "http://localhost:8545",
		State:     StateHealthy,
		Metrics:   &EndpointMetrics{},
		LastUsed:  time.Now(),
		Client:    &MockClient{healthy: true},
	}
	
	endpoint2 := &Endpoint{
		URL:       "http://localhost:8546",
		State:     StateHealthy,
		Metrics:   &EndpointMetrics{},
		LastUsed:  time.Now(),
		Client:    &MockClient{healthy: false},
	}
	
	manager.endpoints = []*Endpoint{endpoint1, endpoint2}
	
	// Setup health checker
	checker := &MockHealthChecker{}
	checker.On("CheckHealth", mock.Anything, &MockClient{healthy: true}).Return(nil)
	checker.On("CheckHealth", mock.Anything, &MockClient{healthy: false}).Return(errors.New("connection failed"))
	monitor.SetHealthChecker(checker)
	
	// Perform health checks
	ctx := context.Background()
	monitor.performHealthChecks(ctx)
	
	// Verify health checks were performed
	checker.AssertExpectations(t)
	
	// Check metrics were updated
	assert.Greater(t, endpoint1.Metrics.SuccessfulRequests, uint64(0))
	assert.Greater(t, endpoint2.Metrics.FailedRequests, uint64(0))
}

func TestHealthMonitor_checkEndpointHealth(t *testing.T) {
	tests := []struct {
		name             string
		endpoint         *Endpoint
		setupMock        func(*MockHealthChecker, *Endpoint)
		expectedState    EndpointState
		hasHealthChecker bool
		expectSuccess    bool
	}{
		{
			name: "healthy endpoint",
			endpoint: &Endpoint{
				URL:      "http://localhost:8545",
				State:    StateHealthy,
				Metrics:  &EndpointMetrics{},
				LastUsed: time.Now(),
				Client:   &MockClient{healthy: true},
			},
			setupMock: func(m *MockHealthChecker, e *Endpoint) {
				m.On("CheckHealth", mock.Anything, e.Client).Return(nil)
			},
			expectedState:    StateHealthy,
			hasHealthChecker: true,
			expectSuccess:    true,
		},
		{
			name: "unhealthy endpoint",
			endpoint: &Endpoint{
				URL:      "http://localhost:8546",
				State:    StateHealthy,
				Metrics:  &EndpointMetrics{},
				LastUsed: time.Now(),
				Client:   &MockClient{healthy: false},
			},
			setupMock: func(m *MockHealthChecker, e *Endpoint) {
				m.On("CheckHealth", mock.Anything, e.Client).Return(errors.New("connection failed"))
			},
			expectedState:    StateDegraded, // Failed health check with success rate < 0.5 downgrades to degraded
			hasHealthChecker: true,
			expectSuccess:    false,
		},
		{
			name: "no client",
			endpoint: &Endpoint{
				URL:      "http://localhost:8547",
				State:    StateHealthy,
				Metrics:  &EndpointMetrics{},
				LastUsed: time.Now(),
				Client:   nil,
			},
			setupMock:        nil,
			expectedState:    StateHealthy,
			hasHealthChecker: true,
			expectSuccess:    false,
		},
		{
			name: "no health checker",
			endpoint: &Endpoint{
				URL:      "http://localhost:8548",
				State:    StateHealthy,
				Metrics:  &EndpointMetrics{},
				LastUsed: time.Now(),
				Client:   &MockClient{healthy: true},
			},
			setupMock:        nil,
			expectedState:    StateHealthy,
			hasHealthChecker: false,
			expectSuccess:    false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, _, _ := setupTestHealthMonitor(t)
			
			if tt.hasHealthChecker {
				checker := &MockHealthChecker{}
				if tt.setupMock != nil && tt.endpoint.Client != nil {
					tt.setupMock(checker, tt.endpoint)
				}
				monitor.SetHealthChecker(checker)
			}
			
			ctx := context.Background()
			monitor.checkEndpointHealth(ctx, tt.endpoint)
			
			assert.Equal(t, tt.expectedState, tt.endpoint.State)
			
			if tt.hasHealthChecker && tt.endpoint.Client != nil && tt.setupMock != nil {
				if tt.expectSuccess {
					assert.Greater(t, tt.endpoint.Metrics.SuccessfulRequests, uint64(0))
					assert.Equal(t, uint64(0), tt.endpoint.Metrics.FailedRequests)
				} else {
					assert.Equal(t, uint64(0), tt.endpoint.Metrics.SuccessfulRequests)
					assert.Greater(t, tt.endpoint.Metrics.FailedRequests, uint64(0))
				}
				
				// Verify mock expectations were called
				if tt.hasHealthChecker {
					checker := monitor.healthChecker.(*MockHealthChecker)
					checker.AssertExpectations(t)
				}
			}
		})
	}
}

func TestHealthMonitor_handleExcludedEndpointCheck(t *testing.T) {
	tests := []struct {
		name           string
		success        bool
		timeSinceExcl  time.Duration
		expectedState  EndpointState
		shouldRecover  bool
	}{
		{
			name:           "successful recovery after interval",
			success:        true,
			timeSinceExcl:  3 * time.Second,
			expectedState:  StateDegraded,
			shouldRecover:  true,
		},
		{
			name:           "failed recovery after interval",
			success:        false,
			timeSinceExcl:  3 * time.Second,
			expectedState:  StateExcluded,
			shouldRecover:  false,
		},
		{
			name:           "too soon for recovery",
			success:        true,
			timeSinceExcl:  1 * time.Second,
			expectedState:  StateExcluded,
			shouldRecover:  false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, _, _ := setupTestHealthMonitor(t)
			
			endpoint := &Endpoint{
				URL:        "http://localhost:8545",
				State:      StateExcluded,
				Metrics:    &EndpointMetrics{},
				ExcludedAt: time.Now().Add(-tt.timeSinceExcl),
			}
			
			var err error
			if !tt.success {
				err = errors.New("connection failed")
			}
			
			monitor.handleExcludedEndpointCheck(endpoint, tt.success, 100*time.Millisecond, err)
			
			assert.Equal(t, tt.expectedState, endpoint.State)
			
			if tt.shouldRecover && tt.success {
				assert.Equal(t, float64(70.0), endpoint.Metrics.HealthScore)
			}
		})
	}
}

func TestHealthMonitor_GetHealthStatus(t *testing.T) {
	monitor, manager, _ := setupTestHealthMonitor(t)
	
	// Create test endpoints with different states
	endpoints := []*Endpoint{
		{
			URL:      "http://localhost:8545",
			State:    StateHealthy,
			Metrics:  &EndpointMetrics{HealthScore: 95.0},
			LastUsed: time.Now(),
		},
		{
			URL:      "http://localhost:8546",
			State:    StateDegraded,
			Metrics:  &EndpointMetrics{HealthScore: 60.0},
			LastUsed: time.Now(),
		},
		{
			URL:     "http://localhost:8547",
			State:   StateUnhealthy,
			Metrics: &EndpointMetrics{HealthScore: 30.0, LastError: errors.New("connection timeout")},
			LastUsed: time.Now(),
		},
		{
			URL:        "http://localhost:8548",
			State:      StateExcluded,
			Metrics:    &EndpointMetrics{HealthScore: 0.0},
			LastUsed:   time.Now(),
			ExcludedAt: time.Now(),
		},
	}
	
	manager.endpoints = endpoints
	
	status := monitor.GetHealthStatus()
	
	assert.NotNil(t, status)
	assert.Equal(t, "test-chain", status.ChainID)
	assert.Equal(t, 4, status.TotalEndpoints)
	assert.Equal(t, 1, status.HealthyCount)
	assert.Equal(t, 1, status.DegradedCount)
	assert.Equal(t, 1, status.UnhealthyCount)
	assert.Equal(t, 1, status.ExcludedCount)
	assert.Equal(t, "round-robin", status.Strategy)
	assert.Len(t, status.Endpoints, 4)
	
	// Verify endpoint statuses
	assert.Equal(t, "healthy", status.Endpoints[0].State)
	assert.Equal(t, float64(95.0), status.Endpoints[0].HealthScore)
	
	assert.Equal(t, "degraded", status.Endpoints[1].State)
	assert.Equal(t, float64(60.0), status.Endpoints[1].HealthScore)
	
	assert.Equal(t, "unhealthy", status.Endpoints[2].State)
	assert.Equal(t, "connection timeout", status.Endpoints[2].LastError)
	
	assert.Equal(t, "excluded", status.Endpoints[3].State)
}

func TestHealthMonitor_contextCancellation(t *testing.T) {
	monitor, _, _ := setupTestHealthMonitor(t)
	
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	
	// Start the monitor
	wg.Add(1)
	go monitor.Start(ctx, &wg)
	
	// Give it time to start
	time.Sleep(100 * time.Millisecond)
	
	// Cancel context
	cancel()
	
	// Wait for goroutine to finish
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Monitor did not stop on context cancellation")
	}
}

func TestHealthMonitor_defaultConfigurations(t *testing.T) {
	// Test with zero/negative config values to ensure defaults are used
	cfg := &config.RPCPoolConfig{
		HealthCheckIntervalSeconds: 0,
		RequestTimeoutSeconds:      -1,
		RecoveryIntervalSeconds:    0,
	}
	
	manager := &MockManager{
		chainID:   "test-chain",
		endpoints: []*Endpoint{},
		selector:  NewEndpointSelector(StrategyRoundRobin),
	}
	
	// We need to create the actual Manager for NewHealthMonitor
	realManager := &Manager{
		chainID:   "test-chain",
		endpoints: manager.endpoints,
		selector:  manager.selector,
		logger:    zerolog.Nop(),
		config: &config.RPCPoolConfig{
			UnhealthyThreshold: 3,
		},
	}
	
	monitor := NewHealthMonitor(realManager, cfg, zerolog.Nop())
	
	// Create an endpoint for testing
	endpoint := &Endpoint{
		URL:        "http://localhost:8545",
		State:      StateExcluded,
		Metrics:    &EndpointMetrics{},
		ExcludedAt: time.Now().Add(-6 * time.Minute), // Excluded 6 minutes ago
		Client:     &MockClient{healthy: true},
	}
	
	// Setup health checker
	checker := &MockHealthChecker{}
	checker.On("CheckHealth", mock.Anything, endpoint.Client).Return(nil)
	monitor.SetHealthChecker(checker)
	
	// Test that defaults are applied (recovery after 5 minutes)
	monitor.handleExcludedEndpointCheck(endpoint, true, 100*time.Millisecond, nil)
	
	// Should recover since 6 minutes > default 5 minutes
	assert.Equal(t, StateDegraded, endpoint.State)
}

func TestHealthMonitor_immediateHealthCheck(t *testing.T) {
	monitor, manager, _ := setupTestHealthMonitor(t)
	
	// Create test endpoint
	endpoint := &Endpoint{
		URL:      "http://localhost:8545",
		State:    StateHealthy,
		Metrics:  &EndpointMetrics{},
		LastUsed: time.Now(),
		Client:   &MockClient{healthy: true},
	}
	
	manager.endpoints = []*Endpoint{endpoint}
	
	// Setup health checker
	checkCount := 0
	checker := &MockHealthChecker{}
	checker.On("CheckHealth", mock.Anything, endpoint.Client).Return(nil).Run(func(args mock.Arguments) {
		checkCount++
	})
	monitor.SetHealthChecker(checker)
	
	ctx := context.Background()
	var wg sync.WaitGroup
	
	// Start the monitor
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Run for a short time
		ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		
		var innerWg sync.WaitGroup
		innerWg.Add(1)
		go monitor.Start(ctx, &innerWg)
		innerWg.Wait()
	}()
	
	// Wait for completion
	wg.Wait()
	
	// Should have performed at least one immediate health check
	assert.GreaterOrEqual(t, checkCount, 1)
}

// Ensure MockHealthChecker implements HealthChecker interface
var _ HealthChecker = (*MockHealthChecker)(nil)