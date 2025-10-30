package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/dashboard"
)

// dashboardCmd provides an interactive TUI dashboard for monitoring validator status
func createDashboardCmd() *cobra.Command {
	var (
		refreshInterval time.Duration
		rpcTimeout      time.Duration
		debugMode       bool
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Interactive dashboard for monitoring validator status",
		Long: `Launch an interactive terminal dashboard showing real-time validator metrics:

  • Node process status (running/stopped, PID, version)
  • Chain sync progress with ETA calculation
  • Network connectivity (peers, latency)
  • Validator consensus power and status

The dashboard auto-refreshes every 2 seconds by default. Press '?' for help.

For non-interactive environments (CI/pipes), dashboard automatically falls back
to a static text snapshot.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration
			cfg := loadCfg()

			// Build dashboard options
			opts := dashboard.Options{
				Config:          cfg,
				RefreshInterval: refreshInterval,
				RPCTimeout:      rpcTimeout,
				NoColor:         flagNoColor,
				NoEmoji:         flagNoEmoji,
				Debug:           debugMode,
			}
			opts = normalizeDashboardOptions(opts)

			// Check if we're in a TTY environment
			isTTY := term.IsTerminal(int(os.Stdout.Fd()))

			if !isTTY {
				// Non-TTY environment (CI/pipes): use static mode
				if debugMode {
					fmt.Fprintln(os.Stderr, "Debug: Non-TTY detected, using static mode")
				}
				return runDashboardStatic(cmd.Context(), opts)
			}

			// TTY environment: use interactive Bubble Tea dashboard
			if debugMode {
				fmt.Fprintln(os.Stderr, "Debug: TTY detected, using interactive mode")
			}
			return runDashboardInteractive(opts)
		},
	}

	// Flags
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 2*time.Second, "Dashboard refresh interval")
	cmd.Flags().DurationVar(&rpcTimeout, "rpc-timeout", 15*time.Second, "RPC request timeout")
	cmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug mode for troubleshooting")

	return cmd
}

// runDashboardStatic performs a single fetch and prints static output for non-TTY
func runDashboardStatic(ctx context.Context, opts dashboard.Options) error {
	// Print debug info BEFORE dashboard output
	if opts.Debug {
		fmt.Fprintln(os.Stderr, "Debug: Starting dashboard...")
		fmt.Fprintf(os.Stderr, "Debug: Config loaded - HomeDir: %s, RPC: %s\n", opts.Config.HomeDir, opts.Config.RPCLocal)
		fmt.Fprintln(os.Stderr, "Debug: Running in static mode")
		fmt.Fprintln(os.Stderr, "---") // Separator
	}

	d := dashboard.New(opts)

	// Apply RPC timeout to context (prevents hung RPCs in CI/pipes)
	ctx, cancel := context.WithTimeout(ctx, opts.RPCTimeout)
	defer cancel()

	// Fetch data once with timeout
	data, err := d.FetchDataOnce(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch dashboard data: %w", err)
	}

	// Render static text snapshot to stdout
	fmt.Print(d.RenderStatic(data))
	return nil
}

// runDashboardInteractive launches the Bubble Tea TUI program
func runDashboardInteractive(opts dashboard.Options) error {
	d := dashboard.New(opts)
	if d == nil {
		return fmt.Errorf("failed to create dashboard instance")
	}

	// Create Bubble Tea program with proper TTY configuration
	// Key fix: Use stdin/stdout explicitly instead of /dev/tty
	p := tea.NewProgram(
		d,
		tea.WithAltScreen(),      // Use alternate screen buffer (clean display)
		tea.WithInput(os.Stdin),  // Use stdin instead of trying to open /dev/tty
		tea.WithOutput(os.Stdout), // Use stdout instead of trying to open /dev/tty
	)

	// Run program - blocks until quit
	if _, err := p.Run(); err != nil {
		// If TTY error, fall back to static mode
		if strings.Contains(err.Error(), "tty") || strings.Contains(err.Error(), "device not configured") {
			if opts.Debug {
				fmt.Fprintf(os.Stderr, "Debug: TTY error, falling back to static mode: %v\n", err)
			}
			return runDashboardStatic(context.Background(), opts)
		}
		return fmt.Errorf("dashboard error: %w", err)
	}

	return nil
}

// normalizeDashboardOptions applies default refresh/timeout values to keep behaviour
// consistent between interactive and static dashboard modes.
func normalizeDashboardOptions(opts dashboard.Options) dashboard.Options {
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 2 * time.Second
	}
	if opts.RPCTimeout <= 0 {
		// Default to 15s but cap at twice the refresh interval so the UI remains responsive.
		timeout := 15 * time.Second
		if opts.RefreshInterval > 0 {
			candidate := 2 * opts.RefreshInterval
			if candidate < timeout {
				timeout = candidate
			}
		}
		opts.RPCTimeout = timeout
	}
	return opts
}
