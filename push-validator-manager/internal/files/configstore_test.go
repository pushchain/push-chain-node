package files

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestConfigStore_StateSyncAndPeers(t *testing.T) {
    dir := t.TempDir()
    // Seed minimal config.toml without sections
    cfgDir := filepath.Join(dir, "config")
    if err := os.MkdirAll(cfgDir, 0o755); err != nil { t.Fatal(err) }
    cfgPath := filepath.Join(cfgDir, "config.toml")
    if err := os.WriteFile(cfgPath, []byte(""), 0o644); err != nil { t.Fatal(err) }

    s := New(dir).(*store)

    // Set peers
    peers := []string{"id1@host1:26656", "id2@host2:26656"}
    if err := s.SetPersistentPeers(peers); err != nil { t.Fatal(err) }
    b, _ := os.ReadFile(cfgPath)
    got := string(b)
    if !strings.Contains(got, "[p2p]") { t.Fatalf("missing [p2p] section: %s", got) }
    if !strings.Contains(got, "persistent_peers = \"id1@host1:26656,id2@host2:26656\"") {
        t.Fatalf("peers not set correctly: %s", got)
    }
    if !strings.Contains(got, "addr_book_strict = false") { t.Fatalf("addr_book_strict not set: %s", got) }

    // Enable state sync
    if err := s.EnableStateSync(StateSyncParams{TrustHeight: 1234, TrustHash: "ABCDEF", RPCServers: []string{"https://a:443","https://b:443"}}); err != nil { t.Fatal(err) }
    b, _ = os.ReadFile(cfgPath)
    got = string(b)
    if !strings.Contains(got, "[statesync]") { t.Fatalf("missing [statesync] section: %s", got) }
    if !strings.Contains(got, "enable = true") { t.Fatalf("statesync enable missing: %s", got) }
    if !strings.Contains(got, "trust_height = 1234") { t.Fatalf("trust_height missing: %s", got) }
    if !strings.Contains(got, "trust_hash = \"ABCDEF\"") { t.Fatalf("trust_hash missing: %s", got) }
    if !strings.Contains(got, "rpc_servers = \"https://a:443,https://b:443\"") { t.Fatalf("rpc_servers missing: %s", got) }

    // Disable state sync
    if err := s.DisableStateSync(); err != nil { t.Fatal(err) }
    b, _ = os.ReadFile(cfgPath)
    if !strings.Contains(string(b), "enable = false") { t.Fatalf("disable did not set enable=false: %s", string(b)) }

    // Backup
    p, err := s.Backup()
    if err != nil { t.Fatal(err) }
    if _, err := os.Stat(p); err != nil { t.Fatalf("backup not created: %v", err) }
}

