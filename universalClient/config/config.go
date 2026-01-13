package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pushchain/push-chain-node/universalClient/constant"
)

//go:embed default_config.json
var defaultConfigJSON []byte

// LoadDefaultConfig loads the default configuration from the embedded JSON
func LoadDefaultConfig() (Config, error) {
	var cfg Config
	if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal default config: %w", err)
	}

	// Validate the config (default config validates against itself)
	if err := validateConfig(&cfg, nil); err != nil {
		return Config{}, fmt.Errorf("invalid default config: %w", err)
	}

	return cfg, nil
}

func validateConfig(cfg *Config, defaultCfg *Config) error {
	// Validate log level
	if cfg.LogLevel < 0 || cfg.LogLevel > 5 {
		return fmt.Errorf("log level must be between 0 and 5")
	}

	// Validate log format
	if cfg.LogFormat != "json" && cfg.LogFormat != "console" {
		return fmt.Errorf("log format must be 'json' or 'console'")
	}

	// Set defaults for registry config from default config
	if cfg.ConfigRefreshIntervalSeconds == 0 && defaultCfg != nil {
		cfg.ConfigRefreshIntervalSeconds = defaultCfg.ConfigRefreshIntervalSeconds
	}
	if cfg.MaxRetries == 0 && defaultCfg != nil {
		cfg.MaxRetries = defaultCfg.MaxRetries
	}
	if cfg.RetryBackoffSeconds == 0 && defaultCfg != nil {
		cfg.RetryBackoffSeconds = defaultCfg.RetryBackoffSeconds
	}

	// Set defaults for startup config from default config
	if cfg.InitialFetchRetries == 0 && defaultCfg != nil {
		cfg.InitialFetchRetries = defaultCfg.InitialFetchRetries
	}
	if cfg.InitialFetchTimeoutSeconds == 0 && defaultCfg != nil {
		cfg.InitialFetchTimeoutSeconds = defaultCfg.InitialFetchTimeoutSeconds
	}

	// Set defaults for registry config from default config
	if len(cfg.PushChainGRPCURLs) == 0 && defaultCfg != nil {
		cfg.PushChainGRPCURLs = defaultCfg.PushChainGRPCURLs
	}

	// Set defaults for query server from default config
	if cfg.QueryServerPort == 0 && defaultCfg != nil {
		cfg.QueryServerPort = defaultCfg.QueryServerPort
	}

	// Set defaults and validate hot key management config
	// Don't override if already set, just validate
	if cfg.KeyringBackend != "" {
		// Validate keyring backend
		if cfg.KeyringBackend != KeyringBackendFile && cfg.KeyringBackend != KeyringBackendTest {
			// Try to fix common case issues
			if cfg.KeyringBackend == "test" || cfg.KeyringBackend == KeyringBackend("test") {
				cfg.KeyringBackend = KeyringBackendTest
			} else if cfg.KeyringBackend == "file" || cfg.KeyringBackend == KeyringBackend("file") {
				cfg.KeyringBackend = KeyringBackendFile
			} else {
				return fmt.Errorf("keyring backend must be 'file' or 'test', got: %s", cfg.KeyringBackend)
			}
		}
	} else if defaultCfg != nil {
		cfg.KeyringBackend = defaultCfg.KeyringBackend
	}

	// Set defaults for event monitoring from default config
	if cfg.EventPollingIntervalSeconds == 0 && defaultCfg != nil {
		cfg.EventPollingIntervalSeconds = defaultCfg.EventPollingIntervalSeconds
	}

	// Set defaults for transaction cleanup from default config
	if cfg.TransactionCleanupIntervalSeconds == 0 && defaultCfg != nil {
		cfg.TransactionCleanupIntervalSeconds = defaultCfg.TransactionCleanupIntervalSeconds
	}
	if cfg.TransactionRetentionPeriodSeconds == 0 && defaultCfg != nil {
		cfg.TransactionRetentionPeriodSeconds = defaultCfg.TransactionRetentionPeriodSeconds
	}

	// Initialize ChainConfigs if nil or empty
	if (cfg.ChainConfigs == nil || len(cfg.ChainConfigs) == 0) && defaultCfg != nil {
		cfg.ChainConfigs = defaultCfg.ChainConfigs
	}

	// Set defaults for RPC pool config from default config
	if defaultCfg != nil {
		if cfg.RPCPoolConfig.HealthCheckIntervalSeconds == 0 {
			cfg.RPCPoolConfig.HealthCheckIntervalSeconds = defaultCfg.RPCPoolConfig.HealthCheckIntervalSeconds
		}
		if cfg.RPCPoolConfig.UnhealthyThreshold == 0 {
			cfg.RPCPoolConfig.UnhealthyThreshold = defaultCfg.RPCPoolConfig.UnhealthyThreshold
		}
		if cfg.RPCPoolConfig.RecoveryIntervalSeconds == 0 {
			cfg.RPCPoolConfig.RecoveryIntervalSeconds = defaultCfg.RPCPoolConfig.RecoveryIntervalSeconds
		}
		if cfg.RPCPoolConfig.MinHealthyEndpoints == 0 {
			cfg.RPCPoolConfig.MinHealthyEndpoints = defaultCfg.RPCPoolConfig.MinHealthyEndpoints
		}
		if cfg.RPCPoolConfig.RequestTimeoutSeconds == 0 {
			cfg.RPCPoolConfig.RequestTimeoutSeconds = defaultCfg.RPCPoolConfig.RequestTimeoutSeconds
		}
		if cfg.RPCPoolConfig.LoadBalancingStrategy == "" {
			cfg.RPCPoolConfig.LoadBalancingStrategy = defaultCfg.RPCPoolConfig.LoadBalancingStrategy
		}
	}

	// Validate load balancing strategy
	if cfg.RPCPoolConfig.LoadBalancingStrategy != "" &&
		cfg.RPCPoolConfig.LoadBalancingStrategy != "round-robin" &&
		cfg.RPCPoolConfig.LoadBalancingStrategy != "weighted" {
		return fmt.Errorf("load balancing strategy must be 'round-robin' or 'weighted'")
	}

	// Set TSS defaults
	if cfg.TSSP2PListen == "" {
		cfg.TSSP2PListen = "/ip4/0.0.0.0/tcp/39000"
	}

	// Validate TSS config (TSS is always enabled)
	// Skip TSS validation when defaultCfg is nil (validating default config itself)
	if defaultCfg != nil {
		// Set TSS defaults from default config if available
		if cfg.TSSP2PPrivateKeyHex == "" && defaultCfg.TSSP2PPrivateKeyHex != "" {
			cfg.TSSP2PPrivateKeyHex = defaultCfg.TSSP2PPrivateKeyHex
		}
		if cfg.TSSPassword == "" && defaultCfg.TSSPassword != "" {
			cfg.TSSPassword = defaultCfg.TSSPassword
		}

		// Validate required TSS fields
		if cfg.TSSP2PPrivateKeyHex == "" {
			return fmt.Errorf("tss_p2p_private_key_hex is required for TSS")
		}
		if cfg.TSSPassword == "" {
			return fmt.Errorf("tss_password is required for TSS")
		}
	}

	return nil
}

// Save writes the given config to <NodeDir>/config/pushuv_config.json.
func Save(cfg *Config, basePath string) error {
	// Load default config for validation
	defaultCfg, _ := LoadDefaultConfig()
	if err := validateConfig(cfg, &defaultCfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	configDir := filepath.Join(basePath, constant.ConfigSubdir)
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configFile := filepath.Join(configDir, constant.ConfigFileName)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// Load reads and returns the config from <BasePath>/config/pushuv_config.json.
func Load(basePath string) (Config, error) {
	configFile := filepath.Join(basePath, constant.ConfigSubdir, constant.ConfigFileName)
	data, err := os.ReadFile(filepath.Clean(configFile))
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Don't validate for now - let the config file values pass through
	// if err := validateConfig(&cfg); err != nil {
	//	return Config{}, fmt.Errorf("invalid config: %w", err)
	// }

	return cfg, nil
}
