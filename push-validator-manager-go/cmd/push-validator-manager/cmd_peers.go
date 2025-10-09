package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
	ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
)

func init() {
	peersCmd := &cobra.Command{
		Use:   "peers",
		Short: "List connected peers (from local RPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadCfg()
			base := cfg.RPCLocal
			if base == "" {
				base = "http://127.0.0.1:26657"
			}
			cli := node.New(base)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			plist, err := cli.Peers(ctx)
			if err != nil {
				getPrinter().Error(fmt.Sprintf("peers error: %v", err))
				return err
			}
			c := ui.NewColorConfig()
			headers := []string{"ID", "ADDR"}
			rows := make([][]string, 0, len(plist))
			for _, p := range plist {
				rows = append(rows, []string{p.ID, p.Addr})
			}
			fmt.Println(c.Header(" Connected Peers "))
			// Set widths: 40 for ID (full peer ID), 0 for ADDR (auto)
			fmt.Print(ui.Table(c, headers, rows, []int{40, 0}))
			fmt.Printf("Total Peers: %d\n", len(plist))
			return nil
		},
	}
	rootCmd.AddCommand(peersCmd)
}
