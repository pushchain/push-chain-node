package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	configSubdir   = "config"
	configFileName = "pushuv_config.json"
)

func validateConfig(cfg *Config) error {
	// Validate log level
	if cfg.LogLevel < 0 || cfg.LogLevel > 5 {
		return fmt.Errorf("log level must be between 0 and 5")
	}

	// Validate log format
	if cfg.LogFormat != "json" && cfg.LogFormat != "console" {
		return fmt.Errorf("log format must be 'json' or 'console'")
	}

	// Set defaults for registry config
	if cfg.ConfigRefreshInterval == 0 {
		cfg.ConfigRefreshInterval = 10 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff == 0 {
		cfg.RetryBackoff = time.Second
	}

	// Set defaults for startup config
	if cfg.InitialFetchRetries == 0 {
		cfg.InitialFetchRetries = 5
	}
	if cfg.InitialFetchTimeout == 0 {
		cfg.InitialFetchTimeout = 30 * time.Second
	}

	// Validate registry config
	if len(cfg.PushChainGRPCURLs) == 0 {
		// Default to localhost:9090 if no URLs provided
		cfg.PushChainGRPCURLs = []string{"localhost:9090"}
	}

	// Set defaults for query server
	if cfg.QueryServerPort == 0 {
		cfg.QueryServerPort = 8080
	}

	// Set defaults and validate hot key management config
	if cfg.KeyringBackend == "" {
		cfg.KeyringBackend = KeyringBackendFile // Default to secure file backend
	}
	
	// Validate keyring backend
	if cfg.KeyringBackend != KeyringBackendFile && cfg.KeyringBackend != KeyringBackendTest {
		return fmt.Errorf("keyring backend must be 'file' or 'test'")
	}
	
	// Set default PChainHome if not provided
	if cfg.PChainHome == "" {
		usr, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to get current user for default home directory: %w", err)
		}
		cfg.PChainHome = filepath.Join(usr.HomeDir, ".pushuv")
	}
	
	// Expand home directory if ~ is used
	if strings.HasPrefix(cfg.PChainHome, "~/") {
		usr, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to get current user for home expansion: %w", err)
		}
		cfg.PChainHome = filepath.Join(usr.HomeDir, cfg.PChainHome[2:])
	}
	
	// Validate operator address format if provided
	if cfg.AuthzGranter != "" {
		_, err := sdk.AccAddressFromBech32(cfg.AuthzGranter)
		if err != nil {
			return fmt.Errorf("invalid authz granter address format: %w", err)
		}
	}
	
	// If hot key is configured, granter must also be configured
	if cfg.AuthzHotkey != "" && cfg.AuthzGranter == "" {
		return fmt.Errorf("authz_granter must be set when authz_hotkey is configured")
	}

	return nil
}

// Save writes the given config to <NodeDir>/config/pushuv_config.json.
func Save(cfg *Config, basePath string) error {
	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	configDir := filepath.Join(basePath, configSubdir)
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configFile := filepath.Join(configDir, configFileName)
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
	configFile := filepath.Join(basePath, configSubdir, configFileName)
	data, err := os.ReadFile(filepath.Clean(configFile))
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}

// ValidateHotKeyConfig validates the hot key configuration is complete and valid
func ValidateHotKeyConfig(cfg *Config) error {
	if cfg.AuthzHotkey == "" {
		return fmt.Errorf("authz_hotkey is required for hot key operations")
	}
	
	if cfg.AuthzGranter == "" {
		return fmt.Errorf("authz_granter is required for hot key operations")
	}
	
	// Validate granter address format
	_, err := sdk.AccAddressFromBech32(cfg.AuthzGranter)
	if err != nil {
		return fmt.Errorf("invalid authz granter address: %w", err)
	}
	
	return nil
}

// IsHotKeyConfigured checks if hot key configuration is present
func IsHotKeyConfigured(cfg *Config) bool {
	return cfg.AuthzHotkey != "" && cfg.AuthzGranter != ""
}

// GetKeyringDir returns the full path to the keyring directory
func GetKeyringDir(cfg *Config) string {
	return filepath.Join(cfg.PChainHome, "keys")
}
