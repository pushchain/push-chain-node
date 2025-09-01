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
			name: "Valid config with all fields",
			config: &Config{
				LogLevel:                       2,
				LogFormat:                      "json",
				ConfigRefreshIntervalSeconds:   30,
				MaxRetries:                     5,
				RetryBackoffSeconds:            2,
				InitialFetchRetries:            3,
				InitialFetchTimeoutSeconds:     20,
				PushChainGRPCURLs:              []string{"localhost:9090"},
				QueryServerPort:                8080,
			},
			expectError: false,
		},
		{
			name: "Valid config with console log format",
			config: &Config{
				LogLevel:  1,
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
				assert.Equal(t, 60, cfg.ConfigRefreshIntervalSeconds)
				assert.Equal(t, 3, cfg.MaxRetries)
				assert.Equal(t, 1, cfg.RetryBackoffSeconds)
				assert.Equal(t, 5, cfg.InitialFetchRetries)
				assert.Equal(t, 30, cfg.InitialFetchTimeoutSeconds)
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

func TestSaveAndLoad(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "config_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Run("Save and load valid config", func(t *testing.T) {
		cfg := &Config{
			LogLevel:                     3,
			LogFormat:                    "json",
			ConfigRefreshIntervalSeconds: 20,
			MaxRetries:                   5,
			RetryBackoffSeconds:          2,
			InitialFetchRetries:          10,
			InitialFetchTimeoutSeconds:   60,
			PushChainGRPCURLs:            []string{"localhost:9090", "localhost:9091"},
			QueryServerPort:              8888,
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
		assert.Equal(t, cfg.ConfigRefreshIntervalSeconds, loadedCfg.ConfigRefreshIntervalSeconds)
		assert.Equal(t, cfg.MaxRetries, loadedCfg.MaxRetries)
		assert.Equal(t, cfg.RetryBackoffSeconds, loadedCfg.RetryBackoffSeconds)
		assert.Equal(t, cfg.InitialFetchRetries, loadedCfg.InitialFetchRetries)
		assert.Equal(t, cfg.InitialFetchTimeoutSeconds, loadedCfg.InitialFetchTimeoutSeconds)
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
			LogLevel:                     2,
			LogFormat:                    "console",
			ConfigRefreshIntervalSeconds: 15,
			MaxRetries:                   3,
			RetryBackoffSeconds:          1,
			InitialFetchRetries:          5,
			InitialFetchTimeoutSeconds:   30,
			PushChainGRPCURLs:            []string{"host1:9090", "host2:9090"},
			QueryServerPort:              8080,
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
		assert.Equal(t, cfg.ConfigRefreshIntervalSeconds, unmarshaledCfg.ConfigRefreshIntervalSeconds)
		assert.Equal(t, cfg.MaxRetries, unmarshaledCfg.MaxRetries)
		assert.Equal(t, cfg.RetryBackoffSeconds, unmarshaledCfg.RetryBackoffSeconds)
		assert.Equal(t, cfg.InitialFetchRetries, unmarshaledCfg.InitialFetchRetries)
		assert.Equal(t, cfg.InitialFetchTimeoutSeconds, unmarshaledCfg.InitialFetchTimeoutSeconds)
		assert.Equal(t, cfg.PushChainGRPCURLs, unmarshaledCfg.PushChainGRPCURLs)
		assert.Equal(t, cfg.QueryServerPort, unmarshaledCfg.QueryServerPort)
	})
}
