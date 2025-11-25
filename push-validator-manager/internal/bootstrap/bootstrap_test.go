package bootstrap

import (
    "context"
    "encoding/json"
    "net"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager/internal/statesync"
)

// fakeRunner just records invocations without executing anything.
type fakeRunner struct{ calls [][]string }
func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
    call := append([]string{name}, args...)
    f.calls = append(f.calls, call)
    return nil
}

// fakeProvider returns static trust params.
type fakeProvider struct{}
func (fakeProvider) ComputeTrust(ctx context.Context, rpcURL string) (statesync.TrustParams, error) {
    return statesync.TrustParams{Height: 4000, Hash: "ABC123"}, nil
}

func TestBootstrap_Init_FullFlow(t *testing.T) {
    // Skip if sandbox disallows binding
    if ln, err := net.Listen("tcp", "127.0.0.1:0"); err != nil { t.Skip("binding disabled in sandbox") } else { ln.Close() }

    mux := http.NewServeMux()
    // JSON-RPC POST status handler at root for light client probes
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        if r.Method == http.MethodPost {
            _, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"node_info":{"id":"test"},"sync_info":{"latest_block_height":"5000","catching_up":true}}}`))
            return
        }
        http.NotFound(w, r)
    })
    mux.HandleFunc("/genesis", func(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{"result": map[string]any{"genesis": map[string]any{"chain_id": "push_42101-1"}}}
        _ = json.NewEncoder(w).Encode(resp)
    })
    mux.HandleFunc("/net_info", func(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{"result": map[string]any{"peers": []map[string]any{
            {"node_info": map[string]any{"id": "id1", "listen_addr": "tcp://0.0.0.0:26656"}, "remote_ip": "10.0.0.1"},
            {"node_info": map[string]any{"id": "id2", "listen_addr": "tcp://1.2.3.4:26656"}, "remote_ip": "1.2.3.4"},
        }}}
        _ = json.NewEncoder(w).Encode(resp)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    home := t.TempDir()
    r := &fakeRunner{}
    svc := NewWith(srv.Client(), r, fakeProvider{})
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err := svc.Init(ctx, Options{
        HomeDir: home,
        ChainID: "push_42101-1",
        Moniker: "testnode",
        GenesisDomain: srv.URL, // full URL supported
        BinPath: "pchaind",
        SnapshotRPCPrimary: srv.URL, // uses fake provider anyway
    })
    if err != nil { t.Fatalf("init error: %v", err) }

    // Verify files written
    if _, err := os.Stat(filepath.Join(home, "config", "genesis.json")); err != nil { t.Fatalf("missing genesis.json: %v", err) }
    b, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
    if err != nil { t.Fatalf("missing config.toml: %v", err) }
    s := string(b)
    if !containsAll(s, []string{"[p2p]", "persistent_peers", "addr_book_strict = false"}) { t.Fatalf("p2p peers not configured: %s", s) }
    if !containsAll(s, []string{"[statesync]", "enable = true", "trust_height = 4000", "trust_hash = \"ABC123\""}) {
        t.Fatalf("statesync not configured: %s", s)
    }
    // Verify runner was invoked for init and unsafe-reset-all
    if len(r.calls) == 0 { t.Fatalf("runner not called") }
}

func containsAll(s string, subs []string) bool {
    for _, sub := range subs { if !contains(s, sub) { return false } }
    return true
}
func contains(s, sub string) bool { return strings.Contains(s, sub) }
