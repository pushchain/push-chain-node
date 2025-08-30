package rpcpool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rollchains/pchain/universalClient/config"
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

	h.logger.Info().
		Dur("interval", h.config.HealthCheckInterval).
		Msg("starting health monitor")

	ticker := time.NewTicker(h.config.HealthCheckInterval)
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
	// Create timeout context for this specific health check
	checkCtx, cancel := context.WithTimeout(ctx, h.config.RequestTimeout)
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
	// Check if enough time has passed since exclusion for recovery attempt
	endpoint.mu.RLock()
	excludedAt := endpoint.ExcludedAt
	endpoint.mu.RUnlock()

	if time.Since(excludedAt) < h.config.RecoveryInterval {
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
func (h *HealthMonitor) GetHealthStatus() map[string]interface{} {
	endpoints := h.manager.GetEndpoints()
	
	status := make(map[string]interface{})
	
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0
	excludedCount := 0
	
	endpointStatus := make([]map[string]interface{}, len(endpoints))
	
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
		
		endpointStatus[i] = map[string]interface{}{
			"url":           endpoint.URL,
			"state":         state.String(),
			"health_score":  endpoint.Metrics.GetHealthScore(),
			"success_rate":  endpoint.Metrics.GetSuccessRate(),
			"last_used":     endpoint.LastUsed,
		}
		
		if state == StateExcluded {
			endpoint.mu.RLock()
			nextRecovery := endpoint.ExcludedAt.Add(h.config.RecoveryInterval)
			endpoint.mu.RUnlock()
			
			endpointStatus[i]["excluded_at"] = endpoint.ExcludedAt
			endpointStatus[i]["next_recovery_attempt"] = nextRecovery
			endpointStatus[i]["recovery_in"] = time.Until(nextRecovery).String()
		}
	}
	
	status["healthy_count"] = healthyCount
	status["degraded_count"] = degradedCount  
	status["unhealthy_count"] = unhealthyCount
	status["excluded_count"] = excludedCount
	status["total_count"] = len(endpoints)
	status["min_healthy_required"] = h.config.MinHealthyEndpoints
	status["health_check_interval"] = h.config.HealthCheckInterval.String()
	status["recovery_interval"] = h.config.RecoveryInterval.String()
	status["endpoints"] = endpointStatus
	
	// Overall pool health assessment
	availableCount := healthyCount + degradedCount
	if availableCount >= h.config.MinHealthyEndpoints {
		status["pool_status"] = "healthy"
	} else if availableCount > 0 {
		status["pool_status"] = "degraded"
	} else {
		status["pool_status"] = "unhealthy"
	}
	
	return status
}

// ForceExcludeEndpoint manually excludes an endpoint (useful for testing or manual intervention)
func (h *HealthMonitor) ForceExcludeEndpoint(url string) error {
	endpoints := h.manager.GetEndpoints()

	for _, endpoint := range endpoints {
		if endpoint.URL == url {
			endpoint.UpdateState(StateExcluded)
			h.logger.Info().
				Str("url", url).
				Msg("endpoint manually excluded")
			return nil
		}
	}
	
	return fmt.Errorf("endpoint not found: %s", url)
}

// ForceRecoverEndpoint manually recovers an excluded endpoint (useful for testing or manual intervention)
func (h *HealthMonitor) ForceRecoverEndpoint(url string) error {
	endpoints := h.manager.GetEndpoints()

	for _, endpoint := range endpoints {
		if endpoint.URL == url && endpoint.GetState() == StateExcluded {
			// Reset metrics and promote to degraded for monitoring
			endpoint.Metrics = &EndpointMetrics{HealthScore: 70.0}
			endpoint.UpdateState(StateDegraded)
			
			h.logger.Info().
				Str("url", url).
				Msg("endpoint manually recovered")
			return nil
		}
	}
	
	return fmt.Errorf("excluded endpoint not found: %s", url)
}