package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/admin"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/process"
	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
)

// handleReset stops the node (best-effort), clears chain data while
// preserving the address book, and restarts the node. It emits JSON or text depending on --output.
func handleReset(cfg config.Config, sup process.Supervisor) error {
	wasRunning := sup.IsRunning()
	_ = sup.Stop()

	showSpinner := flagOutput != "json" && term.IsTerminal(int(os.Stdout.Fd()))
	var (
		spinnerStop   chan struct{}
		spinnerTicker *time.Ticker
	)
	if showSpinner {
		c := ui.NewColorConfig()
		prefix := c.Info("Resetting chain data")
		sp := ui.NewSpinner(os.Stdout, prefix)
		spinnerStop = make(chan struct{})
		spinnerTicker = time.NewTicker(120 * time.Millisecond)
		go func() {
			for {
				select {
				case <-spinnerStop:
					return
				case <-spinnerTicker.C:
					sp.Tick()
				}
			}
		}()
	}

	err := admin.Reset(admin.ResetOptions{
		HomeDir:      cfg.HomeDir,
		BinPath:      findPchaind(),
		KeepAddrBook: true,
	})

	if showSpinner {
		spinnerTicker.Stop()
		close(spinnerStop)
		fmt.Fprint(os.Stdout, "\r\033[K")
	}

	if err != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()})
		} else {
			getPrinter().Error(fmt.Sprintf("reset error: %v", err))
		}
		return err
	}

	if flagOutput == "json" {
		getPrinter().JSON(map[string]any{"ok": true, "action": "reset"})
	} else {
		getPrinter().Success("chain data reset (addr book kept)")
	}

	// Restart the node if it was running before reset
	if wasRunning {
		_, startErr := sup.Start(process.StartOpts{
			HomeDir: cfg.HomeDir,
			Moniker: os.Getenv("MONIKER"),
			BinPath: findPchaind(),
		})
		if startErr != nil {
			if flagOutput == "json" {
				getPrinter().JSON(map[string]any{"ok": false, "error": fmt.Sprintf("failed to restart node: %v", startErr)})
			} else {
				getPrinter().Error(fmt.Sprintf("failed to restart node: %v", startErr))
			}
			return startErr
		}
		if flagOutput != "json" {
			getPrinter().Success("Node restarted")
		}
	}

	return nil
}

// handleFullReset performs a complete reset, deleting ALL data including validator keys.
// Requires explicit confirmation unless --yes flag is used.
func handleFullReset(cfg config.Config, sup process.Supervisor) error {
	// Stop node first
	_ = sup.Stop()

	if flagOutput != "json" {
		p := ui.NewPrinter(flagOutput)
		fmt.Println()
		fmt.Println(p.Colors.Warning("⚠️  FULL RESET - This will delete EVERYTHING"))
		fmt.Println()
		fmt.Println("This operation will permanently delete:")
		fmt.Println(p.Colors.Error("  • All blockchain data"))
		fmt.Println(p.Colors.Error("  • Validator consensus keys (priv_validator_key.json)"))
		fmt.Println(p.Colors.Error("  • All keyring accounts and keys"))
		fmt.Println(p.Colors.Error("  • Node identity (node_key.json)"))
		fmt.Println(p.Colors.Error("  • Address book and peer connections"))
		fmt.Println()
		fmt.Println(p.Colors.Warning("This will create a NEW validator identity - you cannot recover the old one!"))
		fmt.Println()

		// Require explicit confirmation
		if !flagYes {
			fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "Type 'yes' to confirm full reset: "))
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))

			if response != "yes" {
				fmt.Println(p.Colors.Info("Full reset cancelled"))
				return nil
			}
		}
	}

	// Perform full reset
	err := admin.FullReset(admin.FullResetOptions{
		HomeDir: cfg.HomeDir,
		BinPath: findPchaind(),
	})

	if err != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()})
		} else {
			getPrinter().Error(fmt.Sprintf("full reset error: %v", err))
		}
		return err
	}

	if flagOutput == "json" {
		getPrinter().JSON(map[string]any{"ok": true, "action": "full-reset"})
	} else {
		p := getPrinter()
		p.Success("✓ Full reset complete")
		fmt.Println()
		fmt.Println(p.Colors.Info("Next steps:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager start"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (will auto-initialize with new validator keys)"))
	}

	return nil
}
