package rpcpool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rollchains/pchain/universalClient/config"
)

// Manager manages a pool of RPC endpoints with load balancing and health checking
type Manager struct {
	chainID        string
	endpoints      []*Endpoint
	selector       *EndpointSelector
	config         *config.RPCPoolConfig
	logger         zerolog.Logger
	HealthMonitor  *HealthMonitor // Exported for external access
	clientFactory  ClientFactory  // Function to create client for URL
	stopCh         chan struct{}
	wg             sync.WaitGroup
	mu             sync.RWMutex
}

// NewManager creates a new RPC pool manager
func NewManager(
	chainID string,
	urls []string,
	poolConfig *config.RPCPoolConfig,
	clientFactory ClientFactory,
	logger zerolog.Logger,
) *Manager {
	if len(urls) == 0 {
		logger.Warn().Str("chain_id", chainID).Msg("no RPC URLs provided for pool")
		return nil
	}

	// Create endpoints
	endpoints := make([]*Endpoint, len(urls))
	for i, url := range urls {
		endpoints[i] = NewEndpoint(url)
	}

	strategy := LoadBalancingStrategy(poolConfig.LoadBalancingStrategy)
	selector := NewEndpointSelector(strategy)

	manager := &Manager{
		chainID:       chainID,
		endpoints:     endpoints,
		selector:      selector,
		config:        poolConfig,
		logger:        logger.With().Str("component", "rpc_pool").Str("chain_id", chainID).Logger(),
		clientFactory: clientFactory,
		stopCh:        make(chan struct{}),
	}

	// Create health monitor
	manager.HealthMonitor = NewHealthMonitor(manager, poolConfig, logger)

	return manager
}

// Start initializes all endpoints and starts health monitoring
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info().
		Int("endpoint_count", len(m.endpoints)).
		Str("strategy", string(m.selector.GetStrategy())).
		Msg("starting RPC pool manager")

	// Initialize all endpoints
	var initErrors []error
	for _, endpoint := range m.endpoints {
		if err := m.initializeEndpoint(ctx, endpoint); err != nil {
			m.logger.Warn().
				Str("url", endpoint.URL).
				Err(err).
				Msg("failed to initialize endpoint")
			endpoint.UpdateState(StateUnhealthy)
			initErrors = append(initErrors, err)
		}
	}

	// Check if we have enough healthy endpoints
	healthyCount := m.getHealthyEndpointCount()
	if healthyCount < m.config.MinHealthyEndpoints {
		return fmt.Errorf("insufficient healthy endpoints: %d/%d (minimum: %d)", 
			healthyCount, len(m.endpoints), m.config.MinHealthyEndpoints)
	}

	// Start health monitoring
	m.wg.Add(1)
	go m.HealthMonitor.Start(ctx, &m.wg)

	m.logger.Info().
		Int("healthy_endpoints", healthyCount).
		Int("total_endpoints", len(m.endpoints)).
		Msg("RPC pool manager started")

	return nil
}

// Stop stops the pool manager and health monitoring
func (m *Manager) Stop() {
	m.logger.Info().Msg("stopping RPC pool manager")
	
	// Stop the health monitor first
	if m.HealthMonitor != nil {
		m.HealthMonitor.Stop()
	}
	
	close(m.stopCh)
	m.wg.Wait()
	
	// Close all client connections
	for _, endpoint := range m.endpoints {
		if client := endpoint.GetClient(); client != nil {
			if err := client.Close(); err != nil {
				m.logger.Warn().
					Str("url", endpoint.URL).
					Err(err).
					Msg("failed to close client connection")
			}
		}
	}
	
	m.logger.Info().Msg("RPC pool manager stopped")
}

// initializeEndpoint creates and initializes the client for an endpoint
func (m *Manager) initializeEndpoint(ctx context.Context, endpoint *Endpoint) error {
	client, err := m.clientFactory(endpoint.URL)
	if err != nil {
		return fmt.Errorf("failed to create client for %s: %w", endpoint.URL, err)
	}

	endpoint.SetClient(client)
	endpoint.UpdateState(StateHealthy)
	
	m.logger.Info().
		Str("url", endpoint.URL).
		Msg("endpoint initialized successfully")
	
	return nil
}

// SelectEndpoint selects an available endpoint based on the configured strategy
func (m *Manager) SelectEndpoint() (*Endpoint, error) {
	healthyEndpoints := m.getHealthyEndpoints()
	
	if len(healthyEndpoints) == 0 {
		return nil, fmt.Errorf("no healthy endpoints available")
	}

	selected := m.selector.SelectEndpoint(healthyEndpoints)
	if selected == nil {
		return nil, fmt.Errorf("failed to select endpoint")
	}

	// Update last used time
	selected.mu.Lock()
	selected.LastUsed = time.Now()
	selected.mu.Unlock()

	return selected, nil
}

// getHealthyEndpoints returns all endpoints that can serve requests
func (m *Manager) getHealthyEndpoints() []*Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	healthy := make([]*Endpoint, 0, len(m.endpoints))
	for _, endpoint := range m.endpoints {
		if endpoint.IsHealthy() {
			healthy = append(healthy, endpoint)
		}
	}
	return healthy
}

// GetHealthyEndpointCount returns the count of healthy endpoints
func (m *Manager) GetHealthyEndpointCount() int {
	return len(m.getHealthyEndpoints())
}

// getHealthyEndpointCount returns the count of healthy endpoints (deprecated: use GetHealthyEndpointCount)
func (m *Manager) getHealthyEndpointCount() int {
	return m.GetHealthyEndpointCount()
}

// UpdateEndpointMetrics updates metrics for an endpoint after a request
func (m *Manager) UpdateEndpointMetrics(endpoint *Endpoint, success bool, latency time.Duration, err error) {
	if success {
		endpoint.Metrics.UpdateSuccess(latency)
		
		// Potentially upgrade state if it was degraded
		if endpoint.GetState() == StateDegraded {
			// If we have a good success rate now, upgrade to healthy
			if endpoint.Metrics.GetSuccessRate() > 0.8 {
				endpoint.UpdateState(StateHealthy)
				m.logger.Info().
					Str("url", endpoint.URL).
					Float64("success_rate", endpoint.Metrics.GetSuccessRate()).
					Msg("endpoint promoted to healthy")
			}
		}
	} else {
		endpoint.Metrics.UpdateFailure(err, latency)
		
		// Check if we should downgrade the endpoint state
		consecutiveFailures := endpoint.Metrics.GetConsecutiveFailures()
		
		if consecutiveFailures >= m.config.UnhealthyThreshold {
			// Mark as excluded
			endpoint.UpdateState(StateExcluded)
			m.logger.Warn().
				Str("url", endpoint.URL).
				Int("consecutive_failures", consecutiveFailures).
				Err(err).
				Msg("endpoint excluded due to consecutive failures")
		} else if endpoint.Metrics.GetSuccessRate() < 0.5 && endpoint.GetState() == StateHealthy {
			// Downgrade to degraded
			endpoint.UpdateState(StateDegraded)
			m.logger.Warn().
				Str("url", endpoint.URL).
				Float64("success_rate", endpoint.Metrics.GetSuccessRate()).
				Msg("endpoint downgraded to degraded")
		}
	}
}

// GetEndpointStats returns statistics about all endpoints
func (m *Manager) GetEndpointStats() *EndpointStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	endpoints := make([]EndpointInfo, len(m.endpoints))
	
	for i, endpoint := range m.endpoints {
		endpoints[i] = EndpointInfo{
			URL:            endpoint.URL,
			State:          endpoint.GetState().String(),
			HealthScore:    endpoint.Metrics.GetHealthScore(),
			LastUsed:       endpoint.LastUsed,
			RequestCount:   endpoint.Metrics.TotalRequests,
			FailureCount:   endpoint.Metrics.FailedRequests,
			TotalLatency:   endpoint.Metrics.AverageLatency.Milliseconds() * int64(endpoint.Metrics.TotalRequests),
			AverageLatency: float64(endpoint.Metrics.AverageLatency.Milliseconds()),
		}
	}
	
	return &EndpointStats{
		ChainID:        m.chainID,
		TotalEndpoints: len(m.endpoints),
		Strategy:       string(m.selector.GetStrategy()),
		Endpoints:      endpoints,
	}
}

// GetEndpoints returns all endpoints (for health monitor access)
func (m *Manager) GetEndpoints() []*Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Return a copy to prevent external modification
	endpoints := make([]*Endpoint, len(m.endpoints))
	copy(endpoints, m.endpoints)
	return endpoints
}

// GetConfig returns the pool configuration (for health monitor access)
func (m *Manager) GetConfig() *config.RPCPoolConfig {
	return m.config
}