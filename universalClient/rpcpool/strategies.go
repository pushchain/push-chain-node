package rpcpool

import (
	"math/rand"
	"sync/atomic"
)

// LoadBalancingStrategy defines how requests are distributed across endpoints
type LoadBalancingStrategy string

const (
	StrategyRoundRobin LoadBalancingStrategy = "round-robin"
	StrategyWeighted   LoadBalancingStrategy = "weighted"
)

// EndpointSelector handles endpoint selection based on different strategies
type EndpointSelector struct {
	strategy     LoadBalancingStrategy
	currentIndex atomic.Uint32
}

// NewEndpointSelector creates a new endpoint selector with the specified strategy
func NewEndpointSelector(strategy LoadBalancingStrategy) *EndpointSelector {
	if strategy != StrategyRoundRobin && strategy != StrategyWeighted {
		strategy = StrategyRoundRobin
	}
	
	return &EndpointSelector{
		strategy: strategy,
	}
}

// SelectEndpoint selects an endpoint from the healthy endpoints based on the configured strategy
func (s *EndpointSelector) SelectEndpoint(healthyEndpoints []*Endpoint) *Endpoint {
	if len(healthyEndpoints) == 0 {
		return nil
	}

	switch s.strategy {
	case StrategyWeighted:
		return s.selectWeighted(healthyEndpoints)
	case StrategyRoundRobin:
		fallthrough
	default:
		return s.selectRoundRobin(healthyEndpoints)
	}
}

// selectRoundRobin implements round-robin selection
func (s *EndpointSelector) selectRoundRobin(endpoints []*Endpoint) *Endpoint {
	if len(endpoints) == 1 {
		return endpoints[0]
	}
	
	index := s.currentIndex.Add(1) % uint32(len(endpoints))
	return endpoints[index]
}

// selectWeighted implements weighted selection based on health scores
func (s *EndpointSelector) selectWeighted(endpoints []*Endpoint) *Endpoint {
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
		return s.selectRoundRobin(endpoints)
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

// GetStrategy returns the current strategy
func (s *EndpointSelector) GetStrategy() LoadBalancingStrategy {
	return s.strategy
}