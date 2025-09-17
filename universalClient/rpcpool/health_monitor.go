package rpcpool

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/pushchain/push-chain-node/universalClient/config"
)

// HealthMonitor monitors the health of RPC endpoints and manages recovery
type HealthMonitor struct {
	manager       *Manager
	config        *config.RPCPoolConfig
	logger        zerolog.Logger
	healthChecker HealthChecker
	stopCh        chan struct{}
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(manager *Manager, config *config.RPCPoolConfig, logger zerolog.Logger) *HealthMonitor {
	return &HealthMonitor{
		manager: manager,
		config:  config,
		logger:  logger.With().Str("component", "health_monitor").Logger(),
		stopCh:  make(chan struct{}),
	}
}

// SetHealthChecker sets the health checker implementation
func (h *HealthMonitor) SetHealthChecker(checker HealthChecker) {
	h.healthChecker = checker
}

// Start begins the health monitoring loop
func (h *HealthMonitor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	// Default to 30 seconds if not configured
	intervalSeconds := h.config.HealthCheckIntervalSeconds
	if intervalSeconds <= 0 {
		intervalSeconds = 30
	}

	h.logger.Info().
		Str("interval", (time.Duration(intervalSeconds) * time.Second).String()).
		Msg("starting health monitor")

	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	// Immediate health check
	h.performHealthChecks(ctx)

	for {
		select {
		case <-ctx.Done():
			h.logger.Info().Msg("health monitor stopping: context cancelled")
			return
		case <-h.stopCh:
			h.logger.Info().Msg("health monitor stopping: stop signal received")
			return
		case <-ticker.C:
			h.performHealthChecks(ctx)
		}
	}
}

// Stop stops the health monitor
func (h *HealthMonitor) Stop() {
	close(h.stopCh)
}

// performHealthChecks checks the health of all endpoints
func (h *HealthMonitor) performHealthChecks(ctx context.Context) {
	h.logger.Debug().Msg("performing health checks on all endpoints")

	endpoints := h.manager.GetEndpoints()

	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		wg.Add(1)
		go func(ep *Endpoint) {
			defer wg.Done()
			h.checkEndpointHealth(ctx, ep)
		}(endpoint)
	}

	wg.Wait()
	h.logger.Debug().Msg("health checks completed")
}

// checkEndpointHealth performs a health check on a single endpoint
func (h *HealthMonitor) checkEndpointHealth(ctx context.Context, endpoint *Endpoint) {
	// Default to 10 seconds if not configured
	timeoutSeconds := h.config.RequestTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	
	// Create timeout context for this specific health check
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	client := endpoint.GetClient()
	if client == nil {
		h.logger.Debug().
			Str("url", endpoint.URL).
			Msg("endpoint has no client, skipping health check")
		return
	}

	start := time.Now()
	var err error

	// Use custom health checker if available, otherwise skip active health checking
	if h.healthChecker != nil {
		err = h.healthChecker.CheckHealth(checkCtx, client)
	} else {
		// No active health checking - rely on passive monitoring only
		h.logger.Debug().
			Str("url", endpoint.URL).
			Msg("no health checker configured, skipping active health check")
		return
	}

	latency := time.Since(start)

	// Handle excluded endpoints trying to recover
	if endpoint.GetState() == StateExcluded {
		h.handleExcludedEndpointCheck(endpoint, err == nil, latency, err)
		return
	}

	// Update metrics based on health check result
	success := err == nil
	h.manager.UpdateEndpointMetrics(endpoint, success, latency, err)

	if success {
		h.logger.Debug().
			Str("url", endpoint.URL).
			Dur("latency", latency).
			Float64("health_score", endpoint.Metrics.GetHealthScore()).
			Msg("endpoint health check passed")
	} else {
		h.logger.Warn().
			Str("url", endpoint.URL).
			Dur("latency", latency).
			Err(err).
			Int("consecutive_failures", endpoint.Metrics.GetConsecutiveFailures()).
			Msg("endpoint health check failed")
	}
}

// handleExcludedEndpointCheck handles health checking for excluded endpoints
func (h *HealthMonitor) handleExcludedEndpointCheck(endpoint *Endpoint, success bool, latency time.Duration, err error) {
	// Default to 5 minutes if not configured
	recoverySeconds := h.config.RecoveryIntervalSeconds
	if recoverySeconds <= 0 {
		recoverySeconds = 300
	}

	// Check if enough time has passed since exclusion for recovery attempt
	endpoint.mu.RLock()
	excludedAt := endpoint.ExcludedAt
	endpoint.mu.RUnlock()

	if time.Since(excludedAt) < time.Duration(recoverySeconds)*time.Second {
		// Not enough time has passed, skip recovery attempt
		return
	}

	h.logger.Info().
		Str("url", endpoint.URL).
		Dur("excluded_duration", time.Since(excludedAt)).
		Bool("success", success).
		Msg("attempting endpoint recovery")

	if success {
		// Recovery successful - reset metrics and promote to degraded state
		// Start with degraded instead of healthy to monitor closely
		endpoint.Metrics = &EndpointMetrics{HealthScore: 70.0} // Start with moderate score
		endpoint.UpdateState(StateDegraded)
		
		h.logger.Info().
			Str("url", endpoint.URL).
			Dur("recovery_latency", latency).
			Msg("endpoint successfully recovered, promoted to degraded state")
	} else {
		// Recovery failed - update exclusion time to wait another recovery interval
		endpoint.mu.Lock()
		endpoint.ExcludedAt = time.Now()
		endpoint.mu.Unlock()
		
		h.logger.Warn().
			Str("url", endpoint.URL).
			Err(err).
			Msg("endpoint recovery failed, extending exclusion period")
	}
}

// GetHealthStatus returns a summary of endpoint health
func (h *HealthMonitor) GetHealthStatus() *HealthStatus {
	endpoints := h.manager.GetEndpoints()
	
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0
	excludedCount := 0
	
	endpointStatuses := make([]EndpointStatus, len(endpoints))
	
	for i, endpoint := range endpoints {
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
		
		var lastError string
		if endpoint.Metrics.LastError != nil {
			lastError = endpoint.Metrics.LastError.Error()
		}
		
		endpointStatuses[i] = EndpointStatus{
			URL:          endpoint.URL,
			State:        state.String(),
			HealthScore:  endpoint.Metrics.GetHealthScore(),
			ResponseTime: endpoint.Metrics.AverageLatency.Milliseconds(),
			LastChecked:  endpoint.LastUsed,
			LastError:    lastError,
		}
	}
	
	return &HealthStatus{
		ChainID:        h.manager.chainID,
		TotalEndpoints: len(endpoints),
		HealthyCount:   healthyCount,
		UnhealthyCount: unhealthyCount,
		DegradedCount:  degradedCount,
		ExcludedCount:  excludedCount,
		Strategy:       string(h.manager.selector.GetStrategy()),
		Endpoints:      endpointStatuses,
	}
}

