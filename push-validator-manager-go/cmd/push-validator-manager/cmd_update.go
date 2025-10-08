package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/exitcodes"
	ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
	"github.com/spf13/cobra"
)

var (
	updateBranch string
	updateForce  bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update validator manager and chain binary",
	Long: `Updates the Push Validator Manager and pchaind binary to the latest version.

This command:
1. Checks current versions
2. Pulls latest code from the repository
3. Rebuilds binaries
4. Restarts the node if it was running

Use --branch to update to a specific branch or tag.
Use --force to skip confirmation prompts.`,
	RunE: runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	p := ui.NewPrinterFromGlobal(flagOutput)
	c := p.Colors

	// Check if we're in a git repository or if we can find the repo
	execPath, err := os.Executable()
	if err != nil {
		return exitcodes.PreconditionErrorf("cannot determine executable path: %v", err)
	}

	// Try to find the repository root
	repoDir := findRepoRoot(filepath.Dir(execPath))
	if repoDir == "" {
		p.Warn("Cannot find repository. The update command requires installation from source.")
		p.Info("To update:")
		p.Info("1. Clone/pull the repository: https://github.com/pushchain/push-chain-node")
		p.Info("2. Run: bash push-validator-manager-go/install.sh --use-local")
		return exitcodes.PreconditionError("not installed from repository")
	}

	fmt.Println(c.Header(" VALIDATOR UPDATE "))
	fmt.Println()

	// Check current version/commit
	currentCommit := getGitCommit(repoDir)
	if currentCommit != "" {
		p.Info(fmt.Sprintf("Current version: %s", currentCommit[:8]))
	}

	// Fetch latest changes
	p.Info("Fetching latest changes...")
	branch := updateBranch
	if branch == "" {
		branch = "feature/pnm" // default branch
	}

	if err := gitFetch(repoDir); err != nil {
		return exitcodes.NetworkErrf("failed to fetch updates: %v", err)
	}

	// Check if update available
	latestCommit := getGitCommit(repoDir)
	if currentCommit == latestCommit && !updateForce {
		p.Success("Already up to date!")
		return nil
	}

	// Show what will be updated
	if !updateForce && !flagYes {
		// Check if non-interactive mode is enabled without --yes
		if flagNonInteractive {
			return exitcodes.PreconditionError("update requires confirmation: use --yes or --force in non-interactive mode")
		}

		fmt.Println()
		p.Warn(fmt.Sprintf("This will update to branch '%s'", branch))
		p.Info("The node will be stopped and restarted if running")
		fmt.Print("\nContinue? (y/N): ")

		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			p.Info("Update cancelled")
			return nil
		}
	}

	fmt.Println()
	p.Info("Updating repository...")

	// Pull latest changes
	if err := gitPull(repoDir, branch); err != nil {
		return exitcodes.NetworkErrf("failed to pull updates: %v", err)
	}

	p.Success("Updated to latest commit")

	// Rebuild binaries
	p.Info("Rebuilding binaries...")

	installScript := filepath.Join(repoDir, "push-validator-manager-go", "install.sh")
	if _, err := os.Stat(installScript); os.IsNotExist(err) {
		return exitcodes.PreconditionErrorf("install script not found at %s", installScript)
	}

	// Run install script with --use-local --no-reset
	installCmd := exec.Command("bash", installScript, "--use-local", "--no-reset", "--no-start")
	installCmd.Dir = filepath.Dir(installScript)
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	if err := installCmd.Run(); err != nil {
		return exitcodes.ProcessErrf("failed to rebuild: %v", err)
	}

	fmt.Println()
	p.Success("Update completed successfully!")
	p.Info("Restart the node with: push-validator-manager restart")

	return nil
}

func findRepoRoot(startDir string) string {
	dir := startDir
	for i := 0; i < 10; i++ { // limit search depth
		gitDir := filepath.Join(dir, ".git")
		if stat, err := os.Stat(gitDir); err == nil && stat.IsDir() {
			return dir
		}

		// Also check for push-validator-manager-go directory
		pvmDir := filepath.Join(dir, "push-validator-manager-go")
		if stat, err := os.Stat(pvmDir); err == nil && stat.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}
	return ""
}

func getGitCommit(repoDir string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitFetch(repoDir string) error {
	cmd := exec.Command("git", "fetch", "origin")
	cmd.Dir = repoDir
	return cmd.Run()
}

func gitPull(repoDir, branch string) error {
	cmd := exec.Command("git", "pull", "origin", branch)
	cmd.Dir = repoDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func init() {
	updateCmd.Flags().StringVar(&updateBranch, "branch", "", "Branch or tag to update to (default: feature/pnm)")
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "Force update without confirmation")
	rootCmd.AddCommand(updateCmd)
}
