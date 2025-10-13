package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/dashboard"
)

// dashboardCmd provides an interactive TUI dashboard for monitoring validator status
func createDashboardCmd() *cobra.Command {
	var (
		refreshInterval time.Duration
		rpcTimeout      time.Duration
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
			cfg := loadCfg()

			// Build dashboard options
			opts := dashboard.Options{
				Config:          cfg,
				RefreshInterval: refreshInterval,
				RPCTimeout:      rpcTimeout,
				NoColor:         flagNoColor,
				NoEmoji:         flagNoEmoji,
			}
			opts = normalizeDashboardOptions(opts)

			// Detect TTY - use golang.org/x/term for portable detection
			isTTY := term.IsTerminal(int(os.Stdout.Fd()))

			if !isTTY {
				// Non-TTY mode: fetch once and render static text
				return runDashboardStatic(cmd.Context(), opts)
			}

			// TTY mode: launch interactive Bubble Tea program
			return runDashboardInteractive(opts)
		},
	}

	// Flags
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 2*time.Second, "Dashboard refresh interval")
	cmd.Flags().DurationVar(&rpcTimeout, "rpc-timeout", 5*time.Second, "RPC request timeout")

	return cmd
}

// runDashboardStatic performs a single fetch and prints static output for non-TTY
func runDashboardStatic(ctx context.Context, opts dashboard.Options) error {
	d := dashboard.New(opts)

	// Apply RPC timeout to context (prevents hung RPCs in CI/pipes)
	ctx, cancel := context.WithTimeout(ctx, opts.RPCTimeout)
	defer cancel()

	// Fetch data once with timeout
	data, err := d.FetchDataOnce(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch dashboard data: %w", err)
	}

	// Render static text snapshot
	fmt.Print(d.RenderStatic(data))
	return nil
}

// runDashboardInteractive launches the Bubble Tea TUI program
func runDashboardInteractive(opts dashboard.Options) error {
	d := dashboard.New(opts)

	// Create Bubble Tea program with alternate screen buffer
	p := tea.NewProgram(
		d,
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	// Run program - blocks until quit
	if _, err := p.Run(); err != nil {
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
		// Default to 5s but cap at twice the refresh interval so the UI remains responsive.
		timeout := 5 * time.Second
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
