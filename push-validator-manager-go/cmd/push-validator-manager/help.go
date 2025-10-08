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
        // Help runs before PersistentPreRun, so manually configure colors
        c := ui.NewColorConfig()
        c.Enabled = c.Enabled && !flagNoColor
        c.EmojiEnabled = c.EmojiEnabled && !flagNoEmoji
        w := os.Stdout

        // Header
        fmt.Fprintln(w, c.Header(" Push Validator Manager "))
        fmt.Fprintln(w, c.Description("Manage a Push Chain validator node: init, start, status, sync, and admin tasks."))
        fmt.Fprintln(w, c.Separator(50))
        fmt.Fprintln(w)

        // Usage
        fmt.Fprintln(w, c.SubHeader("USAGE"))
        fmt.Fprintf(w, "  %s <command> [flags]\n", "push-validator-manager")
        fmt.Fprintln(w)

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
        fmt.Fprintln(w, c.SubHeader("GLOBAL FLAGS"))
        flags := []string{
            "--home <dir>\tNode home directory",
            "--bin <path>\tPath to pchaind binary",
            "--rpc <url>\tLocal RPC base (http[s]://host:port)",
            "--genesis-domain <host|url>\tGenesis RPC domain or URL",
            "-o, --output <text|json|yaml>\tOutput format",
            "--verbose\tVerbose output",
            "-q, --quiet\tQuiet mode (minimal output)",
            "-d, --debug\tDebug output (extra diagnostics)",
            "--no-color\tDisable ANSI colors",
            "--no-emoji\tDisable emoji output",
            "-y, --yes\tAssume yes for all prompts",
            "--non-interactive\tFail instead of prompting",
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

        // Shell Completion
        fmt.Fprintln(w, c.SubHeader("Shell Completion"))
        fmt.Fprintln(w, c.Description("Enable tab completion for your shell:"))
        fmt.Fprintln(w, "  "+c.Description("bash:"))
        fmt.Fprintln(w, "    "+c.Command("push-validator-manager completion bash > /etc/bash_completion.d/push-validator-manager"))
        fmt.Fprintln(w, "  "+c.Description("zsh (add to ~/.zshrc):"))
        fmt.Fprintln(w, "    "+c.Command("source <(push-validator-manager completion zsh)"))
        fmt.Fprintln(w, "  "+c.Description("fish:"))
        fmt.Fprintln(w, "    "+c.Command("push-validator-manager completion fish > ~/.config/fish/completions/push-validator-manager.fish"))
        fmt.Fprintln(w)

        // Exit Codes
        fmt.Fprintln(w, c.SubHeader("Exit Codes"))
        exitCodes := []string{
            "0\tSuccess",
            "1\tGeneral error",
            "2\tInvalid arguments or flags",
            "3\tPrecondition failed (not initialized, missing config)",
            "4\tNetwork error (RPC unreachable, timeout)",
            "5\tProcess error (failed to start/stop, permission denied)",
            "6\tValidation error (invalid config, corrupted data)",
        }
        for _, ec := range exitCodes {
            parts := strings.SplitN(ec, "\t", 2)
            if len(parts) == 2 {
                fmt.Fprintf(w, "  %s  %s\n", c.Value(parts[0]), c.Description(parts[1]))
            }
        }
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
