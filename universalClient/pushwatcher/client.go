// Package pushwatcher provides a client for listening to Push Chain events.
package pushwatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/rs/zerolog"
)

// Client implements the ChainClient interface for Push chain
type Client struct {
	logger        zerolog.Logger
	pushCore      *pushcore.Client
	database      *db.DB
	eventListener *EventListener
	eventCleaner  *common.EventCleaner
	readProcessor *ReadProcessor
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewClient creates a new Push chain client.
// pushSigner and chainResolver may be nil; the read processor (read request
// execution + voting) is only wired when both are present.
func NewClient(
	database *db.DB,
	chainConfig *config.ChainSpecificConfig,
	pushCore *pushcore.Client,
	chainID string,
	logger zerolog.Logger,
	pushSigner *pushsigner.Signer,
	chainResolver ChainResolver,
) (*Client, error) {
	// Normalize nil config so downstream uses don't need nil guards.
	if chainConfig == nil {
		chainConfig = &config.ChainSpecificConfig{}
	}

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

	eventCleaner := common.NewEventCleaner(
		database,
		chainConfig.CleanupIntervalSeconds,
		chainConfig.RetentionPeriodSeconds,
		chainID,
		logger,
	)

	client := &Client{
		logger:        logger.With().Str("component", "push_client").Logger(),
		pushCore:      pushCore,
		database:      database,
		eventListener: eventListener,
		eventCleaner:  eventCleaner,
	}

	// The push DB holds READ_REQUEST events; the read processor executes them
	// on their destination chains (via chainResolver) and votes the results.
	if pushSigner != nil && chainResolver != nil {
		readProcessor, err := NewReadProcessor(
			pushSigner,
			chainResolver,
			database,
			eventListener.cfg.PollInterval,
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create read processor: %w", err)
		}
		client.readProcessor = readProcessor
	}

	return client, nil
}

// Start initializes and starts the Push chain client
func (c *Client) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.logger.Debug().Msg("starting Push chain client")

	// Start event listener
	if err := c.eventListener.Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start event listener: %w", err)
	}

	// Start event cleaner if configured
	if c.eventCleaner != nil {
		if err := c.eventCleaner.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to start event cleaner: %w", err)
		}
	}

	// Start read processor if wired
	if c.readProcessor != nil {
		if err := c.readProcessor.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to start read processor: %w", err)
		}
	}

	c.logger.Info().Msg("Push chain client started successfully")
	return nil
}

// Stop gracefully shuts down the Push chain client
func (c *Client) Stop() error {
	c.logger.Debug().Msg("stopping Push chain client")

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Stop event listener
	if c.eventListener != nil {
		if err := c.eventListener.Stop(); err != nil {
			c.logger.Error().Err(err).Str("subsystem", "event_listener").Msg("subsystem failed to stop")
		}
	}

	// Stop event cleaner
	if c.eventCleaner != nil {
		c.eventCleaner.Stop()
	}

	// Stop read processor
	if c.readProcessor != nil {
		if err := c.readProcessor.Stop(); err != nil {
			c.logger.Error().Err(err).Str("subsystem", "read_processor").Msg("subsystem failed to stop")
		}
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
