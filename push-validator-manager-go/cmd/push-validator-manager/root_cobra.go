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

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/bootstrap"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/exitcodes"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
	ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
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
	Use:   "push-validator-manager",
	Short: "Push Validator Manager (Go)",
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
			if initMoniker == "" {
				initMoniker = getenvDefault("MONIKER", "push-validator")
			}
			if initChainID == "" {
				initChainID = cfg.ChainID
			}
			if initSnapshotRPC == "" {
				initSnapshotRPC = cfg.SnapshotRPC
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
					Hints: []string{"push-validator-manager validators --output json"},
				})
				return err
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

			// Check if initialization is needed (genesis.json missing)
			genesisPath := filepath.Join(cfg.HomeDir, "config", "genesis.json")
			if _, err := os.Stat(genesisPath); os.IsNotExist(err) {
				// Auto-initialize on first start
				if flagOutput != "json" {
					p.Info("Initializing node (first time)...")
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
					p.Success("✓ Initialization complete")
				}
			}

			// Continue with normal start
			if startBin != "" {
				os.Setenv("PCHAIND", startBin)
			}
			_, err := process.New(cfg.HomeDir).Start(process.StartOpts{HomeDir: cfg.HomeDir, Moniker: os.Getenv("MONIKER"), BinPath: findPchaind()})
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
				p.JSON(map[string]any{"ok": true, "action": "start"})
			} else {
				p.Success("✓ Node started")
				fmt.Println()
				fmt.Println(p.Colors.Info("Useful commands:"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager status"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (check node health)"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager dashboard"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (live dashboard)"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager logs"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (view logs)"))
				fmt.Println()

				// Check validator status and show appropriate next steps (skip if --no-prompt)
				if !startNoPrompt {
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
					"Check logs: push-validator-manager logs",
					"Try: push-validator-manager stop; then start",
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
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager status"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (check sync progress)"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager dashboard"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (live dashboard)"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager logs"))
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
				fmt.Printf("push-validator-manager %s (%s) built %s\n", Version, Commit, BuildDate)
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
	// Check if already a validator
	v := validator.NewWith(validator.Options{
		BinPath:       findPchaind(),
		HomeDir:       cfg.HomeDir,
		ChainID:       cfg.ChainID,
		Keyring:       cfg.KeyringBackend,
		GenesisDomain: cfg.GenesisDomain,
		Denom:         cfg.Denom,
	})

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 10*time.Second)
	isValidator, err := v.IsValidator(statusCtx, "")
	statusCancel()

	if err != nil {
		// If we can't check status, just show logs
		fmt.Println(p.Colors.Warning("⚠ Could not verify validator status"))
		fmt.Println()
		sup := process.New(cfg.HomeDir)
		_ = handleLogs(sup)
		return false
	}

	if isValidator {
		// Already a validator - show logs immediately
		fmt.Println(p.Colors.Success("✓ Already registered as validator"))
		fmt.Println()
		sup := process.New(cfg.HomeDir)
		_ = handleLogs(sup)
		return true
	}

	// Not a validator - show registration flow
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("1. Get test tokens from: https://faucet.push.org")
	fmt.Println("2. Register as validator: push-validator-manager register-validator")
	fmt.Println()

	// Check if we're in an interactive terminal
	if !isTerminalInteractive() {
		// Non-interactive - just show logs
		sup := process.New(cfg.HomeDir)
		_ = handleLogs(sup)
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
			// Error reading input - just show logs
			sup := process.New(cfg.HomeDir)
			_ = handleLogs(sup)
			return false
		}
		response = strings.ToLower(strings.TrimSpace(line))
	} else {
		// Fallback to stdin if /dev/tty unavailable
		reader := bufio.NewReader(os.Stdin)
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			sup := process.New(cfg.HomeDir)
			_ = handleLogs(sup)
			return false
		}
		response = strings.ToLower(strings.TrimSpace(line))
	}

	if response == "y" || response == "yes" {
		// User wants to register
		fmt.Println()
		handleRegisterValidator(cfg)
		fmt.Println()
	}

	// Always show logs at the end
	sup := process.New(cfg.HomeDir)
	_ = handleLogs(sup)
	return true
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
