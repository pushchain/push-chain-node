package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTSSPrivateKeyHex = "0101010101010101010101010101010101010101010101010101010101010101"
	testTSSPassword      = "testpassword"
)

func applyDefaultsAndValidate(t *testing.T, cfg *Config) error {
	t.Helper()
	defaults, err := LoadDefaultConfig()
	require.NoError(t, err)
	applyDefaults(cfg, &defaults)
	return validate(cfg)
}

func TestLoadDefaultConfig(t *testing.T) {
	cfg, err := LoadDefaultConfig()
	require.NoError(t, err)
	assert.NotZero(t, cfg.ConfigRefreshIntervalSeconds)
	assert.NotZero(t, cfg.MaxRetries)
	assert.NotZero(t, cfg.QueryServerPort)
	assert.NotEmpty(t, cfg.PushChainGRPCURLs)
	assert.Equal(t, "console", cfg.LogFormat)
}

func TestApplyDefaults(t *testing.T) {
	t.Run("fills zero-valued fields", func(t *testing.T) {
		cfg := &Config{
			LogLevel:            2,
			LogFormat:           "json",
			TSSP2PPrivateKeyHex: testTSSPrivateKeyHex,
			TSSPassword:         testTSSPassword,
		}

		err := applyDefaultsAndValidate(t, cfg)
		require.NoError(t, err)

		defaults, _ := LoadDefaultConfig()
		assert.Equal(t, defaults.ConfigRefreshIntervalSeconds, cfg.ConfigRefreshIntervalSeconds)
		assert.Equal(t, defaults.MaxRetries, cfg.MaxRetries)
		assert.Equal(t, defaults.PushChainGRPCURLs, cfg.PushChainGRPCURLs)
		assert.Equal(t, defaults.QueryServerPort, cfg.QueryServerPort)
		assert.Equal(t, defaults.KeyringBackend, cfg.KeyringBackend)
		assert.NotEmpty(t, cfg.NodeHome)
		assert.NotEmpty(t, cfg.TSSP2PListen)
	})

	t.Run("preserves explicit values", func(t *testing.T) {
		cfg := &Config{
			LogLevel:                     2,
			LogFormat:                    "json",
			ConfigRefreshIntervalSeconds: 30,
			MaxRetries:                   5,
			PushChainGRPCURLs:            []string{"custom:9090"},
			QueryServerPort:              9999,
			KeyringBackend:               KeyringBackendFile,
			TSSP2PPrivateKeyHex:          testTSSPrivateKeyHex,
			TSSPassword:                  testTSSPassword,
		}

		err := applyDefaultsAndValidate(t, cfg)
		require.NoError(t, err)

		assert.Equal(t, 30, cfg.ConfigRefreshIntervalSeconds)
		assert.Equal(t, 5, cfg.MaxRetries)
		assert.Equal(t, []string{"custom:9090"}, cfg.PushChainGRPCURLs)
		assert.Equal(t, 9999, cfg.QueryServerPort)
		assert.Equal(t, KeyringBackendFile, cfg.KeyringBackend)
	})

	t.Run("empty slice gets default", func(t *testing.T) {
		cfg := &Config{
			LogLevel:            2,
			LogFormat:           "json",
			PushChainGRPCURLs:   []string{},
			TSSP2PPrivateKeyHex: testTSSPrivateKeyHex,
			TSSPassword:         testTSSPassword,
		}

		err := applyDefaultsAndValidate(t, cfg)
		require.NoError(t, err)

		defaults, _ := LoadDefaultConfig()
		assert.Equal(t, defaults.PushChainGRPCURLs, cfg.PushChainGRPCURLs)
	})
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		errMsg string
	}{
		{
			name:   "valid minimal config",
			config: Config{LogLevel: 1, LogFormat: "console"},
		},
		{
			name:   "valid json format",
			config: Config{LogLevel: 0, LogFormat: "json"},
		},
		{
			name:   "valid file backend",
			config: Config{LogLevel: 1, LogFormat: "console", KeyringBackend: KeyringBackendFile},
		},
		{
			name:   "log level too high",
			config: Config{LogLevel: 6, LogFormat: "json"},
			errMsg: "log level must be between 0 and 5",
		},
		{
			name:   "log level negative",
			config: Config{LogLevel: -1, LogFormat: "json"},
			errMsg: "log level must be between 0 and 5",
		},
		{
			name:   "invalid log format",
			config: Config{LogLevel: 1, LogFormat: "xml"},
			errMsg: "log format must be 'json' or 'console'",
		},
		{
			name:   "invalid keyring backend",
			config: Config{LogLevel: 1, LogFormat: "console", KeyringBackend: "invalid"},
			errMsg: "keyring backend must be 'file' or 'test'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(&tt.config)
			if tt.errMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &Config{
			LogLevel:                     3,
			LogFormat:                    "json",
			ConfigRefreshIntervalSeconds: 20,
			MaxRetries:                   5,
			PushChainGRPCURLs:            []string{"localhost:9090", "localhost:9091"},
			QueryServerPort:              8888,
			TSSP2PPrivateKeyHex:          testTSSPrivateKeyHex,
			TSSPassword:                  testTSSPassword,
		}

		err := Save(cfg, dir)
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(dir, ConfigSubdir, ConfigFileName))

		loaded, err := Load(dir)
		require.NoError(t, err)

		assert.Equal(t, cfg.LogLevel, loaded.LogLevel)
		assert.Equal(t, cfg.LogFormat, loaded.LogFormat)
		assert.Equal(t, cfg.ConfigRefreshIntervalSeconds, loaded.ConfigRefreshIntervalSeconds)
		assert.Equal(t, cfg.MaxRetries, loaded.MaxRetries)
		assert.Equal(t, cfg.PushChainGRPCURLs, loaded.PushChainGRPCURLs)
		assert.Equal(t, cfg.QueryServerPort, loaded.QueryServerPort)
	})

	t.Run("save invalid config fails", func(t *testing.T) {
		err := Save(&Config{LogLevel: -1, LogFormat: "json"}, t.TempDir())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid config")
	})

	t.Run("load non-existent file fails", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nope"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read config file")
	})

	t.Run("load invalid JSON fails", func(t *testing.T) {
		dir := t.TempDir()
		configDir := filepath.Join(dir, ConfigSubdir)
		require.NoError(t, os.MkdirAll(configDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(configDir, ConfigFileName), []byte("{bad}"), 0o600))

		_, err := Load(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal config")
	})

	t.Run("save creates directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested")
		cfg := &Config{
			LogLevel:            2,
			LogFormat:           "json",
			TSSP2PPrivateKeyHex: testTSSPrivateKeyHex,
			TSSPassword:         testTSSPassword,
		}

		require.NoError(t, Save(cfg, dir))
		assert.DirExists(t, filepath.Join(dir, ConfigSubdir))
	})
}

func TestConfigJSONRoundTrip(t *testing.T) {
	cfg := &Config{
		LogLevel:                     2,
		LogFormat:                    "console",
		ConfigRefreshIntervalSeconds: 15,
		MaxRetries:                   3,
		PushChainGRPCURLs:            []string{"host1:9090", "host2:9090"},
		QueryServerPort:              8080,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)

	var loaded Config
	require.NoError(t, json.Unmarshal(data, &loaded))

	assert.Equal(t, cfg.LogLevel, loaded.LogLevel)
	assert.Equal(t, cfg.LogFormat, loaded.LogFormat)
	assert.Equal(t, cfg.PushChainGRPCURLs, loaded.PushChainGRPCURLs)
}

func TestGetChainCleanupSettings(t *testing.T) {
	cleanup := 1800
	retention := 43200

	t.Run("returns settings", func(t *testing.T) {
		cfg := &Config{
			ChainConfigs: map[string]ChainSpecificConfig{
				"eip155:1": {CleanupIntervalSeconds: &cleanup, RetentionPeriodSeconds: &retention},
			},
		}
		c, r, err := cfg.GetChainCleanupSettings("eip155:1")
		require.NoError(t, err)
		assert.Equal(t, 1800, c)
		assert.Equal(t, 43200, r)
	})

	t.Run("missing chain", func(t *testing.T) {
		cfg := &Config{ChainConfigs: map[string]ChainSpecificConfig{}}
		_, _, err := cfg.GetChainCleanupSettings("eip155:1")
		require.Error(t, err)
	})

	t.Run("missing cleanup interval", func(t *testing.T) {
		cfg := &Config{
			ChainConfigs: map[string]ChainSpecificConfig{
				"eip155:1": {RetentionPeriodSeconds: &retention},
			},
		}
		_, _, err := cfg.GetChainCleanupSettings("eip155:1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cleanup_interval_seconds")
	})
}
