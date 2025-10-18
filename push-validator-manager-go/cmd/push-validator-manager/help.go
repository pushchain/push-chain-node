package main

import (
	"fmt"
	"os"

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
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager start", "Start the node process"))
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager status", "Show node/rpc/sync status"))
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager dashboard", "Live dashboard with metrics"))
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager sync", "Monitor sync progress live"))
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
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager full-reset", "⚠️  Complete reset (deletes ALL keys and data)"))
		fmt.Fprintln(w)

		// Utilities
		fmt.Fprintln(w, c.SubHeader("Utilities"))
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager doctor", "Run diagnostic checks"))
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager peers", "List connected peers"))
		fmt.Fprintln(w, c.FormatCommand("push-validator-manager version", "Show version information"))
		fmt.Fprintln(w)
	})
}
