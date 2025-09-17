package rpcpool

import (
	"sync"
	"time"
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

// Endpoint represents a single RPC endpoint with its client and metrics
type Endpoint struct {
	URL         string
	Client      Client // Generic client interface
	State       EndpointState
	Metrics     *EndpointMetrics
	LastUsed    time.Time
	ExcludedAt  time.Time // When this endpoint was excluded
	mu          sync.RWMutex
}

// NewEndpoint creates a new RPC endpoint
func NewEndpoint(url string) *Endpoint {
	return &Endpoint{
		URL:     url,
		State:   StateHealthy,
		Metrics: &EndpointMetrics{HealthScore: 100.0},
	}
}

// SetClient sets the RPC client for this endpoint
func (e *Endpoint) SetClient(client Client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Client = client
}

// GetClient returns the RPC client (thread-safe)
func (e *Endpoint) GetClient() Client {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Client
}

// UpdateState updates the endpoint state (thread-safe)
func (e *Endpoint) UpdateState(state EndpointState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if state == StateExcluded && e.State != StateExcluded {
		e.ExcludedAt = time.Now()
	}
	
	e.State = state
}

// GetState returns the current state (thread-safe)
func (e *Endpoint) GetState() EndpointState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.State
}

// IsHealthy returns true if endpoint is in a usable state
func (e *Endpoint) IsHealthy() bool {
	state := e.GetState()
	return state == StateHealthy || state == StateDegraded
}