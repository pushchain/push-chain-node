package main

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
)

var rootCmd = &cobra.Command{
    Use:   "push-validator-manager",
    Short: "Push Validator Manager (Go)",
    Long:  "Manage a Push Chain validator node: init, start, status, sync, and admin tasks.",
}

func init() {
    // status command with --output json|text
    var output string
    statusCmd := &cobra.Command{
        Use:   "status",
        Short: "Show node status",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg := config.Load()
            sup := process.New(cfg.HomeDir)
            res := computeStatus(cfg, sup)
            switch output {
            case "json":
                enc := json.NewEncoder(os.Stdout)
                enc.SetIndent("", "  ")
                return enc.Encode(res)
            case "text", "":
                printStatusText(res)
                return nil
            default:
                return fmt.Errorf("invalid --output: %s (use json|text)", output)
            }
        },
    }
    statusCmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: json|text")
    rootCmd.AddCommand(statusCmd)

    // Wrap other commands; disable flag parsing so existing stdlib flags work unchanged.
    rootCmd.AddCommand(&cobra.Command{Use: "init", Short: "Initialize local node home", DisableFlagParsing: true, Run: func(cmd *cobra.Command, args []string) { handleInit(config.Load()) }})
    rootCmd.AddCommand(&cobra.Command{Use: "start", Short: "Start node", DisableFlagParsing: true, Run: func(cmd *cobra.Command, args []string) { cfg := config.Load(); handleStart(cfg, process.New(cfg.HomeDir)) }})
    rootCmd.AddCommand(&cobra.Command{Use: "stop", Short: "Stop node", Run: func(cmd *cobra.Command, args []string) { handleStop(process.New(config.Load().HomeDir)) }})
    rootCmd.AddCommand(&cobra.Command{Use: "restart", Short: "Restart node", DisableFlagParsing: true, Run: func(cmd *cobra.Command, args []string) { cfg := config.Load(); handleRestart(cfg, process.New(cfg.HomeDir)) }})
    rootCmd.AddCommand(&cobra.Command{Use: "logs", Short: "Tail node logs", Run: func(cmd *cobra.Command, args []string) { handleLogs(process.New(config.Load().HomeDir)) }})
    rootCmd.AddCommand(&cobra.Command{Use: "sync", Short: "Monitor sync progress", DisableFlagParsing: true, Run: func(cmd *cobra.Command, args []string) { handleSync(config.Load()) }})
    rootCmd.AddCommand(&cobra.Command{Use: "reset", Short: "Reset chain data", Run: func(cmd *cobra.Command, args []string) { handleReset(config.Load(), process.New(config.Load().HomeDir)) }})
    rootCmd.AddCommand(&cobra.Command{Use: "backup", Short: "Backup config and validator state", Run: func(cmd *cobra.Command, args []string) { handleBackup(config.Load()) }})
    rootCmd.AddCommand(&cobra.Command{Use: "validators", Short: "List validators", Run: func(cmd *cobra.Command, args []string) { handleValidators(config.Load()) }})
    rootCmd.AddCommand(&cobra.Command{Use: "balance", Short: "Show balance", DisableFlagParsing: true, Run: func(cmd *cobra.Command, args []string) { handleBalance(config.Load(), args) }})
    rootCmd.AddCommand(&cobra.Command{Use: "register-validator", Short: "Register this node as validator", DisableFlagParsing: true, Run: func(cmd *cobra.Command, args []string) { handleRegisterValidator(config.Load()) }})
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

