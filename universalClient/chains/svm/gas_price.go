package svm

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/gagliardetto/solana-go/rpc"
)

// GetGasPrice fetches the current gas price from the Solana chain
// Returns the median priority fee in lamports per compute unit
func (c *Client) GetGasPrice(ctx context.Context) (*big.Int, error) {
	if !c.IsHealthy() {
		return nil, fmt.Errorf("chain client is not healthy")
	}

	// Use executeWithFailover to handle RPC calls with automatic failover
	// GetRecentPrioritizationFees returns a slice of structs with Slot and PrioritizationFee fields
	type prioritizationFee struct {
		Slot              uint64
		PrioritizationFee uint64
	}
	var result []prioritizationFee
	
	err := c.executeWithFailover(ctx, "get_gas_price", func(client *rpc.Client) error {
		fees, err := client.GetRecentPrioritizationFees(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to get recent prioritization fees: %w", err)
		}
		// Convert to our local type
		for _, fee := range fees {
			result = append(result, prioritizationFee{
				Slot:              fee.Slot,
				PrioritizationFee: fee.PrioritizationFee,
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		// No recent fees, return a default minimum
		defaultFee := big.NewInt(1000) // 1000 lamports per compute unit as default
		c.logger.Warn().
			Str("chain", c.ChainID()).
			Str("default_fee", defaultFee.String()).
			Msg("no recent prioritization fees found, using default")
		return defaultFee, nil
	}

	// Collect all non-zero fees
	var fees []uint64
	for _, fee := range result {
		if fee.PrioritizationFee > 0 {
			fees = append(fees, fee.PrioritizationFee)
		}
	}

	// If no non-zero fees, use default
	if len(fees) == 0 {
		defaultFee := big.NewInt(1000)
		c.logger.Info().
			Str("chain", c.ChainID()).
			Str("default_fee", defaultFee.String()).
			Msg("all recent fees are zero, using default")
		return defaultFee, nil
	}

	// Calculate median fee
	medianFee := calculateMedian(fees)
	gasPriceBig := big.NewInt(int64(medianFee))

	// Log the gas price
	c.logger.Info().
		Str("chain", c.ChainID()).
		Str("gas_price_lamports_per_cu", gasPriceBig.String()).
		Int("samples", len(fees)).
		Msg("fetched gas price")

	return gasPriceBig, nil
}

// calculateMedian calculates the median of a slice of uint64 values
func calculateMedian(fees []uint64) uint64 {
	if len(fees) == 0 {
		return 0
	}

	// Sort the fees
	sort.Slice(fees, func(i, j int) bool {
		return fees[i] < fees[j]
	})

	// Calculate median
	n := len(fees)
	if n%2 == 0 {
		// Even number of elements, take average of middle two
		return (fees[n/2-1] + fees[n/2]) / 2
	}
	// Odd number of elements, take the middle one
	return fees[n/2]
}

