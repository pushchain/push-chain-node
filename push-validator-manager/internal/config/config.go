package config

import (
    "os"
    "path/filepath"
)

// Config holds user/system configuration for the manager.
// File-backed configuration and env/flag merging will be added.
type Config struct {
    ChainID        string
    HomeDir        string
    GenesisDomain  string
    KeyringBackend string
    SnapshotRPC    string
    RPCLocal       string // e.g., http://127.0.0.1:26657
    Denom          string // staking denom (e.g., upc)
}

// Defaults sets chain-specific defaults aligned with current scripts.
func Defaults() Config {
    home, _ := os.UserHomeDir()
    return Config{
        ChainID:        "push_42101-1",
        HomeDir:        filepath.Join(home, ".pchain"),
        GenesisDomain:  "donut.rpc.push.org",
        KeyringBackend: "test",
        SnapshotRPC:    "https://donut.rpc.push.org",
        RPCLocal:       "http://127.0.0.1:26657",
        Denom:          "upc",
    }
}

// Load returns default config with HOME_DIR override from environment.
// Use flags for other configuration options.
func Load() Config {
    cfg := Defaults()
    // Only support HOME_DIR env var (common pattern for XDG_* style overrides)
    if v := os.Getenv("HOME_DIR"); v != "" {
        cfg.HomeDir = v
    }
    return cfg
}
