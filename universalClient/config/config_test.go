package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfig(t *testing.T) {
	testCases := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
		validate    func(t *testing.T, cfg *Config)
	}{
		{
			name: "Valid config with all fields",
			config: &Config{
				LogLevel:              2,
				LogFormat:             "json",
				ConfigRefreshInterval: 30 * time.Second,
				MaxRetries:            5,
				RetryBackoff:          2 * time.Second,
				InitialFetchRetries:   3,
				InitialFetchTimeout:   20 * time.Second,
				PushChainGRPCURLs:     []string{"localhost:9090"},
				QueryServerPort:       8080,
			},
			expectError: false,
		},
		{
			name: "Valid config with console log format",
			config: &Config{
				LogLevel:  1,
				LogFormat: "console",
			},
			expectError: false,
		},
		{
			name: "Invalid log level (negative)",
			config: &Config{
				LogLevel:  -1,
				LogFormat: "json",
			},
			expectError: true,
			errorMsg:    "log level must be between 0 and 5",
		},
		{
			name: "Invalid log level (too high)",
			config: &Config{
				LogLevel:  6,
				LogFormat: "json",
			},
			expectError: true,
			errorMsg:    "log level must be between 0 and 5",
		},
		{
			name: "Invalid log format",
			config: &Config{
				LogLevel:  2,
				LogFormat: "xml",
			},
			expectError: true,
			errorMsg:    "log format must be 'json' or 'console'",
		},
		{
			name: "Config with defaults applied",
			config: &Config{
				LogLevel:  2,
				LogFormat: "json",
			},
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 10*time.Second, cfg.ConfigRefreshInterval)
				assert.Equal(t, 3, cfg.MaxRetries)
				assert.Equal(t, time.Second, cfg.RetryBackoff)
				assert.Equal(t, 5, cfg.InitialFetchRetries)
				assert.Equal(t, 30*time.Second, cfg.InitialFetchTimeout)
				assert.Equal(t, []string{"localhost:9090"}, cfg.PushChainGRPCURLs)
				assert.Equal(t, 8080, cfg.QueryServerPort)
			},
		},
		{
			name: "Empty PushChainGRPCURLs gets default",
			config: &Config{
				LogLevel:          2,
				LogFormat:         "json",
				PushChainGRPCURLs: []string{},
			},
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, []string{"localhost:9090"}, cfg.PushChainGRPCURLs)
			},
		},
		{
			name: "Zero QueryServerPort gets default",
			config: &Config{
				LogLevel:        2,
				LogFormat:       "json",
				QueryServerPort: 0,
			},
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 8080, cfg.QueryServerPort)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfig(tc.config)

			if tc.expectError {
				assert.Error(t, err)
				if tc.errorMsg != "" {
					assert.Contains(t, err.Error(), tc.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tc.validate != nil {
					tc.validate(t, tc.config)
				}
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "config_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Run("Save and load valid config", func(t *testing.T) {
		cfg := &Config{
			LogLevel:              3,
			LogFormat:             "json",
			ConfigRefreshInterval: 20 * time.Second,
			MaxRetries:            5,
			RetryBackoff:          2 * time.Second,
			InitialFetchRetries:   10,
			InitialFetchTimeout:   60 * time.Second,
			PushChainGRPCURLs:     []string{"localhost:9090", "localhost:9091"},
			QueryServerPort:       8888,
		}

		// Save config
		err := Save(cfg, tempDir)
		require.NoError(t, err)

		// Verify file exists
		configPath := filepath.Join(tempDir, configSubdir, configFileName)
		_, err = os.Stat(configPath)
		assert.NoError(t, err)

		// Load config
		loadedCfg, err := Load(tempDir)
		require.NoError(t, err)

		// Verify loaded config matches saved config
		assert.Equal(t, cfg.LogLevel, loadedCfg.LogLevel)
		assert.Equal(t, cfg.LogFormat, loadedCfg.LogFormat)
		assert.Equal(t, cfg.ConfigRefreshInterval, loadedCfg.ConfigRefreshInterval)
		assert.Equal(t, cfg.MaxRetries, loadedCfg.MaxRetries)
		assert.Equal(t, cfg.RetryBackoff, loadedCfg.RetryBackoff)
		assert.Equal(t, cfg.InitialFetchRetries, loadedCfg.InitialFetchRetries)
		assert.Equal(t, cfg.InitialFetchTimeout, loadedCfg.InitialFetchTimeout)
		assert.Equal(t, cfg.PushChainGRPCURLs, loadedCfg.PushChainGRPCURLs)
		assert.Equal(t, cfg.QueryServerPort, loadedCfg.QueryServerPort)
	})

	t.Run("Save invalid config", func(t *testing.T) {
		cfg := &Config{
			LogLevel:  -1, // Invalid
			LogFormat: "json",
		}

		err := Save(cfg, tempDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid config")
	})

	t.Run("Load from non-existent file", func(t *testing.T) {
		nonExistentDir := filepath.Join(tempDir, "non_existent")
		_, err := Load(nonExistentDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read config file")
	})

	t.Run("Load invalid JSON", func(t *testing.T) {
		// Create config directory
		configDir := filepath.Join(tempDir, "invalid", configSubdir)
		err := os.MkdirAll(configDir, 0o750)
		require.NoError(t, err)

		// Write invalid JSON
		configPath := filepath.Join(configDir, configFileName)
		err = os.WriteFile(configPath, []byte("{invalid json}"), 0o600)
		require.NoError(t, err)

		_, err = Load(filepath.Join(tempDir, "invalid"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal config")
	})

	t.Run("Save with directory creation", func(t *testing.T) {
		newDir := filepath.Join(tempDir, "new_dir")
		cfg := &Config{
			LogLevel:  2,
			LogFormat: "json",
		}

		err := Save(cfg, newDir)
		require.NoError(t, err)

		// Verify directory was created
		configDir := filepath.Join(newDir, configSubdir)
		_, err = os.Stat(configDir)
		assert.NoError(t, err)
	})
}

func TestConfigJSONMarshaling(t *testing.T) {
	t.Run("Marshal and unmarshal config", func(t *testing.T) {
		cfg := &Config{
			LogLevel:              2,
			LogFormat:             "console",
			ConfigRefreshInterval: 15 * time.Second,
			MaxRetries:            3,
			RetryBackoff:          500 * time.Millisecond,
			InitialFetchRetries:   5,
			InitialFetchTimeout:   30 * time.Second,
			PushChainGRPCURLs:     []string{"host1:9090", "host2:9090"},
			QueryServerPort:       8080,
		}

		// Marshal to JSON
		data, err := json.MarshalIndent(cfg, "", "  ")
		require.NoError(t, err)

		// Unmarshal back
		var unmarshaledCfg Config
		err = json.Unmarshal(data, &unmarshaledCfg)
		require.NoError(t, err)

		// Compare
		assert.Equal(t, cfg.LogLevel, unmarshaledCfg.LogLevel)
		assert.Equal(t, cfg.LogFormat, unmarshaledCfg.LogFormat)
		assert.Equal(t, cfg.ConfigRefreshInterval, unmarshaledCfg.ConfigRefreshInterval)
		assert.Equal(t, cfg.MaxRetries, unmarshaledCfg.MaxRetries)
		assert.Equal(t, cfg.RetryBackoff, unmarshaledCfg.RetryBackoff)
		assert.Equal(t, cfg.InitialFetchRetries, unmarshaledCfg.InitialFetchRetries)
		assert.Equal(t, cfg.InitialFetchTimeout, unmarshaledCfg.InitialFetchTimeout)
		assert.Equal(t, cfg.PushChainGRPCURLs, unmarshaledCfg.PushChainGRPCURLs)
		assert.Equal(t, cfg.QueryServerPort, unmarshaledCfg.QueryServerPort)
	})
}