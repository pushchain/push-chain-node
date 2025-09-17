package bootstrap

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/files"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/statesync"
)

type Options struct {
    HomeDir       string
    ChainID       string
    Moniker       string
    Denom         string // e.g., upc
    GenesisDomain string // e.g., rpc-testnet-donut-node1.push.org
    BinPath       string // pchaind path
    SnapshotRPCPrimary   string // e.g., https://rpc-testnet-donut-node2.push.org
    SnapshotRPCSecondary string // optional; if empty uses primary
}

type Service interface { Init(ctx context.Context, opts Options) error }

// HTTPDoer matches http.Client's Do.
type HTTPDoer interface { Do(*http.Request) (*http.Response, error) }

// Runner runs commands; used for pchaind calls in init flow.
type Runner interface { Run(ctx context.Context, name string, args ...string) error }

type svc struct {
    http HTTPDoer
    run  Runner
    stp  statesync.Provider
}

// New builds a default service with real http client and runner.
func New() Service {
    return &svc{http: &http.Client{Timeout: 5 * time.Second}, run: defaultRunner{}, stp: statesync.New()}
}

// NewWith allows injecting http client, runner, and statesync provider for testing.
func NewWith(h HTTPDoer, r Runner, p statesync.Provider) Service {
    if h == nil { h = &http.Client{Timeout: 5 * time.Second} }
    if r == nil { r = defaultRunner{} }
    if p == nil { p = statesync.New() }
    return &svc{http: h, run: r, stp: p}
}

type defaultRunner struct{}
func (defaultRunner) Run(ctx context.Context, name string, args ...string) error {
    cmd := exec.CommandContext(ctx, name, args...)
    cmd.Stdout = io.Discard
    cmd.Stderr = io.Discard
    return cmd.Run()
}

func (s *svc) Init(ctx context.Context, opts Options) error {
    if opts.HomeDir == "" || opts.ChainID == "" {
        return errors.New("HomeDir and ChainID required")
    }
    if opts.Moniker == "" { opts.Moniker = "push-validator" }
    if opts.Denom == "" { opts.Denom = "upc" }
    if opts.BinPath == "" { opts.BinPath = "pchaind" }
    if opts.GenesisDomain == "" { return errors.New("GenesisDomain required") }

    // Ensure base dirs
    if err := os.MkdirAll(filepath.Join(opts.HomeDir, "config"), 0o755); err != nil { return err }
    if err := os.MkdirAll(filepath.Join(opts.HomeDir, "logs"), 0o755); err != nil { return err }

    // Run `pchaind init` if config is missing
    cfgPath := filepath.Join(opts.HomeDir, "config", "config.toml")
    if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
        if err := s.run.Run(ctx, opts.BinPath, "init", opts.Moniker, "--chain-id", opts.ChainID, "--default-denom", opts.Denom, "--home", opts.HomeDir, "--overwrite"); err != nil {
            return fmt.Errorf("pchaind init: %w", err)
        }
        // In test environments where the runner is a noop, ensure the file exists
        if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
            if mkerr := os.MkdirAll(filepath.Dir(cfgPath), 0o755); mkerr == nil {
                _ = os.WriteFile(cfgPath, []byte(""), 0o644)
            }
        }
    }

    // Fetch genesis from remote
    base := baseURL(opts.GenesisDomain)
    genesisURL := base + "/genesis"
    gen, err := s.getGenesis(ctx, genesisURL)
    if err != nil {
        return fmt.Errorf("fetch genesis: %w", err)
    }
    genPath := filepath.Join(opts.HomeDir, "config", "genesis.json")
    if err := os.WriteFile(genPath, gen, 0o644); err != nil { return err }

    // Build peers from net_info; fallback to known RPC nodes
    peers := s.peersFromNetInfo(ctx, base+"/net_info")
    if len(peers) == 0 {
        peers = s.fallbackPeers(ctx, base)
    }
    // Configure peers via config store
    cfgs := files.New(opts.HomeDir)
    if len(peers) > 0 { if err := cfgs.SetPersistentPeers(peers); err != nil { return err } }

    // Write priv_validator_state.json if missing
    pvs := filepath.Join(opts.HomeDir, "data", "priv_validator_state.json")
    if _, err := os.Stat(pvs); os.IsNotExist(err) {
        if err := os.MkdirAll(filepath.Dir(pvs), 0o755); err != nil { return err }
        if err := os.WriteFile(pvs, []byte("{\n  \"height\": \"0\",\n  \"round\": 0,\n  \"step\": 0\n}\n"), 0o644); err != nil { return err }
    }

    // Configure state sync parameters using snapshot RPC
    snapPrimary := opts.SnapshotRPCPrimary
    if snapPrimary == "" { snapPrimary = "https://rpc-testnet-donut-node2.push.org" }
    snapSecondary := opts.SnapshotRPCSecondary
    if snapSecondary == "" { snapSecondary = snapPrimary }
    tp, err := s.stp.ComputeTrust(ctx, snapPrimary)
    if err != nil {
        // Fallback to secondary if different
        if snapSecondary != "" && snapSecondary != snapPrimary {
            if tp2, err2 := s.stp.ComputeTrust(ctx, snapSecondary); err2 == nil {
                tp = tp2
            } else {
                return fmt.Errorf("compute trust params: %v; fallback failed: %v", err, err2)
            }
        } else {
            return fmt.Errorf("compute trust params: %w", err)
        }
    }
    // Build and filter RPC servers to those that accept JSON-RPC POST (light client requirement)
    candidates := []string{hostToStateSyncURL(snapPrimary)}
    if snapSecondary != "" { candidates = append(candidates, hostToStateSyncURL(snapSecondary)) }
    rpcServers := s.pickWorkingRPCs(ctx, candidates)
    if len(rpcServers) == 0 {
        return fmt.Errorf("no working RPC servers for state sync (JSON-RPC POST failed)")
    }
    // Ensure we provide two entries (primary + witness). Duplicate if only one works.
    if len(rpcServers) == 1 { rpcServers = append(rpcServers, rpcServers[0]) }
    if _, err := cfgs.Backup(); err == nil { /* best-effort */ }
    if err := cfgs.EnableStateSync(files.StateSyncParams{
        TrustHeight: tp.Height,
        TrustHash:   tp.Hash,
        RPCServers:  rpcServers,
        TrustPeriod: "336h0m0s",
    }); err != nil {
        return err
    }

    // Clear data for state sync
    _ = s.run.Run(ctx, opts.BinPath, "tendermint", "unsafe-reset-all", "--home", opts.HomeDir, "--keep-addr-book")
    // Mark initial state sync flag
    _ = os.WriteFile(filepath.Join(opts.HomeDir, ".initial_state_sync"), []byte(time.Now().Format(time.RFC3339)), 0o644)

    return nil
}

// ---- helpers ----

func (s *svc) getGenesis(ctx context.Context, url string) ([]byte, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    resp, err := s.http.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    if resp.StatusCode != 200 { return nil, fmt.Errorf("status %d", resp.StatusCode) }
    var payload struct{ Result struct{ Genesis json.RawMessage `json:"genesis"` } `json:"result"` }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { return nil, err }
    if len(payload.Result.Genesis) == 0 { return nil, errors.New("empty genesis") }
    return payload.Result.Genesis, nil
}

func (s *svc) peersFromNetInfo(ctx context.Context, url string) []string {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    resp, err := s.http.Do(req)
    if err != nil || resp.StatusCode != 200 { if resp != nil { resp.Body.Close() }; return nil }
    defer resp.Body.Close()
    var payload struct {
        Result struct {
            Peers []struct {
                NodeInfo struct {
                    ID         string `json:"id"`
                    ListenAddr string `json:"listen_addr"`
                } `json:"node_info"`
                RemoteIP string `json:"remote_ip"`
            } `json:"peers"`
        } `json:"result"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { return nil }
    out := make([]string, 0, len(payload.Result.Peers))
    for _, p := range payload.Result.Peers {
        if strings.Contains(p.NodeInfo.ListenAddr, "0.0.0.0") { continue }
        if p.NodeInfo.ID == "" || p.RemoteIP == "" { continue }
        out = append(out, fmt.Sprintf("%s@%s:26656", p.NodeInfo.ID, p.RemoteIP))
        if len(out) >= 4 { break }
    }
    return out
}

func (s *svc) fallbackPeers(ctx context.Context, base string) []string {
    fetchID := func(statusURL string) string {
        req, _ := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
        resp, err := s.http.Do(req)
        if err != nil || resp.StatusCode != 200 { if resp != nil { resp.Body.Close() }; return "" }
        defer resp.Body.Close()
        var payload struct{ Result struct{ NodeInfo struct{ ID string `json:"id"` } `json:"node_info"` } `json:"result"` }
        if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { return "" }
        return payload.Result.NodeInfo.ID
    }
    var out []string
    // When base is a domain-like string, use defaults; if it is a full URL, reuse host
    if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
        u, err := url.Parse(base)
        if err == nil {
            host := u.Host
            if id := fetchID(base + "/status"); id != "" { out = append(out, fmt.Sprintf("%s@%s:26656", id, host)) }
            return out
        }
    }
    if id := fetchID("https://rpc-testnet-donut-node1.push.org/status"); id != "" { out = append(out, fmt.Sprintf("%s@rpc-testnet-donut-node1.push.org:26656", id)) }
    if id := fetchID("https://rpc-testnet-donut-node2.push.org/status"); id != "" { out = append(out, fmt.Sprintf("%s@rpc-testnet-donut-node2.push.org:26656", id)) }
    return out
}

func hostToStateSyncURL(rpc string) string {
    // Convert base https://host[:port] to https://host:443 for state sync
    rpc = strings.TrimRight(rpc, "/")
    if strings.HasPrefix(rpc, "http://") {
        h := strings.TrimPrefix(rpc, "http://")
        if strings.Contains(h, ":") { return "http://" + h }
        return "http://" + h + ":80"
    }
    if strings.HasPrefix(rpc, "https://") {
        h := strings.TrimPrefix(rpc, "https://")
        if strings.Contains(h, ":") { return "https://" + h }
        return "https://" + h + ":443"
    }
    // default to https
    if strings.Contains(rpc, ":") { return "https://" + rpc }
    return "https://" + rpc + ":443"
}

func baseURL(genesisDomain string) string {
    d := strings.TrimSpace(genesisDomain)
    if strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://") { return d }
    if d == "" { return "https://rpc-testnet-donut-node1.push.org" }
    return "https://" + d
}

// pickWorkingRPCs returns a subset of URLs that respond to a JSON-RPC POST (method=status) within timeout.
func (s *svc) pickWorkingRPCs(ctx context.Context, urls []string) []string {
    type req struct {
        JSONRPC string `json:"jsonrpc"`
        Method  string `json:"method"`
        ID      int    `json:"id"`
    }
    httpc := &http.Client{Timeout: 6 * time.Second}
    var out []string
    for _, u := range urls {
        // Support bare hosts (e.g., from local tests) by defaulting to http://
        if !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
            u = "http://" + strings.TrimRight(u, "/")
        }
        // Make a shallow copy of ctx with short timeout per probe
        // attempt twice with short backoff
        var ok bool
        for attempt := 0; attempt < 2 && !ok; attempt++ {
            cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
            body, _ := json.Marshal(req{JSONRPC: "2.0", Method: "status", ID: 1})
            rq, _ := http.NewRequestWithContext(cctx, http.MethodPost, u, strings.NewReader(string(body)))
            rq.Header.Set("Content-Type", "application/json")
            resp, err := httpc.Do(rq)
            cancel()
            if err == nil && resp != nil && resp.StatusCode == 200 {
                _ = resp.Body.Close()
                ok = true
                break
            }
            if resp != nil { _ = resp.Body.Close() }
            time.Sleep(300 * time.Millisecond)
        }
        if ok { out = append(out, u) }
    }
    return out
}
