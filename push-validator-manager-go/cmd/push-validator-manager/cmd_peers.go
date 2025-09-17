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
            if base == "" { base = "http://127.0.0.1:26657" }
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
                id := p.ID
                if len(id) > 10 { id = id[:10] + "…" }
                rows = append(rows, []string{id, p.Addr})
            }
            fmt.Println(c.Header(" Connected Peers "))
            fmt.Print(ui.Table(c, headers, rows, []int{12, 24}))
            fmt.Printf("Total Peers: %d\n", len(plist))
            return nil
        },
    }
    rootCmd.AddCommand(peersCmd)
}

