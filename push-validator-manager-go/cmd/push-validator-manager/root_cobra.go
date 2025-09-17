package main

import (
    "encoding/json"
    "fmt"
    "os"
    "strings"
    "time"

    "github.com/spf13/cobra"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/bootstrap"
    syncmon "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/sync"
    ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
)

// rootCmd wires the CLI surface using Cobra. Persistent flags are
// applied to a loaded config in loadCfg(). Subcommands implement the
// actual operations (init, start/stop, sync, status, etc.).
var rootCmd = &cobra.Command{
    Use:   "push-validator-manager",
    Short: "Push Validator Manager (Go)",
    Long:  "Manage a Push Chain validator node: init, start, status, sync, and admin tasks.",
}

var (
    flagHome   string
    flagBin    string
    flagRPC    string
    flagGenesis string
    flagOutput string
    flagVerbose bool
    flagQuiet bool
    flagDebug bool
)

func init() {
    // Persistent flags to override defaults
    rootCmd.PersistentFlags().StringVar(&flagHome, "home", "", "Node home directory (overrides env)")
    rootCmd.PersistentFlags().StringVar(&flagBin, "bin", "", "Path to pchaind binary (overrides env)")
    rootCmd.PersistentFlags().StringVar(&flagRPC, "rpc", "", "Local RPC base (http[s]://host:port)")
    rootCmd.PersistentFlags().StringVar(&flagGenesis, "genesis-domain", "", "Genesis RPC domain or URL")
    rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "text", "Output format: json|text")
    rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "Verbose output")
    rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Quiet mode: minimal output (suppresses extras)")
    rootCmd.PersistentFlags().BoolVarP(&flagDebug, "debug", "d", false, "Debug output: extra diagnostic logs")

    // status command (uses root --output), with --watch aliasing sync monitor
    var statusWatch bool
    var statusCompact bool
    var statusWindow int
    var statusRPC string
    var statusRemote string
    var statusInterval time.Duration
    statusCmd := &cobra.Command{
        Use:   "status",
        Short: "Show node status",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg := loadCfg()
            sup := process.New(cfg.HomeDir)
            if statusWatch {
                if statusRPC == "" { statusRPC = cfg.RPCLocal }
                if statusRemote == "" { statusRemote = "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443" }
                return syncmon.Run(cmd.Context(), syncmon.Options{
                    LocalRPC: statusRPC,
                    RemoteRPC: statusRemote,
                    LogPath: sup.LogPath(),
                    Window: statusWindow,
                    Compact: statusCompact,
                    Out: os.Stdout,
                    Interval: statusInterval,
                    Quiet: flagQuiet,
                    Debug: flagDebug,
                })
            }
            res := computeStatus(cfg, sup)
            switch flagOutput {
            case "json":
                enc := json.NewEncoder(os.Stdout)
                enc.SetIndent("", "  ")
                return enc.Encode(res)
            case "text", "":
                if flagQuiet {
                    fmt.Printf("running=%v rpc=%v catching_up=%v height=%d\n", res.Running, res.RPCListening, res.CatchingUp, res.Height)
                } else {
                    printStatusText(res)
                }
                return nil
            default:
                return fmt.Errorf("invalid --output: %s (use json|text)", flagOutput)
            }
        },
    }
    statusCmd.Flags().BoolVar(&statusWatch, "watch", false, "Continuously monitor sync progress (alias for 'sync')")
    statusCmd.Flags().BoolVar(&statusCompact, "compact", false, "Compact output when --watch")
    statusCmd.Flags().IntVar(&statusWindow, "window", 30, "Moving average window (headers) when --watch")
    statusCmd.Flags().StringVar(&statusRPC, "rpc", "", "Local RPC base when --watch (http[s]://host:port)")
    statusCmd.Flags().StringVar(&statusRemote, "remote", "", "Remote RPC base when --watch")
    statusCmd.Flags().DurationVar(&statusInterval, "interval", 1*time.Second, "Update interval when --watch (e.g. 1s, 2s)")
    rootCmd.AddCommand(statusCmd)

    // init (Cobra flags)
    var initMoniker, initChainID, initSnapshotRPC string
    initCmd := &cobra.Command{
        Use:   "init",
        Short: "Initialize local node home",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg := loadCfg()
            if initMoniker == "" { initMoniker = getenvDefault("MONIKER", "push-validator") }
            if initChainID == "" { initChainID = cfg.ChainID }
            if initSnapshotRPC == "" { initSnapshotRPC = cfg.SnapshotRPC }
            svc := bootstrap.New()
            if err := svc.Init(cmd.Context(), bootstrap.Options{
                HomeDir: cfg.HomeDir,
                ChainID: initChainID,
                Moniker: initMoniker,
                GenesisDomain: cfg.GenesisDomain,
                BinPath: findPchaind(),
                SnapshotRPCPrimary: initSnapshotRPC,
                SnapshotRPCSecondary: initSnapshotRPC,
            }); err != nil {
                ui.PrintError(ui.ErrorMessage{
                    Problem: "Initialization failed",
                    Causes: []string{
                        "Network issue fetching genesis or status",
                        "Incorrect --genesis-domain or RPC unreachable",
                        "pchaind binary missing or not executable",
                    },
                    Actions: []string{
                        "Verify connectivity: curl https://<genesis-domain>/status",
                        "Set --genesis-domain to a working RPC host",
                        "Ensure pchaind is installed and in PATH or pass --bin",
                    },
                    Hints: []string{"push-validator-manager validators --output json"},
                })
                return err
            }
            return nil
        },
    }
    initCmd.Flags().StringVar(&initMoniker, "moniker", "", "Validator moniker")
    initCmd.Flags().StringVar(&initChainID, "chain-id", "", "Chain ID")
    initCmd.Flags().StringVar(&initSnapshotRPC, "snapshot-rpc", "", "Snapshot RPC base URL")
    rootCmd.AddCommand(initCmd)

    // start (Cobra flags)
    var startBin string
    startCmd := &cobra.Command{
        Use:   "start",
        Short: "Start node",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg := loadCfg()
            if startBin != "" { os.Setenv("PCHAIND", startBin) }
            _, err := process.New(cfg.HomeDir).Start(process.StartOpts{HomeDir: cfg.HomeDir, Moniker: os.Getenv("MONIKER"), BinPath: findPchaind()})
            if err != nil {
                ui.PrintError(ui.ErrorMessage{
                    Problem: "Failed to start node",
                    Causes: []string{
                        "Missing genesis.json (init not run)",
                        "Invalid home directory or permissions",
                        "pchaind not found or incompatible",
                    },
                    Actions: []string{
                        "Run: push-validator-manager init",
                        "Check: ls <home>/config/genesis.json",
                        "Confirm pchaind version matches network",
                    },
                })
            }
            return err
        },
    }
    startCmd.Flags().StringVar(&startBin, "bin", "", "Path to pchaind binary")
    rootCmd.AddCommand(startCmd)

    rootCmd.AddCommand(&cobra.Command{Use: "stop", Short: "Stop node", RunE: func(cmd *cobra.Command, args []string) error { return handleStop(process.New(loadCfg().HomeDir)) }})

    var restartBin string
    restartCmd := &cobra.Command{Use: "restart", Short: "Restart node", RunE: func(cmd *cobra.Command, args []string) error {
        cfg := loadCfg(); if restartBin != "" { os.Setenv("PCHAIND", restartBin) }
        _, err := process.New(cfg.HomeDir).Restart(process.StartOpts{HomeDir: cfg.HomeDir, Moniker: os.Getenv("MONIKER"), BinPath: findPchaind()})
        if err != nil {
            ui.PrintError(ui.ErrorMessage{
                Problem: "Failed to restart node",
                Causes: []string{
                    "Process could not be stopped cleanly",
                    "Start preconditions failed (see start command)",
                },
                Actions: []string{
                    "Check logs: push-validator-manager logs",
                    "Try: push-validator-manager stop; then start",
                },
            })
        }
        return err
    }}
    restartCmd.Flags().StringVar(&restartBin, "bin", "", "Path to pchaind binary")
    rootCmd.AddCommand(restartCmd)

    rootCmd.AddCommand(&cobra.Command{Use: "logs", Short: "Tail node logs", RunE: func(cmd *cobra.Command, args []string) error { return handleLogs(process.New(loadCfg().HomeDir)) }})

    rootCmd.AddCommand(&cobra.Command{Use: "reset", Short: "Reset chain data", RunE: func(cmd *cobra.Command, args []string) error { return handleReset(loadCfg(), process.New(loadCfg().HomeDir)) }})
    rootCmd.AddCommand(&cobra.Command{Use: "backup", Short: "Backup config and validator state", RunE: func(cmd *cobra.Command, args []string) error { return handleBackup(loadCfg()) }})
    validatorsCmd := &cobra.Command{Use: "validators", Short: "List validators", RunE: func(cmd *cobra.Command, args []string) error { return handleValidatorsWithFormat(loadCfg(), flagOutput == "json") }}
    rootCmd.AddCommand(validatorsCmd)
    var balAddr string
    balanceCmd := &cobra.Command{Use: "balance [address]", Short: "Show balance", Args: cobra.RangeArgs(0,1), RunE: func(cmd *cobra.Command, args []string) error {
        if balAddr != "" { args = []string{balAddr} }
        return handleBalance(loadCfg(), args)
    }}
    balanceCmd.Flags().StringVar(&balAddr, "address", "", "Account address")
    rootCmd.AddCommand(balanceCmd)
    // register-validator: keep back-compat handler for now
    var regMoniker, regKey, regAmount string
    regCmd := &cobra.Command{Use: "register-validator", Short: "Register this node as validator", RunE: func(cmd *cobra.Command, args []string) error {
        cfg := loadCfg()
        if regMoniker == "" { regMoniker = getenvDefault("MONIKER", "push-validator") }
        if regKey == "" { regKey = getenvDefault("KEY_NAME", "validator-key") }
        if regAmount == "" { regAmount = getenvDefault("STAKE_AMOUNT", "1500000000000000000") }
        runRegisterValidator(cfg, regMoniker, regKey, regAmount)
        return nil
    }}
    regCmd.Flags().StringVar(&regMoniker, "moniker", "", "Validator moniker")
    regCmd.Flags().StringVar(&regKey, "key-name", "", "Key name")
    regCmd.Flags().StringVar(&regAmount, "amount", "", "Stake amount in base denom")
    rootCmd.AddCommand(regCmd)

    // completion and version
    rootCmd.AddCommand(&cobra.Command{Use: "completion [bash|zsh|fish|powershell]", Short: "Generate shell completion", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
        switch args[0] {
        case "bash": return rootCmd.GenBashCompletion(os.Stdout)
        case "zsh": return rootCmd.GenZshCompletion(os.Stdout)
        case "fish": return rootCmd.GenFishCompletion(os.Stdout, true)
        case "powershell": return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
        default: return fmt.Errorf("unknown shell: %s", args[0])
        }
    }})
    rootCmd.AddCommand(&cobra.Command{Use: "version", Short: "Show version", Run: func(cmd *cobra.Command, args []string) { fmt.Println("push-validator-manager dev") }})
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

// loadCfg reads defaults + env via internal/config.Load() and then
// applies overrides from persistent flags (home, bin, rpc, domain).
func loadCfg() config.Config {
    cfg := config.Load()
    if flagHome != "" { cfg.HomeDir = flagHome }
    if flagRPC != "" { cfg.RPCLocal = flagRPC }
    if flagGenesis != "" { cfg.GenesisDomain = flagGenesis }
    if flagBin != "" { os.Setenv("PCHAIND", flagBin) }
    return cfg
}
