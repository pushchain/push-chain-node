package main

import (
    "fmt"

    "github.com/pushchain/push-chain-node/push-validator-manager/internal/admin"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
)

// handleBackup creates a backup archive of the node configuration and
// prints the resulting path, or a JSON object when --output=json.
func handleBackup(cfg config.Config) error {
    path, err := admin.Backup(admin.BackupOptions{HomeDir: cfg.HomeDir})
    if err != nil {
        if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()}) } else { getPrinter().Error(fmt.Sprintf("backup error: %v", err)) }
        return err
    }
    if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": true, "backup_path": path}) } else { getPrinter().Success(fmt.Sprintf("backup created: %s", path)) }
    return nil
}
