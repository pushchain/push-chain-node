package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "math/big"
    "os"
    "os/signal"
    "context"
    "os/exec"
    "flag"
    "sort"
    "strconv"
    "strings"
    "syscall"
    "time"
    "net/url"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/bootstrap"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/admin"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
    syncmon "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/sync"
)

// NOTE: We will migrate this to Cobra once dependencies are wired.
// For now, keep a tiny stdlib router so the binary can be built without network.

func usage() {
    fmt.Println("Push Validator Manager (Go) — CLI")
    fmt.Println()
    fmt.Println("Usage:")
    fmt.Println("  push-validator-manager <command> [flags]")
    fmt.Println()
    fmt.Println("Commands:")
    fmt.Println("  status            Show node status")
    fmt.Println("  init              Initialize local node home")
    fmt.Println("  start             Start node (expects initialized home)")
    fmt.Println("  stop              Stop node")
    fmt.Println("  restart           Restart node")
    fmt.Println("  logs              Tail node logs")
    fmt.Println("  sync              Monitor sync progress (WebSocket-only)")
    fmt.Println("  reset             Reset chain data (keeps addr book)")
    fmt.Println("  backup            Backup config and validator state")
    fmt.Println("  validators        List validators (summary)")
    fmt.Println("  balance [addr]    Show balance for address (upc)")
    fmt.Println("  register-validator  Register this node as a validator")
    fmt.Println()
    fmt.Println("Flags (examples):")
    fmt.Println("  init:   --moniker --home --chain-id --genesis-domain --snapshot-rpc --bin")
    fmt.Println("  start:  --home --bin")
    fmt.Println("  status: --json")
    fmt.Println("  sync:   --compact --window --rpc --remote")
    fmt.Println("  register-validator: --moniker --key-name --amount")
}

func main() { Execute() }

func findPchaind() string {
    if v := os.Getenv("PCHAIND"); v != "" { return v }
    if v := os.Getenv("PCHAIN_BIN"); v != "" { return v }
    return "pchaind"
}

func handleInit(cfg config.Config) {
    fs := flag.NewFlagSet("init", flag.ExitOnError)
    moniker := fs.String("moniker", getenvDefault("MONIKER", "push-validator"), "Validator moniker")
    home := fs.String("home", cfg.HomeDir, "Node home directory")
    chainID := fs.String("chain-id", cfg.ChainID, "Chain ID")
    genesisDomain := fs.String("genesis-domain", cfg.GenesisDomain, "Genesis RPC domain or URL")
    snap := fs.String("snapshot-rpc", cfg.SnapshotRPC, "Snapshot RPC base URL")
    bin := fs.String("bin", findPchaind(), "Path to pchaind binary")
    _ = fs.Parse(os.Args[2:])
    svc := bootstrap.New()
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()
    err := svc.Init(ctx, bootstrap.Options{
        HomeDir: *home,
        ChainID: *chainID,
        Moniker: *moniker,
        Denom: "upc",
        GenesisDomain: *genesisDomain,
        BinPath: *bin,
        SnapshotRPCPrimary: *snap,
        SnapshotRPCSecondary: *snap,
    })
    if err != nil {
        fmt.Printf("init error: %v\n", err)
        os.Exit(1)
    }
    fmt.Println("initialized node home")
}

func handleStart(cfg config.Config, sup process.Supervisor) {
    fs := flag.NewFlagSet("start", flag.ExitOnError)
    home := fs.String("home", cfg.HomeDir, "Node home directory")
    bin := fs.String("bin", findPchaind(), "Path to pchaind binary")
    _ = fs.Parse(os.Args[2:])
    pid, err := sup.Start(process.StartOpts{ HomeDir: *home, Moniker: os.Getenv("MONIKER"), BinPath: *bin })
    if err != nil {
        fmt.Printf("start error: %v\n", err)
        // Helpful hint if config is missing
        if _, statErr := os.Stat(*home + "/config/config.toml"); os.IsNotExist(statErr) {
            fmt.Println("hint: initialize first or use 'init-and-start' when available")
        }
        os.Exit(1)
    }
    fmt.Printf("node started (pid %d)\n", pid)
}

func handleStop(sup process.Supervisor) {
    if err := sup.Stop(); err != nil {
        fmt.Printf("stop error: %v\n", err)
        os.Exit(1)
    }
    fmt.Println("node stopped")
}

func handleRestart(cfg config.Config, sup process.Supervisor) {
    fs := flag.NewFlagSet("restart", flag.ExitOnError)
    home := fs.String("home", cfg.HomeDir, "Node home directory")
    bin := fs.String("bin", findPchaind(), "Path to pchaind binary")
    _ = fs.Parse(os.Args[2:])
    pid, err := sup.Restart(process.StartOpts{ HomeDir: *home, Moniker: os.Getenv("MONIKER"), BinPath: *bin })
    if err != nil {
        fmt.Printf("restart error: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("node restarted (pid %d)\n", pid)
}

func handleStatus(cfg config.Config, sup process.Supervisor) {
    fs := flag.NewFlagSet("status", flag.ExitOnError)
    jsonOut := fs.Bool("json", false, "Output JSON")
    _ = fs.Parse(os.Args[2:])

    result := struct {
        Running     bool   `json:"running"`
        PID         int    `json:"pid,omitempty"`
        RPCListening bool  `json:"rpc_listening"`
        CatchingUp  bool   `json:"catching_up"`
        Height      int64  `json:"height"`
        Error       string `json:"error,omitempty"`
    }{}

    result.Running = sup.IsRunning()
    if pid, ok := sup.PID(); ok { result.PID = pid }

    // RPC status
    rpc := strings.TrimRight(cfg.RPCLocal, "/")
    if rpc == "" { rpc = "http://127.0.0.1:26657" }
    result.RPCListening = process.IsRPCListening("127.0.0.1:26657", 500*time.Millisecond)
    if result.RPCListening {
        cli := node.New(rpc)
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        st, err := cli.Status(ctx)
        if err == nil {
            result.CatchingUp = st.CatchingUp
            result.Height = st.Height
        } else {
            result.Error = fmt.Sprintf("RPC status error: %v", err)
        }
    }

    if *jsonOut {
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        _ = enc.Encode(result)
        return
    }

    if result.Running {
        if result.PID != 0 {
            fmt.Printf("Node process: running (pid %d)\n", result.PID)
        } else {
            fmt.Println("Node process: running")
        }
    } else {
        fmt.Println("Node process: stopped")
    }
    if !result.RPCListening {
        fmt.Println("RPC: not listening on 127.0.0.1:26657")
        return
    }
    if result.Error != "" {
        fmt.Println(result.Error)
        return
    }
    fmt.Printf("RPC: catching_up=%v height=%d\n", result.CatchingUp, result.Height)
}

func handleLogs(sup process.Supervisor) {
    lp := sup.LogPath()
    if lp == "" {
        fmt.Println("no log path configured")
        os.Exit(1)
    }
    if _, err := os.Stat(lp); err != nil {
        fmt.Printf("log file not found: %s\n", lp)
        os.Exit(1)
    }
    fmt.Printf("Tailing %s (Ctrl+C to stop)\n", lp)
    stop := make(chan struct{})
    // Stop on SIGINT/SIGTERM
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    go func() { <-sigs; close(stop) }()
    if err := process.TailFollow(lp, os.Stdout, stop); err != nil {
        fmt.Printf("tail error: %v\n", err)
        os.Exit(1)
    }
}

func handleSync(cfg config.Config) {
    fs := flag.NewFlagSet("sync", flag.ExitOnError)
    compact := fs.Bool("compact", false, "Compact output")
    window := fs.Int("window", 30, "Moving average window (headers)")
    localRPC := fs.String("rpc", cfg.RPCLocal, "Local RPC base (http[s]://host:port)")
    remote := fs.String("remote", "https://"+strings.TrimSuffix(cfg.GenesisDomain, "/")+":443", "Remote RPC base")
    _ = fs.Parse(os.Args[2:])
    sup := process.New(cfg.HomeDir)
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()
    if err := syncmon.Run(ctx, syncmon.Options{
        LocalRPC: *localRPC,
        RemoteRPC: *remote,
        LogPath: sup.LogPath(),
        Window: *window,
        Compact: *compact,
        Out: os.Stdout,
    }); err != nil {
        fmt.Printf("sync error: %v\n", err)
        os.Exit(1)
    }
}

func movingRate(buf []struct{ h int64; t time.Time }) float64 {
    n := len(buf)
    if n < 2 { return 0 }
    dh := float64(buf[n-1].h - buf[0].h)
    dt := buf[n-1].t.Sub(buf[0].t).Seconds()
    if dt <= 0 { return 0 }
    return dh / dt
}

func handleReset(cfg config.Config, sup process.Supervisor) {
    // Stop first if running
    _ = sup.Stop()
    if err := admin.Reset(admin.ResetOptions{
        HomeDir: cfg.HomeDir,
        BinPath: findPchaind(),
        KeepAddrBook: true,
    }); err != nil {
        fmt.Printf("reset error: %v\n", err)
        os.Exit(1)
    }
    fmt.Println("chain data reset (addr book kept)")
}

func handleBackup(cfg config.Config) {
    path, err := admin.Backup(admin.BackupOptions{HomeDir: cfg.HomeDir})
    if err != nil {
        fmt.Printf("backup error: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("backup created: %s\n", path)
}

func handleValidators(cfg config.Config) {
    // Use pchaind query to fetch validators and print summary
    bin := findPchaind()
    remote := fmt.Sprintf("tcp://%s:26657", cfg.GenesisDomain)

    // Create context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    cmd := exec.CommandContext(ctx, bin, "query", "staking", "validators", "--node", remote, "-o", "json")

    // Capture output to process it
    output, err := cmd.Output()
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            fmt.Printf("validators error: timeout connecting to %s\n", cfg.GenesisDomain)
        } else {
            fmt.Printf("validators error: %v\n", err)
        }
        os.Exit(1)
    }

    // Parse JSON with proper nested structure
    var result struct {
        Validators []struct {
            Description struct {
                Moniker string `json:"moniker"`
                Details string `json:"details"`
            } `json:"description"`
            OperatorAddress string `json:"operator_address"`
            Status          string `json:"status"`
            Tokens          string `json:"tokens"`
            Commission      struct {
                CommissionRates struct {
                    Rate          string `json:"rate"`
                    MaxRate       string `json:"max_rate"`
                    MaxChangeRate string `json:"max_change_rate"`
                } `json:"commission_rates"`
            } `json:"commission"`
        } `json:"validators"`
    }

    if err := json.Unmarshal(output, &result); err != nil {
        // If parsing fails, just output the raw JSON
        fmt.Println(string(output))
        return
    }

    if len(result.Validators) == 0 {
        fmt.Println("No validators found or node not synced")
        return
    }

    // Create a slice for sorting with processed data
    type validatorDisplay struct {
        moniker       string
        status        string
        statusOrder   int
        tokensPC      float64
        commissionPct float64
    }

    validators := make([]validatorDisplay, 0, len(result.Validators))

    for _, v := range result.Validators {
        vd := validatorDisplay{}

        // Get moniker with fallback
        vd.moniker = v.Description.Moniker
        if vd.moniker == "" {
            vd.moniker = "unknown"
        }

        // Translate status and set order
        switch v.Status {
        case "BOND_STATUS_BONDED":
            vd.status = "BONDED"
            vd.statusOrder = 1
        case "BOND_STATUS_UNBONDING":
            vd.status = "UNBONDING"
            vd.statusOrder = 2
        case "BOND_STATUS_UNBONDED":
            vd.status = "UNBONDED"
            vd.statusOrder = 3
        default:
            vd.status = v.Status
            vd.statusOrder = 4
        }

        // Convert tokens to PC (divide by 1e18)
        if v.Tokens != "" && v.Tokens != "0" {
            if t, err := strconv.ParseFloat(v.Tokens, 64); err == nil {
                vd.tokensPC = t / 1e18
            }
        }

        // Convert commission rate to percentage
        if v.Commission.CommissionRates.Rate != "" {
            if c, err := strconv.ParseFloat(v.Commission.CommissionRates.Rate, 64); err == nil {
                vd.commissionPct = c * 100
            }
        }

        validators = append(validators, vd)
    }

    // Sort validators: first by status order, then by stake amount (descending)
    sort.Slice(validators, func(i, j int) bool {
        if validators[i].statusOrder != validators[j].statusOrder {
            return validators[i].statusOrder < validators[j].statusOrder
        }
        // Within same status, sort by stake amount (higher first)
        return validators[i].tokensPC > validators[j].tokensPC
    })

    // Print header matching bash version
    fmt.Println("\n👥 Active Push Chain Validators")
    fmt.Println("═══════════════════════════════════════════════════════════════")

    // Display validators in table format matching bash version columns
    fmt.Printf("\n%-26s %-12s %12s %11s\n", "VALIDATOR", "STATUS", "STAKE (PC)", "COMMISSION")
    fmt.Println("─────────────────────────────────────────────────────────────────")

    for _, v := range validators {
        fmt.Printf("%-26s %-12s %12.1f %10.0f%%\n",
            truncate(v.moniker, 26),
            v.status,
            v.tokensPC,
            v.commissionPct)
    }

    fmt.Printf("\nTotal Validators: %d\n", len(result.Validators))
}

func truncate(s string, max int) string {
    if len(s) <= max { return s }
    return s[:max-3] + "..."
}

func handleBalance(cfg config.Config, args []string) {
    var addr string
    if len(args) > 0 { addr = args[0] }
    if addr == "" {
        // Try KEY_NAME env to resolve address
        key := os.Getenv("KEY_NAME")
        if key == "" { fmt.Println("usage: push-validator-manager balance <address> (or set KEY_NAME)"); os.Exit(1) }
        // Resolve via keys show
        out, err := exec.Command(findPchaind(), "keys", "show", key, "-a", "--keyring-backend", cfg.KeyringBackend, "--home", cfg.HomeDir).Output()
        if err != nil { fmt.Printf("resolve address error: %v\n", err); os.Exit(1) }
        addr = strings.TrimSpace(string(out))
    }
    v := validator.NewWith(validator.Options{
        BinPath: findPchaind(), HomeDir: cfg.HomeDir, ChainID: cfg.ChainID,
        Keyring: cfg.KeyringBackend, GenesisDomain: cfg.GenesisDomain, Denom: cfg.Denom,
    })
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    bal, err := v.Balance(ctx, addr)
    if err != nil { fmt.Printf("balance error: %v\n", err); os.Exit(1) }
    fmt.Printf("%s %s\n", bal, cfg.Denom)
}

func handleRegisterValidator(cfg config.Config) {
    // Flags
    fs := flag.NewFlagSet("register-validator", flag.ExitOnError)
    moniker := fs.String("moniker", getenvDefault("MONIKER", "push-validator"), "Validator moniker")
    keyName := fs.String("key-name", getenvDefault("KEY_NAME", "validator-key"), "Key name")
    amount := fs.String("amount", getenvDefault("STAKE_AMOUNT", "1500000000000000000"), "Stake amount in base denom")
    _ = fs.Parse(os.Args[2:])
    _ = amount // Using fixed stakeAmount constant instead
    // Preflight sync check
    local := strings.TrimRight(cfg.RPCLocal, "/")
    if local == "" { local = "http://127.0.0.1:26657" }
    remoteHTTP := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
    cliLocal := node.New(local)
    cliRemote := node.New(remoteHTTP)
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    stLocal, err1 := cliLocal.Status(ctx)
    _, err2 := cliRemote.RemoteStatus(ctx, remoteHTTP)
    if err1 == nil && err2 == nil {
        // Only allow registration if node is fully synced (catching_up = false)
        if stLocal.CatchingUp {
            fmt.Printf("node is still syncing. Run 'push-validator-manager sync' first.\n")
            os.Exit(1)
        }
    }

    v := validator.NewWith(validator.Options{
        BinPath: findPchaind(), HomeDir: cfg.HomeDir, ChainID: cfg.ChainID,
        Keyring: cfg.KeyringBackend, GenesisDomain: cfg.GenesisDomain, Denom: cfg.Denom,
    })
    // Ensure key
    ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel2()
    addr, err := v.EnsureKey(ctx2, *keyName)
    if err != nil { fmt.Printf("key error: %v\n", err); os.Exit(1) }
    fmt.Printf("✅ Key '%s' ready\n", *keyName)

    // Check existing validator (consensus pubkey)
    isVal, err := v.IsValidator(ctx2, addr)
    if err == nil && isVal {
        fmt.Println("❌ this node is already registered as a validator")
        return
    }

    // Get EVM address for faucet
    cmd := exec.Command(findPchaind(), "debug", "addr", addr, "--home", cfg.HomeDir)
    debugOut, _ := cmd.Output()
    evmAddr := ""
    for _, line := range strings.Split(string(debugOut), "\n") {
        if strings.Contains(line, "hex") {
            parts := strings.Fields(line)
            if len(parts) >= 3 { evmAddr = "0x" + parts[2] }
        }
    }

    // Check balance and prompt for funding if needed
    const requiredBalance = "1600000000000000000" // 1.6 PC (1.5 PC stake + 0.1 PC gas)
    const stakeAmount = "1500000000000000000"     // 1.5 PC

    maxRetries := 10
    retryCount := 0

    for {
        // Create a new context with timeout for each balance check
        balCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        bal, err := v.Balance(balCtx, addr)
        cancel()

        if err != nil {
            fmt.Printf("⚠️ Balance check failed: %v\n", err)
            retryCount++
            if retryCount >= maxRetries {
                fmt.Printf("Failed to check balance after %d attempts\n", maxRetries)
                os.Exit(1)
            }
            fmt.Printf("Retrying... (%d/%d)\n", retryCount, maxRetries)
            time.Sleep(2 * time.Second)
            continue
        }

        retryCount = 0 // Reset retry count on successful balance check

        balInt := new(big.Int)
        balInt.SetString(bal, 10)
        reqInt := new(big.Int)
        reqInt.SetString(requiredBalance, 10)

        if balInt.Cmp(reqInt) >= 0 {
            fmt.Println("✅ Sufficient balance")
            break
        }

        // Show balance in PC
        pcAmount := "0.000000"
        if bal != "0" {
            balFloat, _ := new(big.Float).SetString(bal)
            divisor := new(big.Float).SetFloat64(1e18)
            result := new(big.Float).Quo(balFloat, divisor)
            pcAmount = fmt.Sprintf("%.6f", result)
        }

        fmt.Printf("Balance: %s PC (need 1.6 PC)\n", pcAmount)
        fmt.Printf("Faucet: https://faucet.push.org | EVM: %s\n", evmAddr)
        fmt.Print("Press ENTER after funding...")

        reader := bufio.NewReader(os.Stdin)
        _, _ = reader.ReadString('\n')
    }

    // Submit
    txHash, err := v.Register(ctx2, validator.RegisterArgs{
        Moniker: *moniker, Amount: stakeAmount, KeyName: *keyName,
        CommissionRate: "0.10", MinSelfDelegation: "1",
    })
    if err != nil { fmt.Printf("register error: %v\n", err); os.Exit(1) }
    fmt.Printf("validator registered. txhash: %s\n", txHash)
}

func getenvDefault(k, d string) string { if v := os.Getenv(k); v != "" { return v }; return d }

// --- shared status helpers for Cobra ---

type statusResult struct {
    Running      bool   `json:"running"`
    PID          int    `json:"pid,omitempty"`
    RPCListening bool   `json:"rpc_listening"`
    CatchingUp   bool   `json:"catching_up"`
    Height       int64  `json:"height"`
    Error        string `json:"error,omitempty"`
}

func computeStatus(cfg config.Config, sup process.Supervisor) statusResult {
    res := statusResult{}
    res.Running = sup.IsRunning()
    if pid, ok := sup.PID(); ok { res.PID = pid }

    // RPC status
    rpc := strings.TrimRight(cfg.RPCLocal, "/")
    if rpc == "" { rpc = "http://127.0.0.1:26657" }
    hostport := "127.0.0.1:26657"
    if u, err := url.Parse(rpc); err == nil && u.Host != "" { hostport = u.Host }
    res.RPCListening = process.IsRPCListening(hostport, 500*time.Millisecond)
    if res.RPCListening {
        cli := node.New(rpc)
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        st, err := cli.Status(ctx)
        if err == nil {
            res.CatchingUp = st.CatchingUp
            res.Height = st.Height
        } else {
            res.Error = fmt.Sprintf("RPC status error: %v", err)
        }
    }
    return res
}

func printStatusText(result statusResult) {
    if result.Running {
        if result.PID != 0 {
            fmt.Printf("Node process: running (pid %d)\n", result.PID)
        } else {
            fmt.Println("Node process: running")
        }
    } else {
        fmt.Println("Node process: stopped")
    }
    if !result.RPCListening {
        fmt.Println("RPC: not listening on 127.0.0.1:26657")
        return
    }
    if result.Error != "" {
        fmt.Println(result.Error)
        return
    }
    fmt.Printf("RPC: catching_up=%v height=%d\n", result.CatchingUp, result.Height)
}
