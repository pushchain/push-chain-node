package evm

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/rs/zerolog"
)

// ChainMetaOracle handles fetching and reporting gas prices
type ChainMetaOracle struct {
	rpcClient               *RPCClient
	pushSigner              *pushsigner.Signer
	chainID                 string
	gasPriceIntervalSeconds int
	gasPriceMarkupPercent   int
	logger                  zerolog.Logger
	stopCh                  chan struct{}
	wg                      sync.WaitGroup
}

// NewChainMetaOracle creates a new gas oracle
func NewChainMetaOracle(
	rpcClient *RPCClient,
	pushSigner *pushsigner.Signer,
	chainID string,
	gasPriceIntervalSeconds int,
	gasPriceMarkupPercent int,
	logger zerolog.Logger,
) *ChainMetaOracle {
	return &ChainMetaOracle{
		rpcClient:               rpcClient,
		pushSigner:              pushSigner,
		chainID:                 chainID,
		gasPriceIntervalSeconds: gasPriceIntervalSeconds,
		gasPriceMarkupPercent:   gasPriceMarkupPercent,
		logger:                  logger.With().Str("component", "evm_chain_meta_oracle").Logger(),
		stopCh:                  make(chan struct{}),
	}
}

// Start begins fetching and voting on gas prices
func (g *ChainMetaOracle) Start(ctx context.Context) error {
	g.wg.Add(1)
	go g.fetchAndVoteChainMeta(ctx)
	return nil
}

// Stop stops the gas oracle
func (g *ChainMetaOracle) Stop() {
	close(g.stopCh)
	g.wg.Wait()
}

// fetchAndVoteChainMeta periodically fetches gas price and votes on it
func (g *ChainMetaOracle) fetchAndVoteChainMeta(ctx context.Context) {
	defer g.wg.Done()

	// Get gas oracle fetch interval from config
	interval := g.getChainMetaOracleFetchInterval()
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

			// Apply markup to gas price to handle spikes
			if g.gasPriceMarkupPercent > 0 {
				markup := new(big.Int).Mul(gasPrice, big.NewInt(int64(g.gasPriceMarkupPercent)))
				markup.Div(markup, big.NewInt(100))
				gasPrice.Add(gasPrice, markup)

				g.logger.Info().
					Str("chain", g.chainID).
					Int("markup_percent", g.gasPriceMarkupPercent).
					Str("adjusted_gas_price", gasPrice.String()).
					Msg("applied gas price markup")
			}

			// Vote on chain meta (gas price + block height)
			priceUint64 := gasPrice.Uint64()
			voteTxHash, err := g.pushSigner.VoteChainMeta(ctx, g.chainID, priceUint64, blockNumber)
			if err != nil {
				g.logger.Error().
					Err(err).
					Uint64("price", priceUint64).
					Uint64("block", blockNumber).
					Msg("failed to vote on chain meta")
				continue
			}

			g.logger.Info().
				Str("vote_tx_hash", voteTxHash).
				Uint64("price", priceUint64).
				Uint64("block", blockNumber).
				Msg("successfully voted on chain meta")
		}
	}
}

// getChainMetaOracleFetchInterval returns the gas oracle fetch interval
func (g *ChainMetaOracle) getChainMetaOracleFetchInterval() time.Duration {
	if g.gasPriceIntervalSeconds <= 0 {
		return 30 * time.Second
	}

	return time.Duration(g.gasPriceIntervalSeconds) * time.Second
}
