package common

import (
	"context"
	"math/big"
)

// GasPriceFetcher defines the interface for fetching gas prices from different chains
type GasPriceFetcher interface {
	// GetGasPrice fetches the current gas price from the chain
	// For EVM chains, returns the price in Wei
	// For Solana, returns the price in lamports per compute unit
	GetGasPrice(ctx context.Context) (*big.Int, error)
}

// GasPrice represents a gas price data point
type GasPrice struct {
	ChainID   string   `json:"chain_id"`
	Price     *big.Int `json:"price"`
	Unit      string   `json:"unit"` // "wei" for EVM, "lamports/cu" for Solana
	Timestamp int64    `json:"timestamp"`
}