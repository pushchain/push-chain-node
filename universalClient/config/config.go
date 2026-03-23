// Package config provides configuration loading, validation, and persistence
// for the Push Universal Validator.
//
// Directory layout:
//
//	<NodeHome>/                        (default: ~/.puniversal)
//	├── config/
//	│   └── pushuv_config.json
//	├── databases/
//	│   ├── eip155_1.db
//	│   └── eip155_97.db
//	└── relayer/
//	    └── <namespace>.json
package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	NodeDir         = ".puniversal"
	ConfigSubdir    = "config"
	ConfigFileName  = "pushuv_config.json"
	DatabasesSubdir = "databases"
	RelayerSubdir   = "relayer"
)

// DefaultNodeHome returns the default node home directory (~/.puniversal).
func DefaultNodeHome() string {
	return os.ExpandEnv("$HOME/") + NodeDir
}

//go:embed default_config.json
var defaultConfigJSON []byte

// LoadDefaultConfig loads the embedded default configuration.
func LoadDefaultConfig() (Config, error) {
	var cfg Config
	if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal default config: %w", err)
	}
	if err := validate(&cfg); err != nil {
		return Config{}, fmt.Errorf("invalid default config: %w", err)
	}
	return cfg, nil
}

// Load reads the config from <basePath>/config/pushuv_config.json,
// applies defaults for any missing fields, and validates.
func Load(basePath string) (Config, error) {
	path := filepath.Join(basePath, ConfigSubdir, ConfigFileName)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	defaults, _ := LoadDefaultConfig()
	applyDefaults(&cfg, &defaults)

	if err := validate(&cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Save validates the config and writes it to <basePath>/config/pushuv_config.json.
func Save(cfg *Config, basePath string) error {
	defaults, _ := LoadDefaultConfig()
	applyDefaults(cfg, &defaults)

	if err := validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	dir := filepath.Join(basePath, ConfigSubdir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// applyDefaults fills zero-valued fields in cfg from defaults.
func applyDefaults(cfg *Config, defaults *Config) {
	if defaults == nil {
		return
	}

	if cfg.NodeHome == "" {
		cfg.NodeHome = DefaultNodeHome()
	}
	if cfg.ConfigRefreshIntervalSeconds == 0 {
		cfg.ConfigRefreshIntervalSeconds = defaults.ConfigRefreshIntervalSeconds
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if len(cfg.PushChainGRPCURLs) == 0 {
		cfg.PushChainGRPCURLs = defaults.PushChainGRPCURLs
	}
	if cfg.QueryServerPort == 0 {
		cfg.QueryServerPort = defaults.QueryServerPort
	}
	if cfg.KeyringBackend == "" {
		cfg.KeyringBackend = defaults.KeyringBackend
	}
	if len(cfg.ChainConfigs) == 0 {
		cfg.ChainConfigs = defaults.ChainConfigs
	}
	if cfg.TSSP2PListen == "" {
		cfg.TSSP2PListen = "/ip4/0.0.0.0/tcp/39000"
	}
	if cfg.TSSP2PPrivateKeyHex == "" {
		cfg.TSSP2PPrivateKeyHex = defaults.TSSP2PPrivateKeyHex
	}
	if cfg.TSSPassword == "" {
		cfg.TSSPassword = defaults.TSSPassword
	}
}

// validate checks that all config values are within acceptable ranges.
// It does NOT apply defaults — call applyDefaults first if needed.
func validate(cfg *Config) error {
	if cfg.LogLevel < 0 || cfg.LogLevel > 5 {
		return fmt.Errorf("log level must be between 0 and 5")
	}
	if cfg.LogFormat != "json" && cfg.LogFormat != "console" {
		return fmt.Errorf("log format must be 'json' or 'console'")
	}
	if cfg.KeyringBackend != "" && cfg.KeyringBackend != KeyringBackendFile && cfg.KeyringBackend != KeyringBackendTest {
		return fmt.Errorf("keyring backend must be 'file' or 'test', got: %s", cfg.KeyringBackend)
	}
	return nil
}
