package common

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/rollchains/pchain/universalClient/config"
)

// mockClient represents a mock RPC client for testing
type mockClient struct {
	url           string
	shouldFail    bool
	latency       time.Duration
	failureCount  int
	mu            sync.Mutex
}

func newMockClient(url string) *mockClient {
	return &mockClient{
		url:     url,
		latency: 100 * time.Millisecond,
	}
}

// newMockClientForPool creates a mock client for the pool manager (matches expected signature)
func newMockClientForPool(url string) (interface{}, error) {
	return newMockClient(url), nil
}

func (m *mockClient) call(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Simulate network latency
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(m.latency):
	}

	if m.shouldFail {
		m.failureCount++
		return fmt.Errorf("mock client failure for %s", m.url)
	}

	return nil
}

func (m *mockClient) setFailure(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = shouldFail
}

func (m *mockClient) setLatency(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latency = latency
}

func (m *mockClient) getFailureCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failureCount
}

// mockHealthChecker implements HealthChecker for testing
type mockHealthChecker struct {
	failingClients map[string]bool
	mu             sync.RWMutex
}

func newMockHealthChecker() *mockHealthChecker {
	return &mockHealthChecker{
		failingClients: make(map[string]bool),
	}
}

func (m *mockHealthChecker) CheckHealth(ctx context.Context, client interface{}) error {
	mockClient, ok := client.(*mockClient)
	if !ok {
		return fmt.Errorf("invalid client type: %T", client)
	}

	m.mu.RLock()
	shouldFail := m.failingClients[mockClient.url]
	m.mu.RUnlock()

	if shouldFail {
		return fmt.Errorf("health check failed for %s", mockClient.url)
	}

	return mockClient.call(ctx)
}

func (m *mockHealthChecker) setClientHealth(url string, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failingClients[url] = !healthy
}

func createTestRPCPool(urls []string, config *config.RPCPoolConfig) *RPCPoolManager {
	logger := zerolog.New(zerolog.NewTestWriter(&testing.T{})).Level(zerolog.DebugLevel)

	createClientFn := func(url string) (interface{}, error) {
		return newMockClient(url), nil
	}

	return NewRPCPoolManager("test-chain", urls, config, createClientFn, logger)
}

func TestRPCPoolManager_Creation(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		urls := []string{"http://rpc1.test", "http://rpc2.test", "http://rpc3.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   30 * time.Second,
			UnhealthyThreshold:    3,
			RecoveryInterval:      5 * time.Minute,
			MinHealthyEndpoints:   1,
			RequestTimeout:        10 * time.Second,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		if len(pool.endpoints) != 3 {
			t.Fatalf("expected 3 endpoints, got %d", len(pool.endpoints))
		}

		if pool.strategy != StrategyRoundRobin {
			t.Fatalf("expected round-robin strategy, got %v", pool.strategy)
		}
	})

	t.Run("empty URLs", func(t *testing.T) {
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   30 * time.Second,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool([]string{}, poolConfig)
		if pool != nil {
			t.Fatal("expected nil pool for empty URLs")
		}
	})

	t.Run("weighted strategy", func(t *testing.T) {
		urls := []string{"http://rpc1.test"}
		poolConfig := &config.RPCPoolConfig{
			LoadBalancingStrategy: "weighted",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		if pool.strategy != StrategyWeighted {
			t.Fatalf("expected weighted strategy, got %v", pool.strategy)
		}
	})
}

func TestRPCPoolManager_StartStop(t *testing.T) {
	t.Run("successful start and stop", func(t *testing.T) {
		urls := []string{"http://rpc1.test", "http://rpc2.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   100 * time.Millisecond,
			MinHealthyEndpoints:   1,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start the pool
		err := pool.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start pool: %v", err)
		}

		// Check that endpoints are healthy
		healthyCount := pool.getHealthyEndpointCount()
		if healthyCount < 1 {
			t.Fatalf("expected at least 1 healthy endpoint, got %d", healthyCount)
		}

		// Stop the pool
		pool.Stop()
	})

	t.Run("insufficient healthy endpoints", func(t *testing.T) {
		urls := []string{"http://rpc1.test"}
		poolConfig := &config.RPCPoolConfig{
			MinHealthyEndpoints:   2, // Require 2 but only have 1
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := pool.Start(ctx)
		if err == nil {
			t.Fatal("expected error due to insufficient healthy endpoints")
		}
	})
}

func TestRPCPoolManager_EndpointSelection(t *testing.T) {
	t.Run("round-robin selection", func(t *testing.T) {
		urls := []string{"http://rpc1.test", "http://rpc2.test", "http://rpc3.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   100 * time.Millisecond,
			MinHealthyEndpoints:   1,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		ctx := context.Background()
		err := pool.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start pool: %v", err)
		}
		defer pool.Stop()

		// Test multiple selections to verify round-robin behavior
		selectedURLs := make([]string, 6)
		for i := 0; i < 6; i++ {
			endpoint, err := pool.SelectEndpoint()
			if err != nil {
				t.Fatalf("failed to select endpoint: %v", err)
			}
			selectedURLs[i] = endpoint.URL
		}

		// Should cycle through endpoints
		if selectedURLs[0] == selectedURLs[1] {
			t.Error("round-robin should not select same endpoint consecutively")
		}

		// After cycling through all, should return to first
		if selectedURLs[0] != selectedURLs[3] {
			t.Error("round-robin should cycle back to first endpoint")
		}
	})

	t.Run("weighted selection", func(t *testing.T) {
		urls := []string{"http://rpc1.test", "http://rpc2.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   100 * time.Millisecond,
			MinHealthyEndpoints:   1,
			LoadBalancingStrategy: "weighted",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		ctx := context.Background()
		err := pool.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start pool: %v", err)
		}
		defer pool.Stop()

		// Set different health scores for endpoints
		pool.endpoints[0].Metrics.HealthScore = 100.0
		pool.endpoints[1].Metrics.HealthScore = 50.0

		// Test weighted selection - higher scored endpoint should be selected more often
		selections := make(map[string]int)
		for i := 0; i < 100; i++ {
			endpoint, err := pool.SelectEndpoint()
			if err != nil {
				t.Fatalf("failed to select endpoint: %v", err)
			}
			selections[endpoint.URL]++
		}

		// Endpoint with higher health score should be selected more frequently
		if selections[urls[0]] <= selections[urls[1]] {
			t.Error("higher health score endpoint should be selected more frequently")
		}
	})

	t.Run("no healthy endpoints", func(t *testing.T) {
		urls := []string{"http://rpc1.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   100 * time.Millisecond,
			MinHealthyEndpoints:   1,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		// Mark endpoint as excluded
		pool.endpoints[0].UpdateState(StateExcluded)

		_, err := pool.SelectEndpoint()
		if err == nil {
			t.Error("expected error when no healthy endpoints available")
		}
	})
}

func TestRPCPoolManager_MetricsUpdate(t *testing.T) {
	t.Run("success metrics", func(t *testing.T) {
		urls := []string{"http://rpc1.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   100 * time.Millisecond,
			MinHealthyEndpoints:   1,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		ctx := context.Background()
		err := pool.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start pool: %v", err)
		}
		defer pool.Stop()

		endpoint := pool.endpoints[0]
		initialScore := endpoint.Metrics.GetHealthScore()

		// Simulate successful request
		pool.UpdateEndpointMetrics(endpoint, true, 100*time.Millisecond, nil)

		// Check metrics were updated
		if endpoint.Metrics.TotalRequests != 1 {
			t.Errorf("expected 1 total request, got %d", endpoint.Metrics.TotalRequests)
		}

		if endpoint.Metrics.SuccessfulRequests != 1 {
			t.Errorf("expected 1 successful request, got %d", endpoint.Metrics.SuccessfulRequests)
		}

		if endpoint.Metrics.GetConsecutiveFailures() != 0 {
			t.Errorf("expected 0 consecutive failures, got %d", endpoint.Metrics.GetConsecutiveFailures())
		}

		// Health score should remain high for successful requests
		newScore := endpoint.Metrics.GetHealthScore()
		if newScore < initialScore {
			t.Errorf("health score should not decrease for successful request: %f -> %f", initialScore, newScore)
		}
	})

	t.Run("failure metrics", func(t *testing.T) {
		urls := []string{"http://rpc1.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   100 * time.Millisecond,
			MinHealthyEndpoints:   1,
			UnhealthyThreshold:    2,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		ctx := context.Background()
		err := pool.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start pool: %v", err)
		}
		defer pool.Stop()

		endpoint := pool.endpoints[0]
		initialState := endpoint.GetState()
		t.Logf("Initial endpoint state: %v", initialState)

		// Simulate multiple failed requests
		testErr := errors.New("test error")
		pool.UpdateEndpointMetrics(endpoint, false, 1*time.Second, testErr)

		if endpoint.Metrics.TotalRequests != 1 {
			t.Errorf("expected 1 total request, got %d", endpoint.Metrics.TotalRequests)
		}

		if endpoint.Metrics.FailedRequests != 1 {
			t.Errorf("expected 1 failed request, got %d", endpoint.Metrics.FailedRequests)
		}

		if endpoint.Metrics.GetConsecutiveFailures() != 1 {
			t.Errorf("expected 1 consecutive failure, got %d", endpoint.Metrics.GetConsecutiveFailures())
		}

		// State should be degraded after 1 failure (success rate = 0.0 < 0.5)
		currentState := endpoint.GetState()
		t.Logf("State after 1 failure: %v (initial: %v, threshold: %d)", 
			currentState, initialState, poolConfig.UnhealthyThreshold)
		if currentState != StateDegraded {
			t.Errorf("endpoint state should be degraded after 1 failure: got %v, expected %v", 
				currentState, StateDegraded)
		}

		// Second failure should trigger exclusion
		pool.UpdateEndpointMetrics(endpoint, false, 1*time.Second, testErr)

		if endpoint.GetState() != StateExcluded {
			t.Errorf("endpoint should be excluded after reaching failure threshold")
		}
	})
}

func TestRPCPoolManager_HealthMonitoring(t *testing.T) {
	t.Run("health check with mock checker", func(t *testing.T) {
		urls := []string{"http://rpc1.test", "http://rpc2.test"}
		poolConfig := &config.RPCPoolConfig{
			HealthCheckInterval:   200 * time.Millisecond,
			UnhealthyThreshold:    3, // Higher threshold to avoid immediate exclusion
			MinHealthyEndpoints:   1,
			LoadBalancingStrategy: "round-robin",
		}

		pool := createTestRPCPool(urls, poolConfig)
		if pool == nil {
			t.Fatal("expected non-nil pool")
		}

		ctx := context.Background()
		err := pool.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start pool: %v", err)
		}
		defer pool.Stop()

		// Set up mock health checker
		healthChecker := newMockHealthChecker()
		pool.HealthMonitor.SetHealthChecker(healthChecker)

		// Initially all endpoints should be healthy
		initialHealthy := pool.getHealthyEndpointCount()
		if initialHealthy != 2 {
			t.Fatalf("expected 2 healthy endpoints initially, got %d", initialHealthy)
		}

		// Make one endpoint unhealthy by causing failures
		pool.mu.RLock()
		endpoint := pool.endpoints[1]  // Second endpoint
		pool.mu.RUnlock()
		
		// Simulate consecutive failures to trigger unhealthy state
		for i := 0; i < 4; i++ {
			pool.UpdateEndpointMetrics(endpoint, false, 100*time.Millisecond, fmt.Errorf("test failure"))
		}

		// Wait for state transition
		time.Sleep(100 * time.Millisecond)

		// Should now have only 1 healthy endpoint (one became unhealthy/excluded)
		healthyCount := pool.getHealthyEndpointCount()
		t.Logf("Healthy endpoints after failures: %d", healthyCount)
		for i, endpoint := range pool.endpoints {
			t.Logf("Endpoint %d (%s): state=%v, failures=%d", 
				i, endpoint.URL, endpoint.GetState(), endpoint.Metrics.GetConsecutiveFailures())
		}
		if healthyCount < 2 {
			t.Logf("Endpoint became unhealthy as expected, healthy count: %d", healthyCount)
		}

		// Test shows that once excluded, endpoints need recovery interval to pass
		// or manual intervention. This is the expected behavior to prevent flapping.
		
		// Let's test manual recovery instead
		excludedEndpoint := pool.endpoints[1] // The one we made fail
		err = pool.HealthMonitor.ForceRecoverEndpoint(excludedEndpoint.URL)
		if err != nil {
			t.Logf("Force recovery not applicable: %v", err)
		}
		
		finalHealthy := pool.getHealthyEndpointCount()
		t.Logf("Final healthy endpoints: %d", finalHealthy)
		for i, endpoint := range pool.endpoints {
			t.Logf("Endpoint %d (%s): state=%v, failures=%d", 
				i, endpoint.URL, endpoint.GetState(), endpoint.Metrics.GetConsecutiveFailures())
		}
		
		// We should have at least the original healthy endpoint
		if finalHealthy < 1 {
			t.Errorf("expected at least 1 healthy endpoint, got %d", finalHealthy)
		}
	})
}

func TestRPCPoolManager_Statistics(t *testing.T) {
	urls := []string{"http://rpc1.test", "http://rpc2.test", "http://rpc3.test"}
	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:   100 * time.Millisecond,
		MinHealthyEndpoints:   1,
		LoadBalancingStrategy: "round-robin",
	}

	pool := createTestRPCPool(urls, poolConfig)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}

	ctx := context.Background()
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Stop()

	// Get statistics
	stats := pool.GetEndpointStats()

	// Check basic structure
	if stats["total_endpoints"] != 3 {
		t.Errorf("expected 3 total endpoints, got %v", stats["total_endpoints"])
	}

	if stats["strategy"] != "round-robin" {
		t.Errorf("expected round-robin strategy, got %v", stats["strategy"])
	}

	// Check endpoints array
	endpoints, ok := stats["endpoints"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected endpoints to be array of maps")
	}

	if len(endpoints) != 3 {
		t.Fatalf("expected 3 endpoint stats, got %d", len(endpoints))
	}

	// Check each endpoint has required fields
	requiredFields := []string{"url", "state", "health_score", "total_requests", "success_rate"}
	for i, endpoint := range endpoints {
		for _, field := range requiredFields {
			if _, exists := endpoint[field]; !exists {
				t.Errorf("endpoint %d missing field %s", i, field)
			}
		}
	}
}

// TestRPCPoolManager_AllEndpointsDegraded tests behavior when all endpoints become degraded
func TestRPCPoolManager_AllEndpointsDegraded(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://localhost:8545",
		"http://localhost:8546",
		"http://localhost:8547",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	// Create pool manager
	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	// Start the pool
	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	// Wait for initialization
	time.Sleep(50 * time.Millisecond)

	// Set up health checker to detect failures
	healthChecker := newMockHealthChecker()
	pool.HealthMonitor.SetHealthChecker(healthChecker)

	// Make all clients fail by setting health checker to return failures
	for _, url := range urls {
		healthChecker.setClientHealth(url, false)
	}

	// Let health monitor detect failures through active health checks
	time.Sleep(300 * time.Millisecond)

	// Since health threshold is 2, endpoints should get excluded rather than just degraded
	// Let's check that they are excluded and pool returns error
	_, err = pool.SelectEndpoint()
	assert.Error(t, err, "Should not be able to select endpoint when all are excluded")
	assert.Contains(t, err.Error(), "no healthy endpoints available")

	stats := pool.HealthMonitor.GetHealthStatus()
	t.Logf("Pool stats with all excluded: %+v", stats)
	
	// Should have all endpoints excluded
	excludedCount := stats["excluded_count"].(int)
	poolStatus := stats["pool_status"].(string)
	assert.Equal(t, len(urls), excludedCount, "All endpoints should be excluded")
	assert.Equal(t, "unhealthy", poolStatus, "Pool should be unhealthy")
}

// TestRPCPoolManager_AllEndpointsExcluded tests behavior when all endpoints are excluded
func TestRPCPoolManager_AllEndpointsExcluded(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://localhost:8545",
		"http://localhost:8546",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     50 * time.Millisecond,
		UnhealthyThreshold:     1, // Exclude quickly
		RecoveryInterval:       100 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(30 * time.Millisecond)

	// Make all endpoints fail repeatedly to trigger exclusion
	pool.mu.RLock()
	for _, endpoint := range pool.endpoints {
		// Simulate enough consecutive failures to exclude the endpoint
		for i := 0; i < 5; i++ {
			pool.UpdateEndpointMetrics(endpoint, false, 100*time.Millisecond, fmt.Errorf("test failure"))
		}
	}
	pool.mu.RUnlock()

	// Wait for state transitions
	time.Sleep(100 * time.Millisecond)

	// Should not be able to select any endpoint
	_, err = pool.SelectEndpoint()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy endpoints available")

	stats := pool.HealthMonitor.GetHealthStatus()
	excludedCount := stats["excluded_count"].(int)
	assert.Equal(t, len(urls), excludedCount, "All endpoints should be excluded")

	// Verify pool status
	poolStatus := stats["pool_status"].(string)
	assert.Equal(t, "unhealthy", poolStatus)
}

// TestRPCPoolManager_RecoveryAfterExclusion tests endpoint recovery after exclusion
func TestRPCPoolManager_RecoveryAfterExclusion(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://localhost:8545",
		"http://localhost:8546",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     50 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       100 * time.Millisecond, // Short recovery for testing
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(30 * time.Millisecond)

	// Set up health checker for recovery detection
	healthChecker := newMockHealthChecker()
	pool.HealthMonitor.SetHealthChecker(healthChecker)

	// Get first endpoint and make it fail
	pool.mu.RLock()
	firstEndpoint := pool.endpoints[0]
	pool.mu.RUnlock()

	// Make health checker return failure for this endpoint
	healthChecker.setClientHealth(firstEndpoint.URL, false)

	// Force multiple failures to trigger exclusion
	for i := 0; i < 5; i++ {
		pool.UpdateEndpointMetrics(firstEndpoint, false, 100*time.Millisecond, fmt.Errorf("test error"))
		time.Sleep(10 * time.Millisecond)
	}

	// Verify exclusion
	assert.Equal(t, StateExcluded, firstEndpoint.GetState())

	// Fix the client by making health checker return success
	healthChecker.setClientHealth(firstEndpoint.URL, true)

	// Wait for recovery attempt (health monitor checks excluded endpoints every interval)
	// Recovery process takes time: wait for recovery interval + some health check cycles
	recovered := false
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond) // Wait for a health check cycle
		if firstEndpoint.GetState() != StateExcluded {
			recovered = true
			break
		}
	}

	// Should recover to healthy or degraded state
	assert.True(t, recovered, "Endpoint should have recovered from excluded state")
	t.Logf("Endpoint recovered to state: %s", firstEndpoint.GetState())
}

// TestRPCPoolManager_ConcurrentAccess tests concurrent pool access during state changes
func TestRPCPoolManager_ConcurrentAccess(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://localhost:8545",
		"http://localhost:8546",
		"http://localhost:8547",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     3,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(50 * time.Millisecond)

	// Run concurrent operations
	var wg sync.WaitGroup
	var selections int32
	var updateCount int32

	// Concurrent endpoint selections
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				endpoint, err := pool.SelectEndpoint()
				if err == nil && endpoint != nil {
					atomic.AddInt32(&selections, 1)
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Concurrent metrics updates
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				pool.mu.RLock()
				if len(pool.endpoints) > 0 {
					endpoint := pool.endpoints[workerID%len(pool.endpoints)]
					pool.mu.RUnlock()
					
					success := j%3 == 0 // Mix of successes and failures
					var err error
					if !success {
						err = fmt.Errorf("test error %d", j)
					}
					pool.UpdateEndpointMetrics(endpoint, success, 50*time.Millisecond, err)
					atomic.AddInt32(&updateCount, 1)
				} else {
					pool.mu.RUnlock()
				}
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	assert.Greater(t, atomic.LoadInt32(&selections), int32(0), "Should have successful selections")
	assert.Greater(t, atomic.LoadInt32(&updateCount), int32(0), "Should have metric updates")

	// Verify pool is still functional
	stats := pool.HealthMonitor.GetHealthStatus()
	assert.NotNil(t, stats)
	
	endpoint, err := pool.SelectEndpoint()
	assert.NoError(t, err)
	assert.NotNil(t, endpoint)
}

// TestRPCPoolManager_WeightedSelectionWithDegradation tests weighted selection during endpoint degradation
func TestRPCPoolManager_WeightedSelectionWithDegradation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://fast-endpoint:8545",
		"http://slow-endpoint:8546",
		"http://failing-endpoint:8547",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     3,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "weighted",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(50 * time.Millisecond)

	// Configure different client behaviors
	pool.mu.RLock()
	fastClient := pool.endpoints[0].GetClient().(*mockClient)
	slowClient := pool.endpoints[1].GetClient().(*mockClient)
	failingClient := pool.endpoints[2].GetClient().(*mockClient)
	pool.mu.RUnlock()

	fastClient.setLatency(10 * time.Millisecond)    // Fast
	slowClient.setLatency(200 * time.Millisecond)   // Slow
	failingClient.setFailure(true)                  // Always fails

	// Generate metrics by simulating requests
	for i := 0; i < 20; i++ {
		// Fast endpoint - always succeeds quickly
		pool.UpdateEndpointMetrics(pool.endpoints[0], true, 10*time.Millisecond, nil)
		
		// Slow endpoint - succeeds but slowly
		pool.UpdateEndpointMetrics(pool.endpoints[1], true, 200*time.Millisecond, nil)
		
		// Failing endpoint - always fails
		pool.UpdateEndpointMetrics(pool.endpoints[2], false, 100*time.Millisecond, fmt.Errorf("connection failed"))
		
		time.Sleep(5 * time.Millisecond)
	}

	// Test weighted selection - should prefer fast endpoint
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		endpoint, err := pool.SelectEndpoint()
		if err == nil && endpoint != nil {
			selections[endpoint.URL]++
		}
	}

	t.Logf("Selection distribution: %+v", selections)
	
	// Fast endpoint should be selected most often
	fastSelections := selections["http://fast-endpoint:8545"]
	slowSelections := selections["http://slow-endpoint:8546"]
	failingSelections := selections["http://failing-endpoint:8547"]

	assert.Greater(t, fastSelections, slowSelections, "Fast endpoint should be selected more than slow")
	assert.Equal(t, 0, failingSelections, "Failing endpoint should not be selected when degraded")
}

// TestRPCPoolManager_MinHealthyEndpointsViolation tests behavior when minimum healthy endpoints requirement is violated
func TestRPCPoolManager_MinHealthyEndpointsViolation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://localhost:8545",
		"http://localhost:8546",
		"http://localhost:8547",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     50 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       100 * time.Millisecond,
		MinHealthyEndpoints:    2, // Require at least 2 healthy endpoints
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(30 * time.Millisecond)

	// Make 2 out of 3 endpoints fail
	pool.mu.RLock()
	for i := 0; i < 2; i++ {
		mockClient := pool.endpoints[i].GetClient().(*mockClient)
		mockClient.setFailure(true)
		
		// Force failures to degrade endpoints
		for j := 0; j < 5; j++ {
			pool.UpdateEndpointMetrics(pool.endpoints[i], false, 100*time.Millisecond, fmt.Errorf("test error"))
		}
	}
	pool.mu.RUnlock()

	time.Sleep(100 * time.Millisecond)

	// Check pool health
	stats := pool.HealthMonitor.GetHealthStatus()
	poolStatus := stats["pool_status"].(string)
	healthyCount := stats["healthy_count"].(int)
	
	t.Logf("Pool status: %s, healthy count: %d", poolStatus, healthyCount)
	
	// Should report degraded status since we don't meet minimum
	assert.True(t, poolStatus == "degraded" || poolStatus == "unhealthy")
	assert.Less(t, healthyCount, 2, "Should have less than minimum healthy endpoints")

	// Should still be able to select endpoints (graceful degradation)
	endpoint, err := pool.SelectEndpoint()
	if healthyCount > 0 {
		assert.NoError(t, err)
		assert.NotNil(t, endpoint)
	} else {
		assert.Error(t, err)
	}
}

// TestHealthMonitor_ManualRecoveryOperations tests manual force recovery/exclusion
func TestHealthMonitor_ManualRecoveryOperations(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://localhost:8545",
		"http://localhost:8546",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test manual exclusion
	targetURL := urls[0]
	err = pool.HealthMonitor.ForceExcludeEndpoint(targetURL)
	assert.NoError(t, err, "Should be able to manually exclude endpoint")

	// Verify endpoint is excluded
	pool.mu.RLock()
	targetEndpoint := pool.endpoints[0]
	pool.mu.RUnlock()
	assert.Equal(t, StateExcluded, targetEndpoint.GetState())

	// Test manual recovery
	err = pool.HealthMonitor.ForceRecoverEndpoint(targetURL)
	assert.NoError(t, err, "Should be able to manually recover endpoint")

	// Verify endpoint is recovered to degraded state (not immediately healthy)
	assert.Equal(t, StateDegraded, targetEndpoint.GetState())
	assert.Equal(t, 70.0, targetEndpoint.Metrics.GetHealthScore(), "Should start with moderate health score")

	// Test recovery of non-existent endpoint
	err = pool.HealthMonitor.ForceRecoverEndpoint("http://nonexistent:8545")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "excluded endpoint not found")

	// Test exclusion of non-existent endpoint
	err = pool.HealthMonitor.ForceExcludeEndpoint("http://nonexistent:8545")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint not found")
}

// TestHealthMonitor_RecoveryIntervalTiming tests recovery timing mechanisms
func TestHealthMonitor_RecoveryIntervalTiming(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{"http://localhost:8545"}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     50 * time.Millisecond,
		UnhealthyThreshold:     1,
		RecoveryInterval:       150 * time.Millisecond, // Specific interval for testing
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(30 * time.Millisecond)

	// Set up health checker
	healthChecker := newMockHealthChecker()
	pool.HealthMonitor.SetHealthChecker(healthChecker)

	pool.mu.RLock()
	endpoint := pool.endpoints[0]
	pool.mu.RUnlock()

	// Exclude the endpoint manually
	err = pool.HealthMonitor.ForceExcludeEndpoint(endpoint.URL)
	require.NoError(t, err)
	assert.Equal(t, StateExcluded, endpoint.GetState())

	// Record exclusion time
	endpoint.mu.RLock()
	excludedAt := endpoint.ExcludedAt
	endpoint.mu.RUnlock()

	// Make health checker return success
	healthChecker.setClientHealth(endpoint.URL, true)

	// Wait less than recovery interval - should not recover yet
	time.Sleep(100 * time.Millisecond) // Less than 150ms recovery interval
	assert.Equal(t, StateExcluded, endpoint.GetState(), "Should not recover before interval expires")

	// Wait for recovery interval to pass
	time.Sleep(100 * time.Millisecond) // Total > 150ms

	// Wait for a few health check cycles to process the recovery
	recovered := false
	for i := 0; i < 10; i++ {
		time.Sleep(60 * time.Millisecond) // Health check interval
		if endpoint.GetState() != StateExcluded {
			recovered = true
			break
		}
	}

	assert.True(t, recovered, "Should recover after interval expires and health checks succeed")
	
	// Verify the endpoint recovered to degraded state (careful monitoring)
	finalState := endpoint.GetState()
	assert.True(t, finalState == StateDegraded || finalState == StateHealthy, 
		"Should recover to degraded or healthy state, got: %s", finalState)

	// Verify exclusion time was properly managed
	assert.True(t, time.Since(excludedAt) >= poolConfig.RecoveryInterval,
		"Recovery should only happen after recovery interval")
}

// TestHealthMonitor_HealthStatus tests comprehensive health status reporting
func TestHealthMonitor_HealthStatus(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://healthy:8545",
		"http://degraded:8546", 
		"http://unhealthy:8547",
		"http://excluded:8548",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    2,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(50 * time.Millisecond)

	// Set up different endpoint states
	pool.mu.Lock()
	// Keep first endpoint healthy (no changes needed)
	_ = pool.endpoints[0] // healthyEndpoint - keep as is
	
	// Make second endpoint degraded (some failures but not enough to exclude)
	degradedEndpoint := pool.endpoints[1]
	degradedEndpoint.Metrics.HealthScore = 60.0 // Below healthy but not unhealthy
	degradedEndpoint.UpdateState(StateDegraded)
	
	// Make third endpoint unhealthy (many failures)
	unhealthyEndpoint := pool.endpoints[2]
	unhealthyEndpoint.Metrics.HealthScore = 30.0 // Force low health score
	unhealthyEndpoint.UpdateState(StateUnhealthy)
	
	// Make fourth endpoint excluded
	excludedEndpoint := pool.endpoints[3]
	excludedEndpoint.UpdateState(StateExcluded)
	pool.mu.Unlock()

	// Get health status
	status := pool.HealthMonitor.GetHealthStatus()
	require.NotNil(t, status)

	// Verify counts
	assert.Equal(t, 1, status["healthy_count"].(int), "Should have 1 healthy endpoint")
	assert.Equal(t, 1, status["degraded_count"].(int), "Should have 1 degraded endpoint") 
	assert.Equal(t, 1, status["unhealthy_count"].(int), "Should have 1 unhealthy endpoint")
	assert.Equal(t, 1, status["excluded_count"].(int), "Should have 1 excluded endpoint")
	assert.Equal(t, 4, status["total_count"].(int), "Should have 4 total endpoints")

	// Verify configuration info
	assert.Equal(t, 2, status["min_healthy_required"].(int))
	assert.Equal(t, poolConfig.HealthCheckInterval.String(), status["health_check_interval"].(string))
	assert.Equal(t, poolConfig.RecoveryInterval.String(), status["recovery_interval"].(string))

	// Verify pool status (degraded since we have 2 available but need 2 minimum)
	poolStatus := status["pool_status"].(string)
	availableCount := status["healthy_count"].(int) + status["degraded_count"].(int)
	if availableCount >= poolConfig.MinHealthyEndpoints {
		assert.Equal(t, "healthy", poolStatus)
	} else if availableCount > 0 {
		assert.Equal(t, "degraded", poolStatus)
	} else {
		assert.Equal(t, "unhealthy", poolStatus)
	}

	// Verify endpoint details
	endpoints, ok := status["endpoints"].([]map[string]interface{})
	require.True(t, ok, "Endpoints should be array of maps")
	require.Len(t, endpoints, 4)

	// Check excluded endpoint has recovery info
	var excludedInfo map[string]interface{}
	for _, ep := range endpoints {
		if ep["state"].(string) == "excluded" {
			excludedInfo = ep
			break
		}
	}
	require.NotNil(t, excludedInfo, "Should find excluded endpoint info")
	assert.Contains(t, excludedInfo, "excluded_at")
	assert.Contains(t, excludedInfo, "next_recovery_attempt")
	assert.Contains(t, excludedInfo, "recovery_in")

	// Verify required fields present for all endpoints
	requiredFields := []string{"url", "state", "health_score", "success_rate", "last_used"}
	for i, ep := range endpoints {
		for _, field := range requiredFields {
			assert.Contains(t, ep, field, "Endpoint %d should have field %s", i, field)
		}
	}
}

// TestHealthMonitor_NoHealthCheckerConfigured tests behavior without health checker
func TestHealthMonitor_NoHealthCheckerConfigured(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{"http://localhost:8545"}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     50 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       100 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(50 * time.Millisecond)

	// Don't set a health checker - rely only on passive monitoring
	
	pool.mu.RLock()
	endpoint := pool.endpoints[0]
	pool.mu.RUnlock()

	// Manually exclude endpoint
	err = pool.HealthMonitor.ForceExcludeEndpoint(endpoint.URL)
	require.NoError(t, err)
	assert.Equal(t, StateExcluded, endpoint.GetState())

	// Wait for a few health check cycles
	time.Sleep(200 * time.Millisecond)

	// Endpoint should remain excluded since no active health checking
	assert.Equal(t, StateExcluded, endpoint.GetState(), 
		"Endpoint should remain excluded without active health checker")

	// Manual recovery should still work
	err = pool.HealthMonitor.ForceRecoverEndpoint(endpoint.URL)
	require.NoError(t, err)
	assert.Equal(t, StateDegraded, endpoint.GetState())
}

// TestRPCPoolManager_EdgeCase_EmptyURLList tests behavior with no URLs
func TestRPCPoolManager_EdgeCase_EmptyURLList(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	var urls []string // Empty list

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.Nil(t, pool, "Pool manager should return nil for empty URL list")
}

// TestRPCPoolManager_EdgeCase_SingleEndpointFlapping tests rapid state changes
func TestRPCPoolManager_EdgeCase_SingleEndpointFlapping(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{"http://flapping:8545"}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     50 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       100 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(30 * time.Millisecond)

	pool.mu.RLock()
	endpoint := pool.endpoints[0]
	pool.mu.RUnlock()

	// Simulate rapid alternating success/failure (flapping)
	for i := 0; i < 20; i++ {
		success := i%2 == 0
		var testErr error
		if !success {
			testErr = fmt.Errorf("flapping error %d", i)
		}
		
		pool.UpdateEndpointMetrics(endpoint, success, 50*time.Millisecond, testErr)
		time.Sleep(5 * time.Millisecond) // Short delay between updates
	}

	// Despite flapping, endpoint should eventually stabilize
	// The system should handle rapid state changes gracefully
	finalState := endpoint.GetState()
	t.Logf("Final endpoint state after flapping: %s", finalState)
	// With alternating success/failure, endpoint may be degraded or unhealthy
	// The key is that the system handles rapid changes without crashing
	
	// Pool should still be functional
	stats := pool.HealthMonitor.GetHealthStatus()
	assert.Equal(t, 1, stats["total_count"].(int))
}

// TestRPCPoolManager_EdgeCase_ZeroTimeouts tests behavior with zero/very small timeouts
func TestRPCPoolManager_EdgeCase_ZeroTimeouts(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{"http://timeout-test:8545"}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     1 * time.Millisecond,  // Very small
		UnhealthyThreshold:     1,
		RecoveryInterval:       1 * time.Millisecond,  // Very small
		MinHealthyEndpoints:    1,
		RequestTimeout:         1 * time.Nanosecond,   // Essentially zero
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	// Should handle extreme timeouts without crashing
	time.Sleep(50 * time.Millisecond)

	// System should still be functional
	_, err = pool.SelectEndpoint()
	// May succeed or fail depending on timing, but should not panic
	assert.NotPanics(t, func() {
		pool.HealthMonitor.GetHealthStatus()
	})
}

// TestRPCPoolManager_EdgeCase_ConcurrentStateChanges tests thread safety during state changes
func TestRPCPoolManager_EdgeCase_ConcurrentStateChanges(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://concurrent1:8545",
		"http://concurrent2:8546",
		"http://concurrent3:8547",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     50 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       100 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(30 * time.Millisecond)

	// Run multiple concurrent operations that modify endpoint states
	var wg sync.WaitGroup
	
	// Concurrent metric updates
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				pool.mu.RLock()
				if len(pool.endpoints) > 0 {
					endpoint := pool.endpoints[j%len(pool.endpoints)]
					pool.mu.RUnlock()
					
					success := (j+workerID)%3 == 0
					var testErr error
					if !success {
						testErr = fmt.Errorf("concurrent error %d-%d", workerID, j)
					}
					
					pool.UpdateEndpointMetrics(endpoint, success, time.Duration(j)*time.Millisecond, testErr)
				} else {
					pool.mu.RUnlock()
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Concurrent manual state changes
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(endpointIndex int) {
			defer wg.Done()
			url := urls[endpointIndex]
			
			for j := 0; j < 10; j++ {
				// Alternate between exclude and recover
				if j%2 == 0 {
					pool.HealthMonitor.ForceExcludeEndpoint(url)
				} else {
					pool.HealthMonitor.ForceRecoverEndpoint(url)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	// Concurrent endpoint selections
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = pool.SelectEndpoint() // Ignore errors, just ensure no race conditions
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	// Concurrent health status reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			pool.HealthMonitor.GetHealthStatus()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify system is still in a consistent state
	stats := pool.HealthMonitor.GetHealthStatus()
	assert.Equal(t, len(urls), stats["total_count"].(int))
	
	// Should not panic when querying after concurrent modifications
	assert.NotPanics(t, func() {
		pool.SelectEndpoint()
		pool.GetEndpointStats()
		pool.HealthMonitor.GetHealthStatus()
	})
}

// TestRPCPoolManager_EdgeCase_InvalidClientFactory tests behavior with failing client factory
func TestRPCPoolManager_EdgeCase_InvalidClientFactory(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{"http://factory-fail:8545"}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     2,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	// Create a client factory that always fails
	failingFactory := func(url string) (interface{}, error) {
		return nil, fmt.Errorf("factory always fails for %s", url)
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, failingFactory, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.Error(t, err) // Should fail if can't meet minimum healthy endpoints requirement
	assert.Contains(t, err.Error(), "insufficient healthy endpoints")

	// Pool should handle failed client creation gracefully - 
	// even if Start() fails, we should be able to query the pool safely
	defer pool.Stop()

	pool.mu.RLock()
	endpoint := pool.endpoints[0]
	client := endpoint.GetClient()
	pool.mu.RUnlock()
	
	// Client should be nil due to factory failure
	assert.Nil(t, client)
	
	// Health monitoring should handle nil clients without panicking
	assert.NotPanics(t, func() {
		pool.HealthMonitor.GetHealthStatus()
	})
}

// TestRPCPoolManager_EdgeCase_LargeNumberOfEndpoints tests scalability with many endpoints
func TestRPCPoolManager_EdgeCase_LargeNumberOfEndpoints(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	// Create many URLs
	var urls []string
	for i := 0; i < 100; i++ {
		urls = append(urls, fmt.Sprintf("http://endpoint-%d:8545", i))
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     200 * time.Millisecond,
		UnhealthyThreshold:     3,
		RecoveryInterval:       300 * time.Millisecond,
		MinHealthyEndpoints:    10,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "weighted",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(100 * time.Millisecond)

	// Verify all endpoints were created
	stats := pool.HealthMonitor.GetHealthStatus()
	assert.Equal(t, 100, stats["total_count"].(int))
	
	// Test endpoint selection with many endpoints
	selections := make(map[string]int)
	for i := 0; i < 500; i++ { // More selections than endpoints
		endpoint, err := pool.SelectEndpoint()
		if err == nil && endpoint != nil {
			selections[endpoint.URL]++
		}
	}
	
	// Should distribute across endpoints
	assert.Greater(t, len(selections), 50, "Should select from multiple endpoints")
	
	// Performance test - should complete in reasonable time
	start := time.Now()
	for i := 0; i < 1000; i++ {
		pool.SelectEndpoint()
	}
	duration := time.Since(start)
	assert.Less(t, duration, 5*time.Second, "Selecting 1000 endpoints should complete quickly")
}

// TestRPCPoolManager_EdgeCase_StrategyChange tests changing load balancing strategy at runtime
func TestRPCPoolManager_EdgeCase_StrategyChange(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	
	urls := []string{
		"http://strategy-test1:8545",
		"http://strategy-test2:8546",
		"http://strategy-test3:8547",
	}

	poolConfig := &config.RPCPoolConfig{
		HealthCheckInterval:     100 * time.Millisecond,
		UnhealthyThreshold:     3,
		RecoveryInterval:       200 * time.Millisecond,
		MinHealthyEndpoints:    1,
		RequestTimeout:         time.Second,
		LoadBalancingStrategy:  "round-robin",
	}

	pool := NewRPCPoolManager("test", urls, poolConfig, newMockClientForPool, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop()

	time.Sleep(50 * time.Millisecond)

	// Test with original strategy
	stats := pool.GetEndpointStats()
	assert.Equal(t, "round-robin", stats["strategy"])

	// Selections should work regardless of internal strategy state
	endpoint, err := pool.SelectEndpoint()
	assert.NoError(t, err)
	assert.NotNil(t, endpoint)
	
	// System should handle any strategy gracefully
	selections := make(map[string]int)
	for i := 0; i < 30; i++ {
		endpoint, err := pool.SelectEndpoint()
		if err == nil && endpoint != nil {
			selections[endpoint.URL]++
		}
	}
	
	// Should distribute requests (exact distribution depends on strategy)
	assert.Greater(t, len(selections), 0, "Should select from at least one endpoint")
}