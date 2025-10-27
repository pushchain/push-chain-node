package main

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
)

// handleBalance prints an account balance. It resolves the address from
// either a positional argument or KEY_NAME when --address/arg is omitted.
// When --output=json is set, it emits a structured object.
func handleBalance(cfg config.Config, args []string) error {
    var addr string
    if len(args) > 0 { addr = args[0] }
    if addr == "" {
        key := os.Getenv("KEY_NAME")
        if key == "" {
            if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": "address not provided; set KEY_NAME or pass --address"}) } else { fmt.Println("usage: push-validator-manager balance <address> (or set KEY_NAME)") }
            return fmt.Errorf("address not provided")
        }
        out, err := exec.Command(findPchaind(), "keys", "show", key, "-a", "--keyring-backend", cfg.KeyringBackend, "--home", cfg.HomeDir).Output()
        if err != nil {
            if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()}) } else { fmt.Printf("resolve address error: %v\n", err) }
            return fmt.Errorf("resolve address: %w", err)
        }
        addr = strings.TrimSpace(string(out))
    }
    v := validator.NewWith(validator.Options{BinPath: findPchaind(), HomeDir: cfg.HomeDir, ChainID: cfg.ChainID, Keyring: cfg.KeyringBackend, GenesisDomain: cfg.GenesisDomain, Denom: cfg.Denom})
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    bal, err := v.Balance(ctx, addr)
    if err != nil {
        if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": err.Error(), "address": addr}) } else { getPrinter().Error(fmt.Sprintf("balance error: %v", err)) }
        return err
    }
    if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": true, "address": addr, "balance": bal, "denom": cfg.Denom}) } else { getPrinter().Info(fmt.Sprintf("%s %s", bal, cfg.Denom)) }
    return nil
}
