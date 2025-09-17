package evm

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

// GetGasPrice fetches the current gas price from the EVM chain
// Returns the price in Wei
func (c *Client) GetGasPrice(ctx context.Context) (*big.Int, error) {
	if !c.IsHealthy() {
		return nil, fmt.Errorf("chain client is not healthy")
	}

	var gasPrice *big.Int
	err := c.executeWithFailover(ctx, "get_gas_price", func(client *ethclient.Client) error {
		// Create a timeout context for the gas price call
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		price, err := client.SuggestGasPrice(callCtx)
		if err != nil {
			return fmt.Errorf("failed to get gas price: %w", err)
		}

		gasPrice = price
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Log the gas price
	c.logger.Info().
		Str("chain", c.ChainID()).
		Str("gas_price_wei", gasPrice.String()).
		Str("gas_price_gwei", weiToGwei(gasPrice)).
		Msg("fetched gas price")

	return gasPrice, nil
}

// weiToGwei converts wei to gwei for logging
func weiToGwei(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	gwei := new(big.Int).Div(wei, big.NewInt(1e9))
	return gwei.String()
}