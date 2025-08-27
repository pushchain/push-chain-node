package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateHotKeyConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid hot key config",
			config: Config{
				AuthzHotkey:    "test-hotkey",
				AuthzGranter:   "cosmos1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
				KeyringBackend: KeyringBackendFile,
				PChainHome:     "/tmp/test",
			},
			wantErr: false,
		},
		{
			name: "missing hot key",
			config: Config{
				AuthzGranter:   "cosmos1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
				KeyringBackend: KeyringBackendFile,
			},
			wantErr: true,
			errMsg:  "authz_hotkey is required",
		},
		{
			name: "missing granter",
			config: Config{
				AuthzHotkey:    "test-hotkey",
				KeyringBackend: KeyringBackendFile,
			},
			wantErr: true,
			errMsg:  "authz_granter is required",
		},
		{
			name: "invalid granter address",
			config: Config{
				AuthzHotkey:    "test-hotkey",
				AuthzGranter:   "invalid-address",
				KeyringBackend: KeyringBackendFile,
			},
			wantErr: true,
			errMsg:  "invalid authz granter address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHotKeyConfig(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsHotKeyConfigured(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name: "fully configured",
			config: Config{
				AuthzHotkey:  "test-hotkey",
				AuthzGranter: "cosmos1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
			},
			expected: true,
		},
		{
			name: "missing hotkey",
			config: Config{
				AuthzGranter: "cosmos1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
			},
			expected: false,
		},
		{
			name: "missing granter",
			config: Config{
				AuthzHotkey: "test-hotkey",
			},
			expected: false,
		},
		{
			name:     "both missing",
			config:   Config{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsHotKeyConfigured(&tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetKeyringDir(t *testing.T) {
	config := Config{
		PChainHome: "/tmp/test-home",
	}

	result := GetKeyringDir(&config)
	expected := "/tmp/test-home/keys"
	assert.Equal(t, expected, result)
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
	assert.NotEmpty(t, cfg.PChainHome)
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
		{
			name: "hotkey without granter",
			config: Config{
				LogLevel:    1,
				LogFormat:   "console",
				AuthzHotkey: "test-hotkey",
			},
			errMsg: "authz_granter must be set when authz_hotkey is configured",
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
