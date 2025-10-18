package main

import (
    "os"
    ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
)

// findPchaind returns the path to the pchaind binary, resolving
// either PCHAIND or PCHAIN_BIN environment variables, or falling
// back to the literal "pchaind" on PATH.
func findPchaind() string {
    if v := os.Getenv("PCHAIND"); v != "" { return v }
    if v := os.Getenv("PCHAIN_BIN"); v != "" { return v }
    return "pchaind"
}

// getenvDefault returns the environment value for k, or default d
// when k is not set.
func getenvDefault(k, d string) string { if v := os.Getenv(k); v != "" { return v }; return d }

// getPrinter returns a UI printer bound to the current --output flag.
func getPrinter() ui.Printer { return ui.NewPrinter(flagOutput) }
