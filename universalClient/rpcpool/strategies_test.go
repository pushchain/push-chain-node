package rpcpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewEndpointSelector(t *testing.T) {
	tests := []struct {
		name     string
		strategy LoadBalancingStrategy
		expected LoadBalancingStrategy
	}{
		{
			name:     "round robin strategy",
			strategy: StrategyRoundRobin,
			expected: StrategyRoundRobin,
		},
		{
			name:     "weighted strategy",
			strategy: StrategyWeighted,
			expected: StrategyWeighted,
		},
		{
			name:     "invalid strategy defaults to round robin",
			strategy: "invalid",
			expected: StrategyRoundRobin,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector := NewEndpointSelector(tt.strategy)
			assert.Equal(t, tt.expected, selector.GetStrategy())
		})
	}
}

func TestEndpointSelector_SelectEndpoint_Empty(t *testing.T) {
	selector := NewEndpointSelector(StrategyRoundRobin)
	endpoints := []*Endpoint{}
	
	selected := selector.SelectEndpoint(endpoints)
	assert.Nil(t, selected)
}

func TestEndpointSelector_SelectEndpoint_Single(t *testing.T) {
	selector := NewEndpointSelector(StrategyRoundRobin)
	endpoint := NewEndpoint("http://test1.com")
	endpoints := []*Endpoint{endpoint}
	
	selected := selector.SelectEndpoint(endpoints)
	assert.Equal(t, endpoint, selected)
}

func TestEndpointSelector_RoundRobin(t *testing.T) {
	selector := NewEndpointSelector(StrategyRoundRobin)
	
	endpoint1 := NewEndpoint("http://test1.com")
	endpoint2 := NewEndpoint("http://test2.com")
	endpoint3 := NewEndpoint("http://test3.com")
	endpoints := []*Endpoint{endpoint1, endpoint2, endpoint3}
	
	// Test round robin distribution
	selections := make(map[*Endpoint]int)
	for i := 0; i < 9; i++ { // 3 complete cycles
		selected := selector.SelectEndpoint(endpoints)
		selections[selected]++
	}
	
	// Each endpoint should be selected 3 times
	assert.Equal(t, 3, selections[endpoint1])
	assert.Equal(t, 3, selections[endpoint2])
	assert.Equal(t, 3, selections[endpoint3])
}

func TestEndpointSelector_WeightedSelection(t *testing.T) {
	selector := NewEndpointSelector(StrategyWeighted)
	
	// Create endpoints with different health scores
	endpoint1 := NewEndpoint("http://test1.com")
	endpoint1.Metrics.HealthScore = 100.0 // Highest score
	
	endpoint2 := NewEndpoint("http://test2.com")
	endpoint2.Metrics.HealthScore = 50.0
	
	endpoint3 := NewEndpoint("http://test3.com")
	endpoint3.Metrics.HealthScore = 25.0 // Lowest score
	
	endpoints := []*Endpoint{endpoint1, endpoint2, endpoint3}
	
	// Test weighted distribution over many selections
	selections := make(map[*Endpoint]int)
	iterations := 1000
	
	for i := 0; i < iterations; i++ {
		selected := selector.SelectEndpoint(endpoints)
		selections[selected]++
	}
	
	// endpoint1 should be selected most often (highest weight)
	assert.Greater(t, selections[endpoint1], selections[endpoint2])
	assert.Greater(t, selections[endpoint2], selections[endpoint3])
	
	// All endpoints should be selected at least once
	assert.Greater(t, selections[endpoint1], 0)
	assert.Greater(t, selections[endpoint2], 0)
	assert.Greater(t, selections[endpoint3], 0)
}

func TestEndpointSelector_WeightedSelection_ZeroHealthScore(t *testing.T) {
	selector := NewEndpointSelector(StrategyWeighted)
	
	// Create endpoints with zero health scores
	endpoint1 := NewEndpoint("http://test1.com")
	endpoint1.Metrics.HealthScore = 0.0
	
	endpoint2 := NewEndpoint("http://test2.com")
	endpoint2.Metrics.HealthScore = 0.0
	
	endpoints := []*Endpoint{endpoint1, endpoint2}
	
	// Should fall back to round robin when all health scores are zero
	selections := make(map[*Endpoint]int)
	for i := 0; i < 10; i++ {
		selected := selector.SelectEndpoint(endpoints)
		selections[selected]++
	}
	
	// Both endpoints should be selected equally (round robin fallback)
	assert.Equal(t, 5, selections[endpoint1])
	assert.Equal(t, 5, selections[endpoint2])
}

func TestEndpointSelector_WeightedSelection_SingleEndpoint(t *testing.T) {
	selector := NewEndpointSelector(StrategyWeighted)
	
	endpoint := NewEndpoint("http://test.com")
	endpoint.Metrics.HealthScore = 75.0
	endpoints := []*Endpoint{endpoint}
	
	// Should always return the single endpoint
	for i := 0; i < 5; i++ {
		selected := selector.SelectEndpoint(endpoints)
		assert.Equal(t, endpoint, selected)
	}
}

func TestLoadBalancingStrategy_String(t *testing.T) {
	assert.Equal(t, "round-robin", string(StrategyRoundRobin))
	assert.Equal(t, "weighted", string(StrategyWeighted))
}