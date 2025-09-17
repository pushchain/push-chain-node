package main

import (
    "fmt"
    "os"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
)

func handleStop(sup process.Supervisor) {
    p := getPrinter()
    if err := sup.Stop(); err != nil {
        if flagOutput == "json" { p.JSON(map[string]any{"ok": false, "error": err.Error()}) } else { fmt.Printf("stop error: %v\n", err) }
        os.Exit(1)
    }
    if flagOutput == "json" { p.JSON(map[string]any{"ok": true, "action": "stop"}) } else { fmt.Println("node stopped") }
}
