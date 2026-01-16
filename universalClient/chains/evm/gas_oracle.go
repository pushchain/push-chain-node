package evm

import (
	"context"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/rs/zerolog"
)

// GasOracle handles fetching and reporting gas prices
type GasOracle struct {
	rpcClient               *RPCClient
	pushSigner              *pushsigner.Signer
	chainID                 string
	gasPriceIntervalSeconds int
	logger                  zerolog.Logger
	stopCh                  chan struct{}
	wg                      sync.WaitGroup
}

// NewGasOracle creates a new gas oracle
func NewGasOracle(
	rpcClient *RPCClient,
	pushSigner *pushsigner.Signer,
	chainID string,
	gasPriceIntervalSeconds int,
	logger zerolog.Logger,
) *GasOracle {
	return &GasOracle{
		rpcClient:               rpcClient,
		pushSigner:              pushSigner,
		chainID:                 chainID,
		gasPriceIntervalSeconds: gasPriceIntervalSeconds,
		logger:                  logger.With().Str("component", "evm_gas_oracle").Logger(),
		stopCh:                  make(chan struct{}),
	}
}

// Start begins fetching and voting on gas prices
func (g *GasOracle) Start(ctx context.Context) error {
	g.wg.Add(1)
	go g.fetchAndVoteGasPrice(ctx)
	return nil
}

// Stop stops the gas oracle
func (g *GasOracle) Stop() {
	close(g.stopCh)
	g.wg.Wait()
}

// fetchAndVoteGasPrice periodically fetches gas price and votes on it
func (g *GasOracle) fetchAndVoteGasPrice(ctx context.Context) {
	defer g.wg.Done()

	// Get gas oracle fetch interval from config
	interval := g.getGasOracleFetchInterval()
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	g.logger.Info().
		Dur("interval", interval).
		Msg("starting gas price fetching and voting")

	for {
		select {
		case <-ctx.Done():
			g.logger.Info().Msg("context cancelled, stopping gas price fetcher")
			return
		case <-g.stopCh:
			g.logger.Info().Msg("stop signal received, stopping gas price fetcher")
			return
		case <-ticker.C:
			// Fetch current gas price
			gasPrice, err := g.rpcClient.GetGasPrice(ctx)
			if err != nil {
				g.logger.Error().Err(err).Msg("failed to fetch gas price")
				continue
			}

			// Log the gas price
			g.logger.Info().
				Str("chain", g.chainID).
				Str("gas_price", gasPrice.String()).
				Msg("fetched gas price")

			// Get current block number
			blockNumber, err := g.rpcClient.GetLatestBlock(ctx)
			if err != nil {
				g.logger.Error().Err(err).Msg("failed to get latest block number")
				continue
			}

			// Vote on gas price
			priceUint64 := gasPrice.Uint64()
			voteTxHash, err := g.pushSigner.VoteGasPrice(ctx, g.chainID, priceUint64, blockNumber)
			if err != nil {
				g.logger.Error().
					Err(err).
					Uint64("price", priceUint64).
					Uint64("block", blockNumber).
					Msg("failed to vote on gas price")
				continue
			}

			g.logger.Info().
				Str("vote_tx_hash", voteTxHash).
				Uint64("price", priceUint64).
				Uint64("block", blockNumber).
				Msg("successfully voted on gas price")
		}
	}
}

// getGasOracleFetchInterval returns the gas oracle fetch interval
func (g *GasOracle) getGasOracleFetchInterval() time.Duration {
	if g.gasPriceIntervalSeconds <= 0 {
		return 30 * time.Second
	}

	return time.Duration(g.gasPriceIntervalSeconds) * time.Second
}
