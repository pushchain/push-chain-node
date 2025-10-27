package main

import (
	"fmt"
	"os"

	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
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
		fmt.Fprintln(w, c.Header(" Push Validator "))
		fmt.Fprintln(w, c.Description("Manage a Push Chain validator node: init, start, status, sync, and admin tasks."))
		fmt.Fprintln(w, c.Separator(50))
		fmt.Fprintln(w)

		// Usage
		fmt.Fprintln(w, c.SubHeader("USAGE"))
		fmt.Fprintf(w, "  %s <command> [flags]\n", "push-validator")
		fmt.Fprintln(w)

		// Quick Start
		fmt.Fprintln(w, c.SubHeader("Quick Start"))
		fmt.Fprintln(w, c.FormatCommand("push-validator start", "Start the node process"))
		fmt.Fprintln(w, c.FormatCommand("push-validator status", "Show node/rpc/sync status"))
		fmt.Fprintln(w, c.FormatCommand("push-validator dashboard", "Live dashboard with metrics"))
		fmt.Fprintln(w, c.FormatCommand("push-validator sync", "Monitor sync progress live"))
		fmt.Fprintln(w)

		// Operations
		fmt.Fprintln(w, c.SubHeader("Operations"))
		fmt.Fprintln(w, c.FormatCommand("push-validator stop", "Stop the node process"))
		fmt.Fprintln(w, c.FormatCommand("push-validator restart", "Restart the node process"))
		fmt.Fprintln(w, c.FormatCommand("push-validator logs", "Tail node logs"))
		fmt.Fprintln(w)

		// Validator
		fmt.Fprintln(w, c.SubHeader("Validator"))
		fmt.Fprintln(w, c.FormatCommand("push-validator validators", "List validators (default pretty, --output json)"))
		fmt.Fprintln(w, c.FormatCommand("push-validator balance [address]", "Check account balance (defaults to KEY_NAME)"))
		fmt.Fprintln(w, c.FormatCommand("push-validator register-validator", "Register this node as a validator"))
		fmt.Fprintln(w, c.FormatCommand("push-validator unjail", "Restore jailed validator to active status"))
		fmt.Fprintln(w, c.FormatCommand("push-validator withdraw-rewards", "Withdraw validator rewards and commission"))
		fmt.Fprintln(w)

		// Maintenance
		fmt.Fprintln(w, c.SubHeader("Maintenance"))
		fmt.Fprintln(w, c.FormatCommand("push-validator backup", "Create config/state backup archive"))
		fmt.Fprintln(w, c.FormatCommand("push-validator reset", "Reset chain data (keeps addr book)"))
		fmt.Fprintln(w, c.FormatCommand("push-validator full-reset", "⚠️  Complete reset (deletes ALL keys and data)"))
		fmt.Fprintln(w)

		// Utilities
		fmt.Fprintln(w, c.SubHeader("Utilities"))
		fmt.Fprintln(w, c.FormatCommand("push-validator doctor", "Run diagnostic checks"))
		fmt.Fprintln(w, c.FormatCommand("push-validator peers", "List connected peers"))
		fmt.Fprintln(w, c.FormatCommand("push-validator version", "Show version information"))
		fmt.Fprintln(w)
	})
}
