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
    RPCLocal       string // e.g., http://localhost:26657
    Denom          string // staking denom (e.g., upc)
}

// Defaults sets chain-specific defaults aligned with current scripts.
func Defaults() Config {
    home, _ := os.UserHomeDir()
    return Config{
        ChainID:        "push_42101-1",
        HomeDir:        filepath.Join(home, ".pchain"),
        GenesisDomain:  "rpc-testnet-donut-node1.push.org",
        KeyringBackend: "test",
        SnapshotRPC:    "https://rpc-testnet-donut-node2.push.org",
        RPCLocal:       "http://localhost:26657",
        Denom:          "upc",
    }
}

// Load merges defaults with environment variables. File/flags later.
func Load() Config {
    cfg := Defaults()
    if v := os.Getenv("CHAIN_ID"); v != "" { cfg.ChainID = v }
    if v := os.Getenv("HOME_DIR"); v != "" { cfg.HomeDir = v }
    if v := os.Getenv("GENESIS_DOMAIN"); v != "" { cfg.GenesisDomain = v }
    if v := os.Getenv("KEYRING_BACKEND"); v != "" { cfg.KeyringBackend = v }
    if v := os.Getenv("SNAPSHOT_RPC"); v != "" { cfg.SnapshotRPC = v }
    if v := os.Getenv("RPC_LOCAL"); v != "" { cfg.RPCLocal = v }
    if v := os.Getenv("DENOM"); v != "" { cfg.Denom = v }
    return cfg
}
