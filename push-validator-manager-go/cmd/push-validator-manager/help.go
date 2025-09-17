package main

import (
    "fmt"
    "os"
    "strings"

    ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
    "github.com/spf13/cobra"
)

func init() {
    // Replace root help to present grouped, example-rich output.
    rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
        c := ui.NewColorConfig()
        w := os.Stdout

        // Header
        fmt.Fprintln(w, c.Header(" Push Validator Manager "))
        fmt.Fprintln(w, c.Description("Manage a Push Chain validator node: init, start, status, sync, and admin tasks."))
        fmt.Fprintln(w, c.Separator(64))

        // Quick Start
        fmt.Fprintln(w, c.SubHeader("Quick Start"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager init", "Initialize node home and fetch genesis"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager start", "Start the node process"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager status", "Show node/rpc/sync status"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager status --watch", "Live monitor sync progress"))
        fmt.Fprintln(w)

        // Operations
        fmt.Fprintln(w, c.SubHeader("Operations"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager stop", "Stop the node process"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager restart", "Restart the node process"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager logs", "Tail node logs"))
        fmt.Fprintln(w)

        // Validator
        fmt.Fprintln(w, c.SubHeader("Validator"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager validators", "List validators (default pretty, --output json)"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager balance [address]", "Check account balance (defaults to KEY_NAME)"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager register-validator", "Register this node as a validator"))
        fmt.Fprintln(w)

        // Maintenance
        fmt.Fprintln(w, c.SubHeader("Maintenance"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager backup", "Create config/state backup archive"))
        fmt.Fprintln(w, c.FormatCommand("push-validator-manager reset", "Reset chain data (keeps addr book)"))
        fmt.Fprintln(w)

        // Flags
        fmt.Fprintln(w, c.SubHeader("Global Flags"))
        flags := []string{
            "--home <dir>\tNode home directory",
            "--bin <path>\tPath to pchaind binary",
            "--rpc <url>\tLocal RPC base (http[s]://host:port)",
            "--genesis-domain <host|url>\tGenesis RPC domain or URL",
            "-o, --output <text|json>\tOutput format",
            "--verbose\tVerbose output",
        }
        for _, f := range flags {
            parts := strings.SplitN(f, "\t", 2)
            if len(parts) == 2 {
                fmt.Fprintln(w, c.FormatFlag(parts[0], parts[1]))
            } else {
                fmt.Fprintln(w, parts[0])
            }
        }
        fmt.Fprintln(w)

        // Examples / Next steps
        fmt.Fprintln(w, c.SubHeader("Examples"))
        fmt.Fprintln(w, c.Description("Initialize and start:"))
        fmt.Fprintln(w, "  "+c.Command("push-validator-manager init --moniker my-node"))
        fmt.Fprintln(w, "  "+c.Command("push-validator-manager start"))
        fmt.Fprintln(w, c.Description("Monitor sync:"))
        fmt.Fprintln(w, "  "+c.Command("push-validator-manager status --watch --compact"))
        fmt.Fprintln(w, c.Description("Get balance for KEY_NAME:"))
        fmt.Fprintln(w, "  "+c.Command("KEY_NAME=mykey push-validator-manager balance"))
        fmt.Fprintln(w)

        // Default Cobra footer: show available subcommands briefly
        fmt.Fprintln(w, c.SubHeader("Commands"))
        for _, sc := range cmd.Commands() {
            if sc.IsAvailableCommand() && sc.Name() != "help" {
                fmt.Fprintf(w, "  %s  %s\n", c.Command(sc.Name()), c.Description(strings.TrimSpace(sc.Short)))
            }
        }
    })
}

