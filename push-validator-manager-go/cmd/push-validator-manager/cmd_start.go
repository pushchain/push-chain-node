package main

import (
    "fmt"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
)

func handleStop(sup process.Supervisor) error {
    p := getPrinter()
    if err := sup.Stop(); err != nil {
        if flagOutput == "json" { p.JSON(map[string]any{"ok": false, "error": err.Error()}) } else { p.Error(fmt.Sprintf("stop error: %v", err)) }
        return err
    }
    if flagOutput == "json" { p.JSON(map[string]any{"ok": true, "action": "stop"}) } else { p.Success("node stopped") }
    return nil
}
