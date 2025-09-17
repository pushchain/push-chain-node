package main

import (
    "os"
    "strings"
    "time"

    "github.com/spf13/cobra"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
    syncmon "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/sync"
)

func init() {
    var syncCompact bool
    var syncWindow int
    var syncRPC string
    var syncRemote string
    var syncInterval time.Duration

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
                Interval: syncInterval,
            })
        },
    }
    syncCmd.Flags().BoolVar(&syncCompact, "compact", false, "Compact output")
    syncCmd.Flags().IntVar(&syncWindow, "window", 30, "Moving average window (headers)")
    syncCmd.Flags().StringVar(&syncRPC, "rpc", "", "Local RPC base (http[s]://host:port)")
    syncCmd.Flags().StringVar(&syncRemote, "remote", "", "Remote RPC base")
    syncCmd.Flags().DurationVar(&syncInterval, "interval", 1*time.Second, "Update interval (e.g. 1s, 2s)")
    rootCmd.AddCommand(syncCmd)
}

