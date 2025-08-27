package config

import (
	"fmt"
	"time"
)

// KeyringBackend represents the type of keyring backend to use
type KeyringBackend string

const (
	// KeyringBackendTest is the test Cosmos keyring backend (unencrypted)
	KeyringBackendTest KeyringBackend = "test"
	
	// KeyringBackendFile is the file Cosmos keyring backend (encrypted)
	KeyringBackendFile KeyringBackend = "file"
)

type Config struct {
	// Log Config
	LogLevel   int    `json:"log_level"`   // e.g., 0 = debug, 1 = info, etc.
	LogFormat  string `json:"log_format"`  // "json" or "console"
	LogSampler bool   `json:"log_sampler"` // if true, samples logs (e.g., 1 in 5)

	// Registry Config
	PushChainGRPCURLs     []string      `json:"push_chain_grpc_urls"`    // Push Chain gRPC endpoints (default: ["localhost:9090"])
	ConfigRefreshInterval time.Duration `json:"config_refresh_interval"` // How often to refresh configs (default: 10m)
	MaxRetries            int           `json:"max_retries"`             // Max retry attempts for registry queries (default: 3)
	RetryBackoff          time.Duration `json:"retry_backoff"`           // Initial retry backoff duration (default: 1s)

	// Startup configuration
	InitialFetchRetries int           `json:"initial_fetch_retries"` // Number of retries for initial config fetch (default: 5)
	InitialFetchTimeout time.Duration `json:"initial_fetch_timeout"` // Timeout per initial fetch attempt (default: 30s)

	// Query Server configuration
	QueryServerPort int `json:"query_server_port"` // Port for HTTP query server (default: 8080)
	
	// Hot Key Management
	AuthzGranter   string         `json:"authz_granter"`    // Operator (validator) address that grants permissions
	AuthzHotkey    string         `json:"authz_hotkey"`     // Hot key name in keyring  
	KeyringBackend KeyringBackend `json:"keyring_backend"`  // Keyring backend type (file/test)
	PChainHome     string         `json:"pchain_home"`      // Directory for keyring storage (default: ~/.pushuv)
	
	// Message Type Configuration
	MessageTypeCategory string   `json:"message_type_category,omitempty"` // "default", "universal-validator", or "custom"
	CustomMessageTypes  []string `json:"custom_message_types,omitempty"`  // Custom message types (only used if category is "custom")
}

// ApplyMessageTypeConfiguration sets the allowed message types based on config
func (c *Config) ApplyMessageTypeConfiguration() error {
	// Import authz package to avoid circular imports
	// This will be called from the main application
	
	switch c.MessageTypeCategory {
	case "universal-validator":
		// Set to Universal Validator message types
		return nil // Will be set by caller using uauthz.UseUniversalValidatorMsgTypes()
	case "custom":
		// Set to custom message types from config
		if len(c.CustomMessageTypes) == 0 {
			return fmt.Errorf("custom message types cannot be empty when category is 'custom'")
		}
		return nil // Will be set by caller using uauthz.SetAllowedMsgTypes(c.CustomMessageTypes)
	case "default", "":
		// Set to default Cosmos SDK message types (or keep default)
		return nil // Will be set by caller using uauthz.UseDefaultMsgTypes()
	default:
		return fmt.Errorf("invalid message type category: %s (must be 'default', 'universal-validator', or 'custom')", c.MessageTypeCategory)
	}
}

// GetEffectiveMessageTypeCategory returns the effective message type category, defaulting to "default" if empty
func (c *Config) GetEffectiveMessageTypeCategory() string {
	if c.MessageTypeCategory == "" {
		return "default"
	}
	return c.MessageTypeCategory
}
