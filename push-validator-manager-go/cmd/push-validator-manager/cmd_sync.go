package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/exitcodes"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
	syncmon "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/sync"
)

func init() {
	var syncCompact bool
	var syncWindow int
	var syncRPC string
	var syncRemote string
	var syncSkipFinal bool
	var syncInterval time.Duration
	var syncStuckTimeout time.Duration

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Monitor sync progress",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadCfg()
			if syncRPC == "" {
				syncRPC = cfg.RPCLocal
			}
			if syncRemote == "" {
				syncRemote = "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
			}
			sup := process.New(cfg.HomeDir)
			if syncStuckTimeout <= 0 {
				if envTimeout := os.Getenv("PNM_SYNC_STUCK_TIMEOUT"); envTimeout != "" {
					if parsed, err := time.ParseDuration(envTimeout); err == nil {
						syncStuckTimeout = parsed
					}
				}
			}
			if err := syncmon.Run(cmd.Context(), syncmon.Options{
				LocalRPC:     syncRPC,
				RemoteRPC:    syncRemote,
				LogPath:      sup.LogPath(),
				Window:       syncWindow,
				Compact:      syncCompact,
				Out:          os.Stdout,
				Interval:     syncInterval,
				Quiet:        flagQuiet,
				Debug:        flagDebug,
				StuckTimeout: syncStuckTimeout,
			}); err != nil {
				if errors.Is(err, syncmon.ErrSyncStuck) {
					return exitcodes.NewError(exitcodes.SyncStuck, err.Error())
				}
				return err
			}
			if !syncSkipFinal {
				out := cmd.OutOrStdout()
				if flagQuiet {
					fmt.Fprintln(out, "Sync complete.")
				} else {
					fmt.Fprintln(out, "✅ Sync complete! Node is fully synced.")
				}
			}
			return nil
		},
	}
	syncCmd.Flags().BoolVar(&syncCompact, "compact", false, "Compact output")
	syncCmd.Flags().IntVar(&syncWindow, "window", 30, "Moving average window (headers)")
	syncCmd.Flags().StringVar(&syncRPC, "rpc", "", "Local RPC base (http[s]://host:port)")
	syncCmd.Flags().StringVar(&syncRemote, "remote", "", "Remote RPC base")
	syncCmd.Flags().DurationVar(&syncInterval, "interval", 120*time.Millisecond, "Update interval (e.g. 1s, 2s)")
	syncCmd.Flags().BoolVar(&syncSkipFinal, "skip-final-message", false, "Suppress completion message (for automation)")
	syncCmd.Flags().DurationVar(&syncStuckTimeout, "stuck-timeout", 0, "Stuck detection timeout (e.g. 2m, 5m). 0 uses default or PNM_SYNC_STUCK_TIMEOUT")
	rootCmd.AddCommand(syncCmd)
}
