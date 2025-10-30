package admin

import (
    "archive/tar"
    "compress/gzip"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "time"
)

type ResetOptions struct {
    HomeDir string
    BinPath string // pchaind path
    KeepAddrBook bool
}

type FullResetOptions struct {
    HomeDir string
    BinPath string // pchaind path
}

type BackupOptions struct {
    HomeDir string
    OutDir  string // if empty, defaults to <HomeDir>/backups
}

// Reset clears ALL blockchain data while preserving validator keys and keyring.
// This ensures clean state without AppHash errors while maintaining validator identity.
func Reset(opts ResetOptions) error {
    if opts.HomeDir == "" { return fmt.Errorf("HomeDir required") }

    // Backup address book if requested
    addrBookPath := filepath.Join(opts.HomeDir, "config", "addrbook.json")
    var addrBookData []byte
    if opts.KeepAddrBook {
        addrBookData, _ = os.ReadFile(addrBookPath)
    }

    // Remove entire data directory (ALL blockchain data including all databases)
    _ = os.RemoveAll(filepath.Join(opts.HomeDir, "data"))

    // Remove logs directory
    _ = os.RemoveAll(filepath.Join(opts.HomeDir, "logs"))

    // Recreate essential directories
    _ = os.MkdirAll(filepath.Join(opts.HomeDir, "data"), 0o755)
    _ = os.MkdirAll(filepath.Join(opts.HomeDir, "logs"), 0o755)

    // Restore address book if it was backed up
    if opts.KeepAddrBook && len(addrBookData) > 0 {
        _ = os.WriteFile(addrBookPath, addrBookData, 0o644)
    }

    return nil
}

// FullReset removes ALL data including validator keys and keyring.
// WARNING: This is destructive and creates a completely new validator identity.
func FullReset(opts FullResetOptions) error {
    if opts.HomeDir == "" { return fmt.Errorf("HomeDir required") }
    if opts.BinPath == "" { opts.BinPath = "pchaind" }

    // Remove entire data directory (includes all blockchain data)
    _ = os.RemoveAll(filepath.Join(opts.HomeDir, "data"))

    // Remove keyring (all keys)
    _ = os.RemoveAll(filepath.Join(opts.HomeDir, "keyring-file"))
    _ = os.RemoveAll(filepath.Join(opts.HomeDir, "keyring-test"))

    // Remove validator keys
    _ = os.Remove(filepath.Join(opts.HomeDir, "config", "priv_validator_key.json"))
    _ = os.Remove(filepath.Join(opts.HomeDir, "config", "node_key.json"))

    // Remove logs
    _ = os.RemoveAll(filepath.Join(opts.HomeDir, "logs"))

    // Clean address book
    _ = os.Remove(filepath.Join(opts.HomeDir, "config", "addrbook.json"))

    // Recreate essential directories
    _ = os.MkdirAll(filepath.Join(opts.HomeDir, "data"), 0o755)
    _ = os.MkdirAll(filepath.Join(opts.HomeDir, "logs"), 0o755)

    return nil
}

// Backup creates a tar.gz with critical config files and priv_validator_state.json.
// Returns the path to the backup file.
func Backup(opts BackupOptions) (string, error) {
    if opts.HomeDir == "" { return "", fmt.Errorf("HomeDir required") }
    outDir := opts.OutDir
    if outDir == "" { outDir = filepath.Join(opts.HomeDir, "backups") }
    if err := os.MkdirAll(outDir, 0o755); err != nil { return "", err }
    ts := time.Now().Format("20060102-150405")
    outPath := filepath.Join(outDir, fmt.Sprintf("backup-%s.tar.gz", ts))
    f, err := os.Create(outPath)
    if err != nil { return "", err }
    defer f.Close()
    gz := gzip.NewWriter(f)
    defer gz.Close()
    tw := tar.NewWriter(gz)
    defer tw.Close()

    // Include important paths
    include := []string{
        filepath.Join(opts.HomeDir, "config", "config.toml"),
        filepath.Join(opts.HomeDir, "config", "app.toml"),
        filepath.Join(opts.HomeDir, "config", "genesis.json"),
        filepath.Join(opts.HomeDir, "data", "priv_validator_state.json"),
    }
    for _, p := range include {
        if err := addFile(tw, p, opts.HomeDir); err != nil {
            // Skip missing files silently
            _ = err
        }
    }
    return outPath, nil
}

func addFile(tw *tar.Writer, path string, base string) error {
    st, err := os.Stat(path)
    if err != nil { return err }
    if st.IsDir() { return nil }
    rel := strings.TrimPrefix(path, base)
    if strings.HasPrefix(rel, string(filepath.Separator)) { rel = rel[1:] }
    hdr, err := tar.FileInfoHeader(st, "")
    if err != nil { return err }
    hdr.Name = rel
    if err := tw.WriteHeader(hdr); err != nil { return err }
    f, err := os.Open(path)
    if err != nil { return err }
    defer f.Close()
    _, err = io.Copy(tw, f)
    return err
}

