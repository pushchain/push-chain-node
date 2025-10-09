package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/admin"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
	ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
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
