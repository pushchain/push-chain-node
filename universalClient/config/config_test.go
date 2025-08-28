package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)



func TestGetKeyringDir(t *testing.T) {
	config := Config{}

	result := GetKeyringDir(&config)
	// Since we now use constant.DefaultNodeHome, test should check that path
	assert.Contains(t, result, "/.puniversal/keys")
}

func TestConfigValidation(t *testing.T) {
	// Test default settings
	cfg := &Config{
		LogLevel:  1,
		LogFormat: "console",
	}

	// This should set defaults and validate
	err := validateConfig(cfg)
	assert.NoError(t, err)

	// Check that defaults were set
	assert.NotZero(t, cfg.ConfigRefreshInterval)
	assert.NotZero(t, cfg.MaxRetries)
	assert.NotZero(t, cfg.RetryBackoff)
	assert.NotZero(t, cfg.InitialFetchRetries)
	assert.NotZero(t, cfg.InitialFetchTimeout)
	assert.NotZero(t, cfg.QueryServerPort)
	assert.Equal(t, KeyringBackendFile, cfg.KeyringBackend)
	assert.NotEmpty(t, cfg.PushChainGRPCURLs)
}

func TestInvalidConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		errMsg string
	}{
		{
			name: "invalid log level",
			config: Config{
				LogLevel:  10,
				LogFormat: "console",
			},
			errMsg: "log level must be between 0 and 5",
		},
		{
			name: "invalid log format",
			config: Config{
				LogLevel:  1,
				LogFormat: "invalid",
			},
			errMsg: "log format must be 'json' or 'console'",
		},
		{
			name: "invalid keyring backend",
			config: Config{
				LogLevel:       1,
				LogFormat:      "console",
				KeyringBackend: "invalid",
			},
			errMsg: "keyring backend must be 'file' or 'test'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.config)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}
