package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/process"
	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
)

// handleLogs tails the node log file until interrupted. It validates
// the log path and prints structured JSON errors when --output=json.
func handleLogs(sup process.Supervisor) error {
	lp := sup.LogPath()
	if lp == "" {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "no log path configured"})
		} else {
			getPrinter().Error("no log path configured")
		}
		return fmt.Errorf("no log path configured")
	}
	if _, err := os.Stat(lp); err != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "log file not found", "path": lp})
		} else {
			getPrinter().Error(fmt.Sprintf("log file not found: %s", lp))
		}
		return fmt.Errorf("log file not found: %s", lp)
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && !flagNonInteractive
	var tty *os.File
	if !interactive && !flagNonInteractive {
		if t, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			if term.IsTerminal(int(t.Fd())) {
				interactive = true
				tty = t
			} else {
				t.Close()
			}
		}
	}
	if interactive {
		var (
			origIn  = os.Stdin
			origOut = os.Stdout
		)
		if tty != nil {
			os.Stdin = tty
			os.Stdout = tty
		}
		defer func() {
			if tty != nil {
				tty.Close()
			}
			os.Stdin = origIn
			os.Stdout = origOut
		}()
		// Pass context.Background() - RunLogUIV2 handles Ctrl+C via raw terminal input
		return ui.RunLogUIV2(context.Background(), ui.LogUIOptions{
			LogPath:    lp,
			BgKey:      'b',
			ShowFooter: true,
			NoColor:    flagNoColor,
		})
	}
	if tty != nil {
		tty.Close()
	}

	getPrinter().Info(fmt.Sprintf("Tailing %s (Ctrl+C to stop)", lp))
	stop := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigs; close(stop) }()
	if err := process.TailFollow(lp, os.Stdout, stop); err != nil {
		fmt.Printf("tail error: %v\n", err)
		return err
	}
	// Print exit message after Ctrl+C
	fmt.Println()
	getPrinter().Success("Stopped tailing logs")
	return nil
}
