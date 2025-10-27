package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/bootstrap"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/dashboard"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/exitcodes"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/metrics"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/process"
	syncmon "github.com/pushchain/push-chain-node/push-validator-manager/internal/sync"
	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
)

// Version information - set via -ldflags during build
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// rootCmd wires the CLI surface using Cobra. Persistent flags are
// applied to a loaded config in loadCfg(). Subcommands implement the
// actual operations (init, start/stop, sync, status, etc.).
var rootCmd = &cobra.Command{
	Use:   "push-validator",
	Short: "Push Validator",
	Long:  "Manage a Push Chain validator node: init, start, status, sync, and admin tasks.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize global UI config from flags after parsing but before command execution
		ui.InitGlobal(ui.Config{
			NoColor:        flagNoColor,
			NoEmoji:        flagNoEmoji,
			Yes:            flagYes,
			NonInteractive: flagNonInteractive,
			Verbose:        flagVerbose,
			Quiet:          flagQuiet,
			Debug:          flagDebug,
		})
	},
}

var (
	flagHome           string
	flagBin            string
	flagRPC            string
	flagGenesis        string
	flagOutput         string
	flagVerbose        bool
	flagQuiet          bool
	flagDebug          bool
	flagNoColor        bool
	flagNoEmoji        bool
	flagYes            bool
	flagNonInteractive bool
)

func init() {
	// Persistent flags to override defaults
	rootCmd.PersistentFlags().StringVar(&flagHome, "home", "", "Node home directory (overrides env)")
	rootCmd.PersistentFlags().StringVar(&flagBin, "bin", "", "Path to pchaind binary (overrides env)")
	rootCmd.PersistentFlags().StringVar(&flagRPC, "rpc", "", "Local RPC base (http[s]://host:port)")
	rootCmd.PersistentFlags().StringVar(&flagGenesis, "genesis-domain", "", "Genesis RPC domain or URL")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "text", "Output format: json|yaml|text")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Quiet mode: minimal output (suppresses extras)")
	rootCmd.PersistentFlags().BoolVarP(&flagDebug, "debug", "d", false, "Debug output: extra diagnostic logs")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable ANSI colors")
	rootCmd.PersistentFlags().BoolVar(&flagNoEmoji, "no-emoji", false, "Disable emoji output")
	rootCmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, "Assume yes for all prompts")
	rootCmd.PersistentFlags().BoolVar(&flagNonInteractive, "non-interactive", false, "Fail instead of prompting")

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
		fmt.Fprintln(w)
	})

	// status command (uses root --output)
	var statusStrict bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show node status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadCfg()
			sup := process.New(cfg.HomeDir)
			res := computeStatus(cfg, sup)

			// Strict mode: exit non-zero if issues detected
			if statusStrict && (res.Error != "" || !res.Running || res.CatchingUp || res.Peers == 0) {
				// Still output the status before exiting
				switch flagOutput {
				case "json":
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					_ = enc.Encode(res)
				case "yaml":
					data, _ := yaml.Marshal(res)
					fmt.Println(string(data))
				case "text", "":
					if !flagQuiet {
						printStatusText(res)
					}
				}
				return exitcodes.ValidationErr("node has issues")
			}

			switch flagOutput {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			case "yaml":
				data, err := yaml.Marshal(res)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			case "text", "":
				if flagQuiet {
					fmt.Printf("running=%v rpc=%v catching_up=%v height=%d\n", res.Running, res.RPCListening, res.CatchingUp, res.Height)
				} else {
					printStatusText(res)
				}
				return nil
			default:
				return fmt.Errorf("invalid --output: %s (use json|yaml|text)", flagOutput)
			}
		},
	}
	statusCmd.Flags().BoolVar(&statusStrict, "strict", false, "Exit non-zero if node has issues (not running, catching up, no peers, or errors)")
	rootCmd.AddCommand(statusCmd)

	// dashboard - interactive TUI for monitoring
	rootCmd.AddCommand(createDashboardCmd())

	// init (Cobra flags)
	var initMoniker, initChainID, initSnapshotRPC string
	initCmd := &cobra.Command{
		Use:    "init",
		Short:  "Initialize local node home",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadCfg()
			p := getPrinter()
			if initMoniker == "" {
				initMoniker = getenvDefault("MONIKER", "push-validator")
			}
			if initChainID == "" {
				initChainID = cfg.ChainID
			}
			if initSnapshotRPC == "" {
				initSnapshotRPC = cfg.SnapshotRPC
			}

			// Create progress callback that shows init steps
			progressCallback := func(msg string) {
				if flagOutput != "json" {
					fmt.Printf("  → %s\n", msg)
				}
			}

			svc := bootstrap.New()
			if err := svc.Init(cmd.Context(), bootstrap.Options{
				HomeDir:              cfg.HomeDir,
				ChainID:              initChainID,
				Moniker:              initMoniker,
				GenesisDomain:        cfg.GenesisDomain,
				BinPath:              findPchaind(),
				SnapshotRPCPrimary:   initSnapshotRPC,
				SnapshotRPCSecondary: "https://rpc-testnet-donut-node1.push.org",
				Progress:             progressCallback,
			}); err != nil {
				ui.PrintError(ui.ErrorMessage{
					Problem: "Initialization failed",
					Causes: []string{
						"Network issue fetching genesis or status",
						"Incorrect --genesis-domain or RPC unreachable",
						"pchaind binary missing or not executable",
					},
					Actions: []string{
						"Verify connectivity: curl https://<genesis-domain>/status",
						"Set --genesis-domain to a working RPC host",
						"Ensure pchaind is installed and in PATH or pass --bin",
					},
					Hints: []string{"push-validator validators --output json"},
				})
				return err
			}
			if flagOutput != "json" {
				p.Success("✓ Initialization complete")
			}
			return nil
		},
	}
	initCmd.Flags().StringVar(&initMoniker, "moniker", "", "Validator moniker")
	initCmd.Flags().StringVar(&initChainID, "chain-id", "", "Chain ID")
	initCmd.Flags().StringVar(&initSnapshotRPC, "snapshot-rpc", "", "Snapshot RPC base URL")
	rootCmd.AddCommand(initCmd)

	// start (Cobra flags)
	var startBin string
	var startNoPrompt bool
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start node",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadCfg()
			p := getPrinter()

			// Check if initialization is needed (genesis.json or validator keys missing)
			genesisPath := filepath.Join(cfg.HomeDir, "config", "genesis.json")
			privValKeyPath := filepath.Join(cfg.HomeDir, "config", "priv_validator_key.json")
			nodeKeyPath := filepath.Join(cfg.HomeDir, "config", "node_key.json")

			// Initialize if genesis OR validator keys are missing
			// (needed for first-time setup and post-full-reset scenarios)
			needsInit := false
			if _, err := os.Stat(genesisPath); os.IsNotExist(err) {
				needsInit = true
			}
			if _, err := os.Stat(privValKeyPath); os.IsNotExist(err) {
				needsInit = true
			}
			if _, err := os.Stat(nodeKeyPath); os.IsNotExist(err) {
				needsInit = true
			}

			if needsInit {
				// Auto-initialize on first start
				if flagOutput != "json" {
					p.Info("Initializing node (first time)...")
					fmt.Println()
				}

				// Create progress callback that shows init steps
				progressCallback := func(msg string) {
					if flagOutput != "json" {
						fmt.Printf("  → %s\n", msg)
					}
				}

				svc := bootstrap.New()
				if err := svc.Init(cmd.Context(), bootstrap.Options{
					HomeDir:              cfg.HomeDir,
					ChainID:              cfg.ChainID,
					Moniker:              getenvDefault("MONIKER", "push-validator"),
					GenesisDomain:        cfg.GenesisDomain,
					BinPath:              findPchaind(),
					SnapshotRPCPrimary:   cfg.SnapshotRPC,
					SnapshotRPCSecondary: "https://rpc-testnet-donut-node1.push.org",
					Progress:             progressCallback,
				}); err != nil {
					ui.PrintError(ui.ErrorMessage{
						Problem: "Initialization failed",
						Causes: []string{
							"Network issue fetching genesis or status",
							"Incorrect genesis domain configuration",
							"pchaind binary missing or not executable",
						},
						Actions: []string{
							"Verify connectivity: curl https://<genesis-domain>/status",
							"Check genesis domain in config",
							"Ensure pchaind is installed and in PATH",
						},
					})
					return err
				}

				if flagOutput != "json" {
					fmt.Println()
					p.Success("✓ Initialization complete")
				}
			}

			// Check if node is already running
			sup := process.New(cfg.HomeDir)
			isAlreadyRunning := sup.IsRunning()

			if flagOutput != "json" {
				if isAlreadyRunning {
					if pid, ok := sup.PID(); ok {
						p.Success(fmt.Sprintf("✓ Node is running (PID: %d)", pid))
					} else {
						p.Success("✓ Node is running")
					}
				} else {
					p.Info("Starting node...")
				}
			}

			// Continue with normal start
			if startBin != "" {
				os.Setenv("PCHAIND", startBin)
			}
			_, err := sup.Start(process.StartOpts{HomeDir: cfg.HomeDir, Moniker: os.Getenv("MONIKER"), BinPath: findPchaind()})
			if err != nil {
				ui.PrintError(ui.ErrorMessage{
					Problem: "Failed to start node",
					Causes: []string{
						"Invalid home directory or permissions",
						"pchaind not found or incompatible",
						"Port already in use",
					},
					Actions: []string{
						"Check: ls <home>/config/genesis.json",
						"Confirm pchaind version matches network",
						"Verify ports 26656/26657 are available",
					},
				})
				return err
			}
			if flagOutput == "json" {
				p.JSON(map[string]any{"ok": true, "action": "start", "already_running": isAlreadyRunning})
			} else {
				if !isAlreadyRunning {
					p.Success("✓ Node started successfully")
				}

				// Check validator status and show appropriate next steps (skip if --no-prompt)
				if !startNoPrompt {
					fmt.Println()
					if !handlePostStartFlow(cfg, &p) {
						// If post-start flow fails, just continue (node is already started)
						return nil
					}
				}
			}
			return nil
		},
	}
	startCmd.Flags().StringVar(&startBin, "bin", "", "Path to pchaind binary")
	startCmd.Flags().BoolVar(&startNoPrompt, "no-prompt", false, "Skip post-start prompts (for use in scripts)")
	rootCmd.AddCommand(startCmd)

	rootCmd.AddCommand(&cobra.Command{Use: "stop", Short: "Stop node", RunE: func(cmd *cobra.Command, args []string) error { return handleStop(process.New(loadCfg().HomeDir)) }})

	var restartBin string
	restartCmd := &cobra.Command{Use: "restart", Short: "Restart node", RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadCfg()
		p := getPrinter()
		if restartBin != "" {
			os.Setenv("PCHAIND", restartBin)
		}
		_, err := process.New(cfg.HomeDir).Restart(process.StartOpts{HomeDir: cfg.HomeDir, Moniker: os.Getenv("MONIKER"), BinPath: findPchaind()})
		if err != nil {
			ui.PrintError(ui.ErrorMessage{
				Problem: "Failed to restart node",
				Causes: []string{
					"Process could not be stopped cleanly",
					"Start preconditions failed (see start command)",
				},
				Actions: []string{
					"Check logs: push-validator logs",
					"Try: push-validator stop; then start",
				},
			})
			return err
		}
		if flagOutput == "json" {
			p.JSON(map[string]any{"ok": true, "action": "restart"})
		} else {
			p.Success("✓ Node restarted")
			fmt.Println()
			fmt.Println(p.Colors.Info("Useful commands:"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator status"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (check sync progress)"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator dashboard"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (live dashboard)"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator logs"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (view logs)"))
		}
		return nil
	}}
	restartCmd.Flags().StringVar(&restartBin, "bin", "", "Path to pchaind binary")
	rootCmd.AddCommand(restartCmd)

	rootCmd.AddCommand(&cobra.Command{Use: "logs", Short: "Tail node logs", RunE: func(cmd *cobra.Command, args []string) error { return handleLogs(process.New(loadCfg().HomeDir)) }})

	rootCmd.AddCommand(&cobra.Command{Use: "reset", Short: "Reset chain data", RunE: func(cmd *cobra.Command, args []string) error {
		return handleReset(loadCfg(), process.New(loadCfg().HomeDir))
	}})
	rootCmd.AddCommand(&cobra.Command{Use: "full-reset", Short: "Complete reset (deletes all keys and data)", RunE: func(cmd *cobra.Command, args []string) error {
		return handleFullReset(loadCfg(), process.New(loadCfg().HomeDir))
	}})
	rootCmd.AddCommand(&cobra.Command{Use: "backup", Short: "Backup config and validator state", RunE: func(cmd *cobra.Command, args []string) error { return handleBackup(loadCfg()) }})
	validatorsCmd := &cobra.Command{Use: "validators", Short: "List validators", RunE: func(cmd *cobra.Command, args []string) error {
		return handleValidatorsWithFormat(loadCfg(), flagOutput == "json")
	}}
	rootCmd.AddCommand(validatorsCmd)
	var balAddr string
	balanceCmd := &cobra.Command{Use: "balance [address]", Short: "Show balance", Args: cobra.RangeArgs(0, 1), RunE: func(cmd *cobra.Command, args []string) error {
		if balAddr != "" {
			args = []string{balAddr}
		}
		return handleBalance(loadCfg(), args)
	}}
	balanceCmd.Flags().StringVar(&balAddr, "address", "", "Account address")
	rootCmd.AddCommand(balanceCmd)
	// register-validator: interactive flow with optional flag overrides
	regCmd := &cobra.Command{Use: "register-validator", Short: "Register this node as validator", RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadCfg()
		handleRegisterValidator(cfg)
		return nil
	}}
	regCmd.Flags().BoolVar(&flagRegisterCheckOnly, "check-only", false, "Exit after reporting validator registration status")
	rootCmd.AddCommand(regCmd)

	// unjail command
	unjailCmd := &cobra.Command{
		Use:   "unjail",
		Short: "Restore jailed validator to active status",
		Long:  "Unjail a validator that was temporarily jailed for downtime, restoring it to the active validator set",
		RunE: func(cmd *cobra.Command, args []string) error {
			handleUnjail(loadCfg())
			return nil
		},
	}
	rootCmd.AddCommand(unjailCmd)

	// withdraw-rewards command
	withdrawRewardsCmd := &cobra.Command{
		Use:     "withdraw-rewards",
		Aliases: []string{"withdraw", "claim-rewards"},
		Short:   "Withdraw validator rewards and commission",
		Long:    "Withdraw accumulated delegation rewards and optionally withdraw validator commission",
		RunE: func(cmd *cobra.Command, args []string) error {
			handleWithdrawRewards(loadCfg())
			return nil
		},
	}
	rootCmd.AddCommand(withdrawRewardsCmd)

	// increase-stake command
	increaseStakeCmd := &cobra.Command{
		Use:   "increase-stake",
		Short: "Increase validator stake",
		Long:  "Delegate additional tokens to increase your validator's stake and voting power",
		RunE: func(cmd *cobra.Command, args []string) error {
			handleIncreaseStake(loadCfg())
			return nil
		},
	}
	rootCmd.AddCommand(increaseStakeCmd)

	// completion and version
	rootCmd.AddCommand(&cobra.Command{Use: "completion [bash|zsh|fish|powershell]", Short: "Generate shell completion", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unknown shell: %s", args[0])
		}
	}})
	// version command with semantic versioning
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			switch flagOutput {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				enc.Encode(map[string]string{
					"version":    Version,
					"commit":     Commit,
					"build_date": BuildDate,
				})
			case "yaml":
				data, _ := yaml.Marshal(map[string]string{
					"version":    Version,
					"commit":     Commit,
					"build_date": BuildDate,
				})
				fmt.Println(string(data))
			default:
				fmt.Printf("push-validator %s (%s) built %s\n", Version, Commit, BuildDate)
			}
		},
	}
	rootCmd.AddCommand(versionCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitcodes.CodeForError(err))
	}
}

// loadCfg reads defaults + env via internal/config.Load() and then
// applies overrides from persistent flags (home, bin, rpc, domain).
func loadCfg() config.Config {
	cfg := config.Load()
	if flagHome != "" {
		cfg.HomeDir = flagHome
	}
	if flagRPC != "" {
		cfg.RPCLocal = flagRPC
	}
	if flagGenesis != "" {
		cfg.GenesisDomain = flagGenesis
	}
	if flagBin != "" {
		os.Setenv("PCHAIND", flagBin)
	}
	return cfg
}

// handlePostStartFlow manages the post-start flow based on validator status.
// Returns false if an error occurred (non-fatal), true if flow completed successfully.
func handlePostStartFlow(cfg config.Config, p *ui.Printer) bool {
	// First, check if the node is still syncing using comprehensive sync check
	// (same logic as dashboard/status to ensure accuracy)
	fmt.Println(p.Colors.Info("▸ Checking Sync Status"))

	collector := metrics.NewWithoutCPU()
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 5*time.Second)
	snap := collector.Collect(syncCtx, "http://127.0.0.1:26657", cfg.GenesisDomain)
	syncCancel()

	// Consider synced only if:
	// 1. CatchingUp is false AND
	// 2. Local height is within 5 blocks of remote height (or remote height unavailable)
	const syncTolerance = 5
	isSyncing := snap.Chain.CatchingUp ||
		(snap.Chain.RemoteHeight > 0 && snap.Chain.LocalHeight < snap.Chain.RemoteHeight-syncTolerance)

	// DEBUG: Log sync status if verbose
	if flagVerbose {
		fmt.Printf("[DEBUG] Sync Check: CatchingUp=%v, LocalHeight=%d, RemoteHeight=%d, IsSyncing=%v\n",
			snap.Chain.CatchingUp, snap.Chain.LocalHeight, snap.Chain.RemoteHeight, isSyncing)
	}

	if isSyncing {
		// Node is still syncing - wait for sync to complete before validator checks
		fmt.Println(p.Colors.Info("  ▸ Node is syncing with the network..."))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "    Waiting for sync to complete...\n"))
		fmt.Println(p.Colors.Info("▸ Monitoring Sync Progress"))

		// Wait for sync to complete using sync monitor
		sup := process.New(cfg.HomeDir)
		remoteURL := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"

		if err := syncmon.Run(context.Background(), syncmon.Options{
			LocalRPC:     "http://127.0.0.1:26657",
			RemoteRPC:    remoteURL,
			LogPath:      sup.LogPath(),
			Window:       30,
			Compact:      false,
			Out:          os.Stdout,
			Interval:     120 * time.Millisecond,
			Quiet:        flagQuiet,
			Debug:        flagDebug,
			StuckTimeout: 2 * time.Minute,
		}); err != nil {
			// Sync failed or stuck - show warning and dashboard
			fmt.Println()
			fmt.Println(p.Colors.Warning("  ⚠ Sync monitoring error (will retry in dashboard)"))
			showDashboardPrompt(cfg, p)
			return false
		}

		// Sync complete - fall through to validator checks
		fmt.Println()
	} else {
		// Node is already synced - show success message
		fmt.Println(p.Colors.Success("  ✓ Node is synced"))
	}

	// Node is synced (or sync check failed) - proceed with validator checks
	// Check if already a validator
	v := validator.NewWith(validator.Options{
		BinPath:       findPchaind(),
		HomeDir:       cfg.HomeDir,
		ChainID:       cfg.ChainID,
		Keyring:       cfg.KeyringBackend,
		GenesisDomain: cfg.GenesisDomain,
		Denom:         cfg.Denom,
	})

	// Show status checking message
	fmt.Println(p.Colors.Info("▸ Checking Validator Status"))

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 10*time.Second)
	isValidator, err := v.IsValidator(statusCtx, "")
	statusCancel()

	if err != nil {
		// If we can't check status, show warning but continue to dashboard
		fmt.Println(p.Colors.Warning("  ⚠ Could not verify validator status (will retry in dashboard)"))
		showDashboardPrompt(cfg, p)
		return false
	}

	if isValidator {
		// Already a validator - show success and dashboard
		fmt.Println(p.Colors.Success("  ✓ Registered as validator"))
		showDashboardPrompt(cfg, p)
		return true
	}

	// Not a validator - show registration prompt
	fmt.Println(p.Colors.Warning("  ⚠ Not registered as validator"))
	fmt.Println()
	fmt.Println("Next steps to register as validator:")
	fmt.Println("1. Get test tokens: https://faucet.push.org")
	fmt.Println("2. Check balance: push-validator balance")
	fmt.Println("3. Register: push-validator register-validator")
	fmt.Println()

	// Check if we're in an interactive terminal
	if !isTerminalInteractive() {
		// Non-interactive - show dashboard with prompt
		showDashboardPrompt(cfg, p)
		return true
	}

	// Interactive prompt - use /dev/tty to avoid buffering os.Stdin
	// This ensures stdin remains clean for subsequent log UI raw mode
	fmt.Print("Register as validator now? (y/N) ")

	ttyFile, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	var response string
	if err == nil {
		reader := bufio.NewReader(ttyFile)
		line, readErr := reader.ReadString('\n')
		ttyFile.Close()
		if readErr != nil {
			// Error reading input - show dashboard
			fmt.Println()
			showDashboardPrompt(cfg, p)
			return false
		}
		response = strings.ToLower(strings.TrimSpace(line))
	} else {
		// Fallback to stdin if /dev/tty unavailable
		reader := bufio.NewReader(os.Stdin)
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			fmt.Println()
			showDashboardPrompt(cfg, p)
			return false
		}
		response = strings.ToLower(strings.TrimSpace(line))
	}

	if response == "y" || response == "yes" {
		// User wants to register
		fmt.Println()
		handleRegisterValidator(cfg)
		fmt.Println()
	} else {
		// User declined - add newline for clean output
		fmt.Println()
	}

	// Always show dashboard at the end
	showDashboardPrompt(cfg, p)
	return true
}

// handleDashboard launches the interactive dashboard
func handleDashboard(cfg config.Config) error {
	opts := dashboard.Options{
		Config:          cfg,
		RefreshInterval: 3 * time.Second,
		RPCTimeout:      5 * time.Second,
		NoColor:         flagNoColor,
		NoEmoji:         flagNoEmoji,
		Debug:           false,
	}
	return runDashboardInteractive(opts)
}

// showDashboardPrompt displays a prompt asking user to press ENTER to launch dashboard
// Always shows the prompt, but handles timeouts gracefully in non-interactive environments
// Follows the install flow pattern with clear before/after messages
func showDashboardPrompt(cfg config.Config, p *ui.Printer) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                   DASHBOARD AVAILABLE                         ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  The node is running in the background.")
	fmt.Println("  Press ENTER to open the interactive dashboard (or Ctrl+C to skip)")
	fmt.Println("  Note: The node will continue running in the background.")
	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────────────")
	fmt.Print("Press ENTER to continue to the dashboard... ")

	// Try /dev/tty first (best for interactive terminals)
	ttyFile, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err == nil {
		// TTY available - wait for ENTER indefinitely (user is present)
		reader := bufio.NewReader(ttyFile)
		_, readErr := reader.ReadString('\n')
		ttyFile.Close()
		if readErr != nil {
			// Ctrl+C or error
			fmt.Println()
			fmt.Println("  Dashboard skipped. Node is running in background.")
			fmt.Println()
			return
		}
		// User pressed ENTER - launch dashboard
		fmt.Println()
		_ = handleDashboard(cfg)

		// After dashboard exit, show status
		fmt.Println()
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(p.Colors.Success("✓ Dashboard closed. Node is still running in background."))
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()
		return
	}

	// No TTY available - try stdin with timeout (for scripts/CI/non-interactive)
	done := make(chan bool, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')
		done <- true
	}()

	select {
	case <-done:
		// Got input - launch dashboard
		fmt.Println()
		_ = handleDashboard(cfg)

		// After dashboard exit, show status
		fmt.Println()
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println(p.Colors.Success("✓ Dashboard closed. Node is still running in background."))
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()
	case <-time.After(2 * time.Second):
		// Timeout - likely script/CI with no input available
		fmt.Println()
		fmt.Println("  Dashboard is available - run: push-validator dashboard")
		fmt.Println()
	}
}

// isTerminalInteractive checks if we're running in an interactive terminal
func isTerminalInteractive() bool {
	// Check stdin is a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	// Check stdout is a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	return true
}
