package validator

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "time"
)

type Options struct {
    BinPath       string
    HomeDir       string
    ChainID       string
    Keyring       string
    GenesisDomain string // e.g., rpc-testnet-donut-node1.push.org
    Denom         string // e.g., upc
}

func NewWith(opts Options) Service { return &svc{opts: opts} }

type svc struct { opts Options }

func (s *svc) EnsureKey(ctx context.Context, name string) (string, error) {
    if name == "" { return "", errors.New("key name required") }
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    show := exec.CommandContext(ctx, s.opts.BinPath, "keys", "show", name, "-a", "--keyring-backend", s.opts.Keyring, "--home", s.opts.HomeDir)
    out, err := show.Output()
    if err == nil { return strings.TrimSpace(string(out)), nil }
    // Try to add key - show interactive output to user
    fmt.Println("Creating validator key...")
    add := exec.CommandContext(ctx, s.opts.BinPath, "keys", "add", name, "--keyring-backend", s.opts.Keyring, "--algo", "eth_secp256k1", "--home", s.opts.HomeDir)
    add.Stdout = os.Stdout
    add.Stderr = os.Stderr
    add.Stdin = os.Stdin
    if err := add.Run(); err != nil {
        return "", fmt.Errorf("keys add: %w", err)
    }
    out2, err := exec.CommandContext(ctx, s.opts.BinPath, "keys", "show", name, "-a", "--keyring-backend", s.opts.Keyring, "--home", s.opts.HomeDir).Output()
    if err != nil { return "", fmt.Errorf("keys show: %w", err) }
    return strings.TrimSpace(string(out2)), nil
}

func (s *svc) IsValidator(ctx context.Context, addr string) (bool, error) {
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    // Compare local consensus pubkey with remote validators
    showVal := exec.CommandContext(ctx, s.opts.BinPath, "tendermint", "show-validator", "--home", s.opts.HomeDir)
    b, err := showVal.Output()
    if err != nil { return false, fmt.Errorf("show-validator: %w", err) }
    var pub struct{ Key string `json:"key"` }
    if err := json.Unmarshal(b, &pub); err != nil { return false, err }
    if pub.Key == "" { return false, errors.New("empty consensus pubkey") }
    // Query validators from remote
    remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
    q := exec.CommandContext(ctx, s.opts.BinPath, "query", "staking", "validators", "--node", remote, "-o", "json")
    vb, err := q.Output()
    if err != nil { return false, fmt.Errorf("query validators: %w", err) }
    var payload struct{ Validators []struct{ ConsensusPubkey struct{ Key string `json:"key"` } `json:"consensus_pubkey"` } `json:"validators"` }
    if err := json.Unmarshal(vb, &payload); err != nil { return false, err }
    for _, v := range payload.Validators {
        if strings.EqualFold(v.ConsensusPubkey.Key, pub.Key) { return true, nil }
    }
    return false, nil
}

func (s *svc) Balance(ctx context.Context, addr string) (string, error) {
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
    q := exec.CommandContext(ctx, s.opts.BinPath, "query", "bank", "balances", addr, "--node", remote, "-o", "json")
    out, err := q.Output()
    if err != nil { return "0", fmt.Errorf("query balance: %w", err) }
    var payload struct{ Balances []struct{ Denom, Amount string } `json:"balances"` }
    if err := json.Unmarshal(out, &payload); err != nil { return "0", err }
    for _, c := range payload.Balances { if c.Denom == s.opts.Denom { return c.Amount, nil } }
    return "0", nil
}

func (s *svc) Register(ctx context.Context, args RegisterArgs) (string, error) {
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    // Prepare validator JSON - use a separate timeout for this command
    showCtx, showCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer showCancel()
    pubJSON, err := exec.CommandContext(showCtx, s.opts.BinPath, "tendermint", "show-validator", "--home", s.opts.HomeDir).Output()
    if err != nil { return "", fmt.Errorf("show-validator: %w", err) }
    tmp, err := os.CreateTemp("", "validator-*.json")
    if err != nil { return "", err }
    defer os.Remove(tmp.Name())
    val := map[string]any{
        "pubkey": json.RawMessage(strings.TrimSpace(string(pubJSON))),
        "amount": fmt.Sprintf("%s%s", args.Amount, s.opts.Denom),
        "moniker": args.Moniker,
        "identity": "",
        "website": "",
        "security": "",
        "details": "Push Chain Validator",
        "commission-rate": valueOr(args.CommissionRate, "0.10"),
        "commission-max-rate": "0.20",
        "commission-max-change-rate": "0.01",
        "min-self-delegation": valueOr(args.MinSelfDelegation, "1"),
    }
    enc := json.NewEncoder(tmp)
    enc.SetEscapeHTML(false)
    if err := enc.Encode(val); err != nil { return "", err }
    _ = tmp.Close()

    // Submit TX
    remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
    ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctxTimeout, s.opts.BinPath, "tx", "staking", "create-validator", tmp.Name(),
        "--from", args.KeyName,
        "--chain-id", s.opts.ChainID,
        "--keyring-backend", s.opts.Keyring,
        "--home", s.opts.HomeDir,
        "--node", remote,
        "--gas=auto", "--gas-adjustment=1.3", fmt.Sprintf("--gas-prices=1000000000%s", s.opts.Denom),
        "--yes",
    )
    out, err := cmd.CombinedOutput()
    if err != nil {
        // Try to extract a clean reason
        msg := extractErrorLine(string(out))
        if msg == "" { msg = err.Error() }
        return "", errors.New(msg)
    }
    // Find txhash:
    lines := strings.Split(string(out), "\n")
    for _, ln := range lines {
        if strings.Contains(ln, "txhash:") {
            parts := strings.SplitN(ln, "txhash:", 2)
            if len(parts) == 2 { return strings.TrimSpace(parts[1]), nil }
        }
    }
    return "", errors.New("transaction submitted; txhash not found in output")
}

func extractErrorLine(s string) string {
    for _, l := range strings.Split(s, "\n") {
        if strings.Contains(l, "rpc error:") || strings.Contains(l, "failed to execute message") || strings.Contains(l, "insufficient") || strings.Contains(l, "unauthorized") {
            return l
        }
    }
    return ""
}

func valueOr(v, d string) string { if strings.TrimSpace(v) == "" { return d }; return v }
