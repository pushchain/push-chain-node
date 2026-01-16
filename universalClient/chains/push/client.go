// Package push provides a client for listening to Push Chain events.
package push

import (
	"context"
	"fmt"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/rs/zerolog"
)

// Client implements the ChainClient interface for Push chain
type Client struct {
	logger        zerolog.Logger
	pushCore      *pushcore.Client
	database      *db.DB
	eventListener *EventListener
	eventCleaner  *common.EventCleaner
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewClient creates a new Push chain client
func NewClient(
	database *db.DB,
	chainConfig *config.ChainSpecificConfig,
	pushCore *pushcore.Client,
	chainID string,
	logger zerolog.Logger,
) (*Client, error) {

	// Create event listener
	eventListener, err := NewEventListener(
		pushCore,
		database,
		logger,
		chainConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create event listener: %w", err)
	}

	// Create event cleaner if config is provided
	var eventCleaner *common.EventCleaner
	if chainConfig != nil && chainConfig.CleanupIntervalSeconds != nil && chainConfig.RetentionPeriodSeconds != nil {
		cleanupInterval := time.Duration(*chainConfig.CleanupIntervalSeconds) * time.Second
		retentionPeriod := time.Duration(*chainConfig.RetentionPeriodSeconds) * time.Second
		eventCleaner = common.NewEventCleaner(
			database,
			cleanupInterval,
			retentionPeriod,
			chainID,
			logger,
		)
	}

	client := &Client{
		logger:        logger.With().Str("component", "push_client").Logger(),
		pushCore:      pushCore,
		database:      database,
		eventListener: eventListener,
		eventCleaner:  eventCleaner,
	}

	return client, nil
}

// Start initializes and starts the Push chain client
func (c *Client) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.logger.Info().Msg("starting Push chain client")

	// Start event listener
	if err := c.eventListener.Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start event listener: %w", err)
	}

	// Start event cleaner if configured
	if c.eventCleaner != nil {
		if err := c.eventCleaner.Start(c.ctx); err != nil {
			c.logger.Warn().Err(err).Msg("failed to start event cleaner")
			// Don't fail startup if cleaner fails
		}
	}

	c.logger.Info().Msg("Push chain client started successfully")
	return nil
}

// Stop gracefully shuts down the Push chain client
func (c *Client) Stop() error {
	c.logger.Info().Msg("stopping Push chain client")

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Stop event listener
	if c.eventListener != nil {
		if err := c.eventListener.Stop(); err != nil {
			c.logger.Error().Err(err).Msg("error stopping event listener")
		}
	}

	// Stop event cleaner
	if c.eventCleaner != nil {
		c.eventCleaner.Stop()
	}

	c.logger.Info().Msg("Push chain client stopped")
	return nil
}

// IsHealthy checks if the Push chain RPC Client is healthy
func (c *Client) IsHealthy() bool {
	if c.pushCore == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.pushCore.GetLatestBlock(ctx)
	return err == nil
}

// GetTxBuilder returns the OutboundTxBuilder for this chain
// Push chain does not support outbound transactions, so this always returns an error
func (c *Client) GetTxBuilder() (common.OutboundTxBuilder, error) {
	return nil, fmt.Errorf("txBuilder not supported for Push chain")
}
