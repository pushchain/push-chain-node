package main

import (
    "fmt"
    "os"
    "os/signal"
    "syscall"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
)

// handleLogs tails the node log file until interrupted. It validates
// the log path and prints structured JSON errors when --output=json.
func handleLogs(sup process.Supervisor) {
    lp := sup.LogPath()
    if lp == "" {
        if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": "no log path configured"}) } else { fmt.Println("no log path configured") }
        os.Exit(1)
    }
    if _, err := os.Stat(lp); err != nil {
        if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": "log file not found", "path": lp}) } else { fmt.Printf("log file not found: %s\n", lp) }
        os.Exit(1)
    }
    fmt.Printf("Tailing %s (Ctrl+C to stop)\n", lp)
    stop := make(chan struct{})
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    go func() { <-sigs; close(stop) }()
    if err := process.TailFollow(lp, os.Stdout, stop); err != nil {
        fmt.Printf("tail error: %v\n", err)
        os.Exit(1)
    }
}
