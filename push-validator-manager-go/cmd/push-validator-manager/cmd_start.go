package main

import (
    "fmt"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
)

func handleStop(sup process.Supervisor) error {
    p := getPrinter()
    if err := sup.Stop(); err != nil {
        if flagOutput == "json" { p.JSON(map[string]any{"ok": false, "error": err.Error()}) } else { fmt.Printf("stop error: %v\n", err) }
        return err
    }
    if flagOutput == "json" { p.JSON(map[string]any{"ok": true, "action": "stop"}) } else { fmt.Println("node stopped") }
    return nil
}
