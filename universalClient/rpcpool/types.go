package rpcpool

import "time"

// HealthStatus represents the health status of the RPC pool
type HealthStatus struct {
	ChainID         string           `json:"chain_id"`
	TotalEndpoints  int              `json:"total_endpoints"`
	HealthyCount    int              `json:"healthy_count"`
	UnhealthyCount  int              `json:"unhealthy_count"`
	DegradedCount   int              `json:"degraded_count"`
	ExcludedCount   int              `json:"excluded_count"`
	Strategy        string           `json:"strategy"`
	Endpoints       []EndpointStatus `json:"endpoints"`
}

// EndpointStatus represents the status of a single endpoint
type EndpointStatus struct {
	URL          string    `json:"url"`
	State        string    `json:"state"`
	HealthScore  float64   `json:"health_score"`
	ResponseTime int64     `json:"response_time_ms"`
	LastChecked  time.Time `json:"last_checked"`
	LastError    string    `json:"last_error,omitempty"`
}

// EndpointStats represents statistics for endpoints
type EndpointStats struct {
	ChainID        string         `json:"chain_id"`
	TotalEndpoints int            `json:"total_endpoints"`
	Strategy       string         `json:"strategy"`
	Endpoints      []EndpointInfo `json:"endpoints"`
}

// EndpointInfo represents information about a single endpoint
type EndpointInfo struct {
	URL            string    `json:"url"`
	State          string    `json:"state"`
	HealthScore    float64   `json:"health_score"`
	LastUsed       time.Time `json:"last_used"`
	RequestCount   uint64    `json:"request_count"`
	FailureCount   uint64    `json:"failure_count"`
	TotalLatency   int64     `json:"total_latency_ms"`
	AverageLatency float64   `json:"average_latency_ms"`
}