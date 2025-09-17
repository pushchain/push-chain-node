package main

import (
    "fmt"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/admin"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
)

// handleBackup creates a backup archive of the node configuration and
// prints the resulting path, or a JSON object when --output=json.
func handleBackup(cfg config.Config) error {
    path, err := admin.Backup(admin.BackupOptions{HomeDir: cfg.HomeDir})
    if err != nil {
        if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()}) } else { fmt.Printf("backup error: %v\n", err) }
        return err
    }
    if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": true, "backup_path": path}) } else { fmt.Printf("backup created: %s\n", path) }
    return nil
}
