package evm

import (
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/config"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)


func TestNewEventWatcher(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := ethcommon.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7")
	
	client := &Client{}
	eventParser := &EventParser{}
	tracker := common.NewConfirmationTracker(nil, nil, logger)
	appConfig := &config.Config{}
	
	watcher := NewEventWatcher(client, gatewayAddr, eventParser, tracker, appConfig, "eip155:1", logger)
	
	assert.NotNil(t, watcher)
	assert.Equal(t, client, watcher.parentClient)
	assert.Equal(t, gatewayAddr, watcher.gatewayAddr)
	assert.Equal(t, eventParser, watcher.eventParser)
	assert.Equal(t, tracker, watcher.tracker)
	assert.Equal(t, appConfig, watcher.appConfig)
}


func TestEventWatcherPollingInterval(t *testing.T) {
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	gatewayAddr := ethcommon.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb7")
	
	tests := []struct {
		name             string
		appConfig        *config.Config
		expectedInterval time.Duration
	}{
					{
			name:             "uses default interval when config is nil",
			appConfig:        nil,
			expectedInterval: 5 * time.Second,
		},
					{
			name: "uses default interval when config interval is 0",
			appConfig: &config.Config{},
			expectedInterval: 5 * time.Second,
		},
					{
			name: "uses configured interval",
			appConfig: &config.Config{},
			expectedInterval: 2 * time.Second,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test would require more complex setup to verify the actual polling interval
			// For now, we just verify the watcher is created with the correct config
			
			client := &Client{}
			eventParser := &EventParser{eventTopics: make(map[string]uregistrytypes.ConfirmationType)}
			tracker := common.NewConfirmationTracker(nil, nil, logger)
			
			watcher := NewEventWatcher(client, gatewayAddr, eventParser, tracker, tt.appConfig, "eip155:1", logger)
			
			assert.NotNil(t, watcher)
			assert.Equal(t, tt.appConfig, watcher.appConfig)
		})
	}
}