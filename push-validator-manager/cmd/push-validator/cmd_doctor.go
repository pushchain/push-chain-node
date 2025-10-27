package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/exitcodes"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/node"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/process"
	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostic checks on validator setup",
	Long: `Performs comprehensive health checks on your validator setup including:
- Process status and accessibility
- Configuration file validity
- Network connectivity (RPC, P2P, remote endpoints)
- Disk space and permissions
- Common configuration issues`,
	RunE: runDoctor,
}

type checkResult struct {
	Name     string
	Status   string // "pass", "warn", "fail"
	Message  string
	Details  []string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cfg := config.Load()
	if flagHome != "" {
		cfg.HomeDir = flagHome
	}

	c := ui.NewColorConfigFromGlobal()
	results := []checkResult{}

	// Header
	fmt.Println(c.Header(" VALIDATOR HEALTH CHECK "))
	fmt.Println()

	// Run all diagnostic checks
	results = append(results, checkProcessRunning(cfg, c))
	results = append(results, checkRPCAccessible(cfg, c))
	results = append(results, checkConfigFiles(cfg, c))
	results = append(results, checkP2PPeers(cfg, c))
	results = append(results, checkRemoteConnectivity(cfg, c))
	results = append(results, checkDiskSpace(cfg, c))
	results = append(results, checkPermissions(cfg, c))
	results = append(results, checkSyncStatus(cfg, c))

	// Summary
	fmt.Println()
	fmt.Println(c.Separator(60))

	passed := 0
	warned := 0
	failed := 0

	for _, r := range results {
		switch r.Status {
		case "pass":
			passed++
		case "warn":
			warned++
		case "fail":
			failed++
		}
	}

	summary := fmt.Sprintf("Checks: %d passed, %d warnings, %d failed", passed, warned, failed)
	if failed > 0 {
		fmt.Println(c.Error("✗ " + summary))
		return exitcodes.ValidationErr("health checks failed")
	} else if warned > 0 {
		fmt.Println(c.Warning("⚠ " + summary))
	} else {
		fmt.Println(c.Success("✓ " + summary))
	}

	return nil
}

func checkProcessRunning(cfg config.Config, c *ui.ColorConfig) checkResult {
	sup := process.New(cfg.HomeDir)
	running := sup.IsRunning()

	result := checkResult{Name: "Process Status"}

	if running {
		if pid, ok := sup.PID(); ok {
			result.Status = "pass"
			result.Message = fmt.Sprintf("Validator process running (PID %d)", pid)
		} else {
			result.Status = "pass"
			result.Message = "Validator process running"
		}
	} else {
		result.Status = "fail"
		result.Message = "Validator process not running"
		result.Details = []string{"Run 'push-validator-manager start' to start the node"}
	}

	printCheck(result, c)
	return result
}

func checkRPCAccessible(cfg config.Config, c *ui.ColorConfig) checkResult {
	rpc := cfg.RPCLocal
	if rpc == "" {
		rpc = "http://127.0.0.1:26657"
	}

	result := checkResult{Name: "RPC Accessibility"}

	hostport := "127.0.0.1:26657"
	if u, err := url.Parse(rpc); err == nil && u.Host != "" {
		hostport = u.Host
	}

	if process.IsRPCListening(hostport, 500*time.Millisecond) {
		result.Status = "pass"
		result.Message = fmt.Sprintf("RPC listening on %s", hostport)
	} else {
		result.Status = "fail"
		result.Message = fmt.Sprintf("RPC not accessible at %s", hostport)
		result.Details = []string{
			"Check if the node is running",
			"Verify firewall rules allow local connections",
			"Check config.toml for correct RPC settings",
		}
	}

	printCheck(result, c)
	return result
}

func checkConfigFiles(cfg config.Config, c *ui.ColorConfig) checkResult {
	result := checkResult{Name: "Configuration Files"}

	configPath := filepath.Join(cfg.HomeDir, "config", "config.toml")
	genesisPath := filepath.Join(cfg.HomeDir, "config", "genesis.json")

	missing := []string{}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		missing = append(missing, "config.toml")
	}
	if _, err := os.Stat(genesisPath); os.IsNotExist(err) {
		missing = append(missing, "genesis.json")
	}

	if len(missing) > 0 {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Missing configuration files: %s", strings.Join(missing, ", "))
		result.Details = []string{"Run 'push-validator-manager init' to initialize configuration"}
	} else {
		result.Status = "pass"
		result.Message = "All required configuration files present"
	}

	printCheck(result, c)
	return result
}

func checkP2PPeers(cfg config.Config, c *ui.ColorConfig) checkResult {
	result := checkResult{Name: "P2P Network"}

	rpc := cfg.RPCLocal
	if rpc == "" {
		rpc = "http://127.0.0.1:26657"
	}

	cli := node.New(rpc)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	peers, err := cli.Peers(ctx)
	if err != nil {
		result.Status = "warn"
		result.Message = "Could not check peer connections"
		result.Details = []string{fmt.Sprintf("RPC error: %v", err)}
	} else if len(peers) == 0 {
		result.Status = "fail"
		result.Message = "No P2P peers connected"
		result.Details = []string{
			"Check persistent_peers in config.toml",
			"Verify firewall allows port 26656",
			"Check if seed nodes are reachable",
		}
	} else if len(peers) < 3 {
		result.Status = "warn"
		result.Message = fmt.Sprintf("Only %d peer(s) connected (recommend 3+)", len(peers))
	} else {
		result.Status = "pass"
		result.Message = fmt.Sprintf("%d peers connected", len(peers))
	}

	printCheck(result, c)
	return result
}

func checkRemoteConnectivity(cfg config.Config, c *ui.ColorConfig) checkResult {
	result := checkResult{Name: "Remote Connectivity"}

	remote := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cli := node.New(remote)
	_, err := cli.Status(ctx)

	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Cannot reach %s", cfg.GenesisDomain)
		result.Details = []string{
			fmt.Sprintf("Error: %v", err),
			"Check internet connectivity",
			"Verify genesis domain is correct",
		}
	} else {
		result.Status = "pass"
		result.Message = fmt.Sprintf("Remote RPC accessible at %s", cfg.GenesisDomain)
	}

	printCheck(result, c)
	return result
}

func checkDiskSpace(cfg config.Config, c *ui.ColorConfig) checkResult {
	result := checkResult{Name: "Disk Space"}

	dataDir := filepath.Join(cfg.HomeDir, "data")

	// Try to get disk usage (cross-platform is tricky, simplified check)
	stat, err := os.Stat(cfg.HomeDir)
	if err != nil {
		result.Status = "warn"
		result.Message = "Could not check disk space"
		result.Details = []string{fmt.Sprintf("Error: %v", err)}
	} else if stat.IsDir() {
		// Simple check: can we write a test file?
		testFile := filepath.Join(cfg.HomeDir, ".diskcheck")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			result.Status = "fail"
			result.Message = "Cannot write to data directory"
			result.Details = []string{
				fmt.Sprintf("Error: %v", err),
				"Check disk space",
				"Verify write permissions",
			}
		} else {
			os.Remove(testFile)
			result.Status = "pass"
			result.Message = fmt.Sprintf("Data directory writable at %s", dataDir)
		}
	} else {
		result.Status = "fail"
		result.Message = fmt.Sprintf("%s is not a directory", cfg.HomeDir)
	}

	printCheck(result, c)
	return result
}

func checkPermissions(cfg config.Config, c *ui.ColorConfig) checkResult {
	result := checkResult{Name: "File Permissions"}

	configPath := filepath.Join(cfg.HomeDir, "config", "config.toml")

	info, err := os.Stat(configPath)
	if err != nil {
		result.Status = "warn"
		result.Message = "Could not check file permissions"
		result.Details = []string{fmt.Sprintf("Error: %v", err)}
	} else {
		mode := info.Mode()
		// Check if world-readable (less strict than world-writable)
		if mode.Perm()&0004 != 0 {
			result.Status = "pass"
			result.Message = "Configuration files have appropriate permissions"
		} else {
			result.Status = "warn"
			result.Message = "Configuration files may have restrictive permissions"
			result.Details = []string{fmt.Sprintf("config.toml has mode %o", mode.Perm())}
		}
	}

	printCheck(result, c)
	return result
}

func checkSyncStatus(cfg config.Config, c *ui.ColorConfig) checkResult {
	result := checkResult{Name: "Sync Status"}

	rpc := cfg.RPCLocal
	if rpc == "" {
		rpc = "http://127.0.0.1:26657"
	}

	cli := node.New(rpc)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := cli.Status(ctx)
	if err != nil {
		result.Status = "warn"
		result.Message = "Could not check sync status"
		result.Details = []string{fmt.Sprintf("RPC error: %v", err)}
	} else {
		if status.CatchingUp {
			result.Status = "warn"
			result.Message = fmt.Sprintf("Node is syncing (height: %d)", status.Height)
			result.Details = []string{"Wait for sync to complete before validating"}
		} else {
			result.Status = "pass"
			result.Message = fmt.Sprintf("Node is synced (height: %d)", status.Height)
		}
	}

	printCheck(result, c)
	return result
}

func printCheck(r checkResult, c *ui.ColorConfig) {
	icon := ""
	msg := ""

	switch r.Status {
	case "pass":
		icon = c.Success("✓")
		msg = c.Success(r.Message)
	case "warn":
		icon = c.Warning("⚠")
		msg = c.Warning(r.Message)
	case "fail":
		icon = c.Error("✗")
		msg = c.Error(r.Message)
	}

	fmt.Printf("%s %s: %s\n", icon, c.Apply(c.Theme.Header, r.Name), msg)

	for _, detail := range r.Details {
		fmt.Printf("  %s %s\n", c.Apply(c.Theme.Pending, "→"), detail)
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
