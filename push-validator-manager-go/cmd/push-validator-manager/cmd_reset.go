package main

import (
    "fmt"
    "os"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/admin"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
)

// handleReset stops the node (best-effort) and clears chain data while
// preserving the address book. It emits JSON or text depending on --output.
func handleReset(cfg config.Config, sup process.Supervisor) {
    _ = sup.Stop()
    if err := admin.Reset(admin.ResetOptions{
        HomeDir: cfg.HomeDir,
        BinPath: findPchaind(),
        KeepAddrBook: true,
    }); err != nil {
        if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()}) } else { fmt.Printf("reset error: %v\n", err) }
        os.Exit(1)
    }
    if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": true, "action": "reset"}) } else { fmt.Println("chain data reset (addr book kept)") }
}
