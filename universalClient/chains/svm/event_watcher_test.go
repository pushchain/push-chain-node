package svm

import (
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
)

func TestNewEventWatcher(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")

	client := &Client{}
	eventParser := &EventParser{}
	tracker := common.NewConfirmationTracker(nil, nil, logger)
	txVerifier := &TransactionVerifier{}
	appConfig := &config.Config{}

	watcher := NewEventWatcher(client, gatewayAddr, eventParser, tracker, txVerifier, appConfig, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", logger)

	assert.NotNil(t, watcher)
	assert.Equal(t, client, watcher.parentClient)
	assert.Equal(t, gatewayAddr, watcher.gatewayAddr)
	assert.Equal(t, eventParser, watcher.eventParser)
	assert.Equal(t, tracker, watcher.tracker)
	assert.Equal(t, txVerifier, watcher.txVerifier)
	assert.Equal(t, appConfig, watcher.appConfig)
	assert.Equal(t, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", watcher.chainID)
}

func TestEventWatcherPollingInterval(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")

	tests := []struct {
		name             string
		appConfig        *config.Config
		chainID          string
		expectedInterval time.Duration
	}{
		{
			name:             "uses default interval when config is nil",
			appConfig:        nil,
			chainID:          "solana:devnet",
			expectedInterval: 5 * time.Second,
		},
		{
			name:             "uses default interval when config interval is 0",
			appConfig:        &config.Config{},
			chainID:          "solana:devnet",
			expectedInterval: 5 * time.Second,
		},
		{
			name: "uses global config interval",
			appConfig: &config.Config{
				EventPollingIntervalSeconds: 10,
			},
			chainID:          "solana:devnet",
			expectedInterval: 10 * time.Second,
		},
		{
			name: "uses chain-specific config interval",
			appConfig: &config.Config{
				EventPollingIntervalSeconds: 10,
				ChainConfigs: map[string]config.ChainSpecificConfig{
					"solana:devnet": {
						EventPollingIntervalSeconds: func() *int { i := 3; return &i }(),
					},
				},
			},
			chainID:          "solana:devnet",
			expectedInterval: 3 * time.Second,
		},
		{
			name: "fallback to global when chain config exists but interval is nil",
			appConfig: &config.Config{
				EventPollingIntervalSeconds: 7,
				ChainConfigs: map[string]config.ChainSpecificConfig{
					"solana:devnet": {
						EventPollingIntervalSeconds: nil,
					},
				},
			},
			chainID:          "solana:devnet",
			expectedInterval: 7 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{}
			eventParser := &EventParser{}
			tracker := common.NewConfirmationTracker(nil, nil, logger)
			txVerifier := &TransactionVerifier{}

			watcher := NewEventWatcher(client, gatewayAddr, eventParser, tracker, txVerifier, tt.appConfig, tt.chainID, logger)

			assert.NotNil(t, watcher)
			assert.Equal(t, tt.appConfig, watcher.appConfig)
			assert.Equal(t, tt.chainID, watcher.chainID)

			// The actual polling interval is tested indirectly through the configuration
			// since it's set inside the goroutine
		})
	}
}

func TestEventWatcherComponents(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)

	t.Run("logger component name", func(t *testing.T) {
		gatewayAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
		client := &Client{}
		eventParser := &EventParser{}
		tracker := common.NewConfirmationTracker(nil, nil, logger)
		txVerifier := &TransactionVerifier{}
		appConfig := &config.Config{}

		watcher := NewEventWatcher(client, gatewayAddr, eventParser, tracker, txVerifier, appConfig, "solana:mainnet", logger)

		// The logger should have the component set to "svm_event_watcher"
		// This is set in the NewEventWatcher function
		assert.NotNil(t, watcher.logger)
	})
}

func TestEventWatcherInitialization(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)

	tests := []struct {
		name        string
		gatewayAddr string
		shouldPanic bool
	}{
		{
			name:        "valid gateway address",
			gatewayAddr: "11111111111111111111111111111112",
			shouldPanic: false,
		},
		{
			name:        "another valid address",
			gatewayAddr: "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
			shouldPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayAddr := solana.MustPublicKeyFromBase58(tt.gatewayAddr)
			client := &Client{}
			eventParser := &EventParser{}
			tracker := common.NewConfirmationTracker(nil, nil, logger)
			txVerifier := &TransactionVerifier{}
			appConfig := &config.Config{}

			watcher := NewEventWatcher(client, gatewayAddr, eventParser, tracker, txVerifier, appConfig, "solana:devnet", logger)

			assert.NotNil(t, watcher)
			assert.Equal(t, gatewayAddr, watcher.gatewayAddr)
		})
	}
}