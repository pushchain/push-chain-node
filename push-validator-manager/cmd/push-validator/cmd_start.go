package main

import (
    "fmt"

    "github.com/pushchain/push-chain-node/push-validator-manager/internal/process"
)

func handleStop(sup process.Supervisor) error {
    p := getPrinter()
    if err := sup.Stop(); err != nil {
        if flagOutput == "json" { p.JSON(map[string]any{"ok": false, "error": err.Error()}) } else { p.Error(fmt.Sprintf("stop error: %v", err)) }
        return err
    }
    if flagOutput == "json" {
        p.JSON(map[string]any{"ok": true, "action": "stop"})
    } else {
        p.Success("✓ Node stopped")
        fmt.Println()
        fmt.Println(p.Colors.Info("Next steps:"))
        fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager start"))
        fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (start the node)"))
    }
    return nil
}
