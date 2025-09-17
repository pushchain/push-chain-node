package main

import (
    "encoding/json"
    "fmt"
    "os"
    "strings"

    "github.com/spf13/cobra"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/bootstrap"
    syncmon "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/sync"
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
)

func init() {
    // Persistent flags to override defaults
    rootCmd.PersistentFlags().StringVar(&flagHome, "home", "", "Node home directory (overrides env)")
    rootCmd.PersistentFlags().StringVar(&flagBin, "bin", "", "Path to pchaind binary (overrides env)")
    rootCmd.PersistentFlags().StringVar(&flagRPC, "rpc", "", "Local RPC base (http[s]://host:port)")
    rootCmd.PersistentFlags().StringVar(&flagGenesis, "genesis-domain", "", "Genesis RPC domain or URL")
    rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "text", "Output format: json|text")
    rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "Verbose output")

    // status command (uses root --output)
    statusCmd := &cobra.Command{
        Use:   "status",
        Short: "Show node status",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg := loadCfg()
            sup := process.New(cfg.HomeDir)
            res := computeStatus(cfg, sup)
            switch flagOutput {
            case "json":
                enc := json.NewEncoder(os.Stdout)
                enc.SetIndent("", "  ")
                return enc.Encode(res)
            case "text", "":
                printStatusText(res)
                return nil
            default:
                return fmt.Errorf("invalid --output: %s (use json|text)", flagOutput)
            }
        },
    }
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
            return svc.Init(cmd.Context(), bootstrap.Options{
                HomeDir: cfg.HomeDir,
                ChainID: initChainID,
                Moniker: initMoniker,
                GenesisDomain: cfg.GenesisDomain,
                BinPath: findPchaind(),
                SnapshotRPCPrimary: initSnapshotRPC,
                SnapshotRPCSecondary: initSnapshotRPC,
            })
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
            return err
        },
    }
    startCmd.Flags().StringVar(&startBin, "bin", "", "Path to pchaind binary")
    rootCmd.AddCommand(startCmd)

    rootCmd.AddCommand(&cobra.Command{Use: "stop", Short: "Stop node", Run: func(cmd *cobra.Command, args []string) { handleStop(process.New(loadCfg().HomeDir)) }})

    var restartBin string
    restartCmd := &cobra.Command{Use: "restart", Short: "Restart node", RunE: func(cmd *cobra.Command, args []string) error {
        cfg := loadCfg(); if restartBin != "" { os.Setenv("PCHAIND", restartBin) }
        _, err := process.New(cfg.HomeDir).Restart(process.StartOpts{HomeDir: cfg.HomeDir, Moniker: os.Getenv("MONIKER"), BinPath: findPchaind()})
        return err
    }}
    restartCmd.Flags().StringVar(&restartBin, "bin", "", "Path to pchaind binary")
    rootCmd.AddCommand(restartCmd)

    rootCmd.AddCommand(&cobra.Command{Use: "logs", Short: "Tail node logs", Run: func(cmd *cobra.Command, args []string) { handleLogs(process.New(loadCfg().HomeDir)) }})
    // Proper Cobra sync command with flags
    var syncCompact bool
    var syncWindow int
    var syncRPC string
    var syncRemote string
    syncCmd := &cobra.Command{
        Use:   "sync",
        Short: "Monitor sync progress",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg := loadCfg()
            if syncRPC == "" { syncRPC = cfg.RPCLocal }
            if syncRemote == "" { syncRemote = "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443" }
            sup := process.New(cfg.HomeDir)
            return syncmon.Run(cmd.Context(), syncmon.Options{
                LocalRPC: syncRPC,
                RemoteRPC: syncRemote,
                LogPath: sup.LogPath(),
                Window: syncWindow,
                Compact: syncCompact,
                Out: os.Stdout,
            })
        },
    }
    syncCmd.Flags().BoolVar(&syncCompact, "compact", false, "Compact output")
    syncCmd.Flags().IntVar(&syncWindow, "window", 30, "Moving average window (headers)")
    syncCmd.Flags().StringVar(&syncRPC, "rpc", "", "Local RPC base (http[s]://host:port)")
    syncCmd.Flags().StringVar(&syncRemote, "remote", "", "Remote RPC base")
    rootCmd.AddCommand(syncCmd)

    rootCmd.AddCommand(&cobra.Command{Use: "reset", Short: "Reset chain data", Run: func(cmd *cobra.Command, args []string) { handleReset(loadCfg(), process.New(loadCfg().HomeDir)) }})
    rootCmd.AddCommand(&cobra.Command{Use: "backup", Short: "Backup config and validator state", Run: func(cmd *cobra.Command, args []string) { handleBackup(loadCfg()) }})
    validatorsCmd := &cobra.Command{Use: "validators", Short: "List validators", Run: func(cmd *cobra.Command, args []string) { handleValidatorsWithFormat(loadCfg(), flagOutput == "json") }}
    rootCmd.AddCommand(validatorsCmd)
    var balAddr string
    balanceCmd := &cobra.Command{Use: "balance [address]", Short: "Show balance", Args: cobra.RangeArgs(0,1), Run: func(cmd *cobra.Command, args []string) {
        if balAddr != "" { args = []string{balAddr} }
        handleBalance(loadCfg(), args)
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
