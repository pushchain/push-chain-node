package common

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rollchains/pchain/universalClient/config"
)

// EndpointState represents the current state of an RPC endpoint
type EndpointState int

const (
	StateHealthy EndpointState = iota
	StateDegraded
	StateUnhealthy
	StateExcluded
)

func (s EndpointState) String() string {
	switch s {
	case StateHealthy:
		return "healthy"
	case StateDegraded:
		return "degraded"
	case StateUnhealthy:
		return "unhealthy"
	case StateExcluded:
		return "excluded"
	default:
		return "unknown"
	}
}

// LoadBalancingStrategy defines how requests are distributed across endpoints
type LoadBalancingStrategy string

const (
	StrategyRoundRobin LoadBalancingStrategy = "round-robin"
	StrategyWeighted   LoadBalancingStrategy = "weighted"
)

// EndpointMetrics tracks performance and health metrics for an endpoint
type EndpointMetrics struct {
	mu                  sync.RWMutex
	TotalRequests      uint64
	SuccessfulRequests uint64
	FailedRequests     uint64
	AverageLatency     time.Duration
	ConsecutiveFailures int
	LastSuccessTime    time.Time
	LastErrorTime      time.Time
	LastError          error
	HealthScore        float64 // 0-100, calculated from success rate and latency
}

// UpdateSuccess updates metrics for a successful request
func (m *EndpointMetrics) UpdateSuccess(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalRequests++
	m.SuccessfulRequests++
	m.ConsecutiveFailures = 0
	m.LastSuccessTime = time.Now()

	// Update rolling average latency
	if m.AverageLatency == 0 {
		m.AverageLatency = latency
	} else {
		// Exponential moving average with alpha = 0.1
		m.AverageLatency = time.Duration(float64(m.AverageLatency)*0.9 + float64(latency)*0.1)
	}

	m.calculateHealthScore()
}

// UpdateFailure updates metrics for a failed request
func (m *EndpointMetrics) UpdateFailure(err error, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalRequests++
	m.FailedRequests++
	m.ConsecutiveFailures++
	m.LastErrorTime = time.Now()
	m.LastError = err

	// Update latency even for failures (for timeout tracking)
	if latency > 0 && m.AverageLatency > 0 {
		m.AverageLatency = time.Duration(float64(m.AverageLatency)*0.9 + float64(latency)*0.1)
	}

	m.calculateHealthScore()
}

// calculateHealthScore computes health score based on success rate and latency
func (m *EndpointMetrics) calculateHealthScore() {
	if m.TotalRequests == 0 {
		m.HealthScore = 100.0
		return
	}

	// Base score from success rate (0-100)
	successRate := float64(m.SuccessfulRequests) / float64(m.TotalRequests)
	baseScore := successRate * 100.0

	// Latency penalty: reduce score based on high latency
	// Assume 1 second is baseline, penalize above that
	latencyPenalty := 0.0
	if m.AverageLatency > time.Second {
		// Each additional second reduces score by up to 20 points
		extraSeconds := m.AverageLatency.Seconds() - 1.0
		latencyPenalty = extraSeconds * 5.0 // 5 points per second
		if latencyPenalty > 20.0 {
			latencyPenalty = 20.0
		}
	}

	// Consecutive failure penalty
	failurePenalty := float64(m.ConsecutiveFailures) * 10.0 // 10 points per consecutive failure
	if failurePenalty > 50.0 {
		failurePenalty = 50.0
	}

	m.HealthScore = baseScore - latencyPenalty - failurePenalty
	if m.HealthScore < 0 {
		m.HealthScore = 0
	}
}

// GetHealthScore returns the current health score (thread-safe)
func (m *EndpointMetrics) GetHealthScore() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.HealthScore
}

// GetSuccessRate returns the success rate (thread-safe)
func (m *EndpointMetrics) GetSuccessRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.TotalRequests == 0 {
		return 1.0
	}
	return float64(m.SuccessfulRequests) / float64(m.TotalRequests)
}

// GetConsecutiveFailures returns consecutive failure count (thread-safe)
func (m *EndpointMetrics) GetConsecutiveFailures() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ConsecutiveFailures
}

// RPCEndpoint represents a single RPC endpoint with its client and metrics
type RPCEndpoint struct {
	URL         string
	Client      interface{} // Can be *ethclient.Client or *rpc.Client (Solana)
	State       EndpointState
	Metrics     *EndpointMetrics
	LastUsed    time.Time
	ExcludedAt  time.Time // When this endpoint was excluded
	mu          sync.RWMutex
}

// NewRPCEndpoint creates a new RPC endpoint
func NewRPCEndpoint(url string) *RPCEndpoint {
	return &RPCEndpoint{
		URL:     url,
		State:   StateHealthy,
		Metrics: &EndpointMetrics{HealthScore: 100.0},
	}
}

// SetClient sets the RPC client for this endpoint
func (e *RPCEndpoint) SetClient(client interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Client = client
}

// GetClient returns the RPC client (thread-safe)
func (e *RPCEndpoint) GetClient() interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Client
}

// UpdateState updates the endpoint state (thread-safe)
func (e *RPCEndpoint) UpdateState(state EndpointState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if state == StateExcluded && e.State != StateExcluded {
		e.ExcludedAt = time.Now()
	}
	
	e.State = state
}

// GetState returns the current state (thread-safe)
func (e *RPCEndpoint) GetState() EndpointState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.State
}

// IsHealthy returns true if endpoint is in a usable state
func (e *RPCEndpoint) IsHealthy() bool {
	state := e.GetState()
	return state == StateHealthy || state == StateDegraded
}

// RPCPoolManager manages a pool of RPC endpoints with load balancing and health checking
type RPCPoolManager struct {
	chainID        string
	endpoints      []*RPCEndpoint
	strategy       LoadBalancingStrategy
	config         *config.RPCPoolConfig
	logger         zerolog.Logger
	currentIndex   atomic.Uint32
	HealthMonitor  *HealthMonitor // Exported for external access
	createClientFn func(string) (interface{}, error) // Function to create client for URL
	stopCh         chan struct{}
	wg             sync.WaitGroup
	mu             sync.RWMutex
}

// NewRPCPoolManager creates a new RPC pool manager
func NewRPCPoolManager(
	chainID string,
	urls []string,
	poolConfig *config.RPCPoolConfig,
	createClientFn func(string) (interface{}, error),
	logger zerolog.Logger,
) *RPCPoolManager {
	if len(urls) == 0 {
		logger.Warn().Str("chain_id", chainID).Msg("no RPC URLs provided for pool")
		return nil
	}

	// Create endpoints
	endpoints := make([]*RPCEndpoint, len(urls))
	for i, url := range urls {
		endpoints[i] = NewRPCEndpoint(url)
	}

	strategy := LoadBalancingStrategy(poolConfig.LoadBalancingStrategy)
	if strategy != StrategyRoundRobin && strategy != StrategyWeighted {
		strategy = StrategyRoundRobin
	}

	pool := &RPCPoolManager{
		chainID:        chainID,
		endpoints:      endpoints,
		strategy:       strategy,
		config:         poolConfig,
		logger:         logger.With().Str("component", "rpc_pool").Str("chain_id", chainID).Logger(),
		createClientFn: createClientFn,
		stopCh:         make(chan struct{}),
	}

	// Create health monitor
	pool.HealthMonitor = NewHealthMonitor(pool, poolConfig, logger)

	return pool
}

// Start initializes all endpoints and starts health monitoring
func (p *RPCPoolManager) Start(ctx context.Context) error {
	p.logger.Info().
		Int("endpoint_count", len(p.endpoints)).
		Str("strategy", string(p.strategy)).
		Msg("starting RPC pool manager")

	// Initialize all endpoints
	var initErrors []error
	for _, endpoint := range p.endpoints {
		if err := p.initializeEndpoint(ctx, endpoint); err != nil {
			p.logger.Warn().
				Str("url", endpoint.URL).
				Err(err).
				Msg("failed to initialize endpoint")
			endpoint.UpdateState(StateUnhealthy)
			initErrors = append(initErrors, err)
		}
	}

	// Check if we have enough healthy endpoints
	healthyCount := p.getHealthyEndpointCount()
	if healthyCount < p.config.MinHealthyEndpoints {
		return fmt.Errorf("insufficient healthy endpoints: %d/%d (minimum: %d)", 
			healthyCount, len(p.endpoints), p.config.MinHealthyEndpoints)
	}

	// Start health monitoring
	p.wg.Add(1)
	go p.HealthMonitor.Start(ctx, &p.wg)

	p.logger.Info().
		Int("healthy_endpoints", healthyCount).
		Int("total_endpoints", len(p.endpoints)).
		Msg("RPC pool manager started")

	return nil
}

// Stop stops the pool manager and health monitoring
func (p *RPCPoolManager) Stop() {
	p.logger.Info().Msg("stopping RPC pool manager")
	
	// Stop the health monitor first
	if p.HealthMonitor != nil {
		p.HealthMonitor.Stop()
	}
	
	close(p.stopCh)
	p.wg.Wait()
	
	// Close all client connections
	for range p.endpoints {
		// Note: We can't close connections generically here since different
		// client types have different close methods. The specific implementations
		// (EVM/SVM clients) should handle this.
	}
	
	p.logger.Info().Msg("RPC pool manager stopped")
}

// initializeEndpoint creates and initializes the client for an endpoint
func (p *RPCPoolManager) initializeEndpoint(ctx context.Context, endpoint *RPCEndpoint) error {
	client, err := p.createClientFn(endpoint.URL)
	if err != nil {
		return fmt.Errorf("failed to create client for %s: %w", endpoint.URL, err)
	}

	endpoint.SetClient(client)
	endpoint.UpdateState(StateHealthy)
	
	p.logger.Info().
		Str("url", endpoint.URL).
		Msg("endpoint initialized successfully")
	
	return nil
}

// SelectEndpoint selects an available endpoint based on the configured strategy
func (p *RPCPoolManager) SelectEndpoint() (*RPCEndpoint, error) {
	healthyEndpoints := p.getHealthyEndpoints()
	
	if len(healthyEndpoints) == 0 {
		return nil, fmt.Errorf("no healthy endpoints available")
	}

	var selected *RPCEndpoint
	
	switch p.strategy {
	case StrategyWeighted:
		selected = p.selectWeighted(healthyEndpoints)
	case StrategyRoundRobin:
		fallthrough
	default:
		selected = p.selectRoundRobin(healthyEndpoints)
	}

	// Update last used time
	selected.mu.Lock()
	selected.LastUsed = time.Now()
	selected.mu.Unlock()

	return selected, nil
}

// selectRoundRobin implements round-robin selection
func (p *RPCPoolManager) selectRoundRobin(endpoints []*RPCEndpoint) *RPCEndpoint {
	if len(endpoints) == 1 {
		return endpoints[0]
	}
	
	index := p.currentIndex.Add(1) % uint32(len(endpoints))
	return endpoints[index]
}

// selectWeighted implements weighted selection based on health scores
func (p *RPCPoolManager) selectWeighted(endpoints []*RPCEndpoint) *RPCEndpoint {
	if len(endpoints) == 1 {
		return endpoints[0]
	}

	// Calculate total weight (sum of health scores)
	totalWeight := 0.0
	for _, endpoint := range endpoints {
		totalWeight += endpoint.Metrics.GetHealthScore()
	}

	if totalWeight == 0 {
		// If all endpoints have zero health score, fall back to round-robin
		return p.selectRoundRobin(endpoints)
	}

	// Generate random number between 0 and totalWeight
	target := rand.Float64() * totalWeight
	
	// Select endpoint based on weight
	currentWeight := 0.0
	for _, endpoint := range endpoints {
		currentWeight += endpoint.Metrics.GetHealthScore()
		if currentWeight >= target {
			return endpoint
		}
	}

	// Fallback to last endpoint (shouldn't happen)
	return endpoints[len(endpoints)-1]
}

// getHealthyEndpoints returns all endpoints that can serve requests
func (p *RPCPoolManager) getHealthyEndpoints() []*RPCEndpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	healthy := make([]*RPCEndpoint, 0, len(p.endpoints))
	for _, endpoint := range p.endpoints {
		if endpoint.IsHealthy() {
			healthy = append(healthy, endpoint)
		}
	}
	return healthy
}

// GetHealthyEndpointCount returns the count of healthy endpoints
func (p *RPCPoolManager) GetHealthyEndpointCount() int {
	return len(p.getHealthyEndpoints())
}

// getHealthyEndpointCount returns the count of healthy endpoints (deprecated: use GetHealthyEndpointCount)
func (p *RPCPoolManager) getHealthyEndpointCount() int {
	return p.GetHealthyEndpointCount()
}

// UpdateEndpointMetrics updates metrics for an endpoint after a request
func (p *RPCPoolManager) UpdateEndpointMetrics(endpoint *RPCEndpoint, success bool, latency time.Duration, err error) {
	if success {
		endpoint.Metrics.UpdateSuccess(latency)
		
		// Potentially upgrade state if it was degraded
		if endpoint.GetState() == StateDegraded {
			// If we have a good success rate now, upgrade to healthy
			if endpoint.Metrics.GetSuccessRate() > 0.8 {
				endpoint.UpdateState(StateHealthy)
				p.logger.Info().
					Str("url", endpoint.URL).
					Float64("success_rate", endpoint.Metrics.GetSuccessRate()).
					Msg("endpoint promoted to healthy")
			}
		}
	} else {
		endpoint.Metrics.UpdateFailure(err, latency)
		
		// Check if we should downgrade the endpoint state
		consecutiveFailures := endpoint.Metrics.GetConsecutiveFailures()
		
		if consecutiveFailures >= p.config.UnhealthyThreshold {
			// Mark as excluded
			endpoint.UpdateState(StateExcluded)
			p.logger.Warn().
				Str("url", endpoint.URL).
				Int("consecutive_failures", consecutiveFailures).
				Err(err).
				Msg("endpoint excluded due to consecutive failures")
		} else if endpoint.Metrics.GetSuccessRate() < 0.5 && endpoint.GetState() == StateHealthy {
			// Downgrade to degraded
			endpoint.UpdateState(StateDegraded)
			p.logger.Warn().
				Str("url", endpoint.URL).
				Float64("success_rate", endpoint.Metrics.GetSuccessRate()).
				Msg("endpoint downgraded to degraded")
		}
	}
}

// GetEndpointStats returns statistics about all endpoints
func (p *RPCPoolManager) GetEndpointStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	stats := make(map[string]interface{})
	endpoints := make([]map[string]interface{}, len(p.endpoints))
	
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0
	excludedCount := 0
	
	for i, endpoint := range p.endpoints {
		state := endpoint.GetState()
		switch state {
		case StateHealthy:
			healthyCount++
		case StateDegraded:
			degradedCount++
		case StateUnhealthy:
			unhealthyCount++
		case StateExcluded:
			excludedCount++
		}
		
		endpoints[i] = map[string]interface{}{
			"url":                 endpoint.URL,
			"state":               state.String(),
			"health_score":        endpoint.Metrics.GetHealthScore(),
			"total_requests":      endpoint.Metrics.TotalRequests,
			"successful_requests": endpoint.Metrics.SuccessfulRequests,
			"failed_requests":     endpoint.Metrics.FailedRequests,
			"success_rate":        endpoint.Metrics.GetSuccessRate(),
			"consecutive_failures": endpoint.Metrics.GetConsecutiveFailures(),
			"average_latency":     endpoint.Metrics.AverageLatency.String(),
			"last_used":          endpoint.LastUsed,
		}
	}
	
	stats["strategy"] = string(p.strategy)
	stats["total_endpoints"] = len(p.endpoints)
	stats["healthy_endpoints"] = healthyCount
	stats["degraded_endpoints"] = degradedCount
	stats["unhealthy_endpoints"] = unhealthyCount
	stats["excluded_endpoints"] = excludedCount
	stats["endpoints"] = endpoints
	
	return stats
}