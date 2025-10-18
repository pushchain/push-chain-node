package syncmon

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"
    "net"
)

func TestRenderProgress_FloorClamp(t *testing.T) {
    // 99.97% should floor to 99.9, not 100.0
    cur, remote := int64(9997), int64(10000)
    percent := float64(cur) / float64(remote) * 100
    percent = floor1(percent)
    if percent >= 100.0 { t.Fatalf("percent should not be 100, got %.1f", percent) }
}

func TestIsSyncedQuick(t *testing.T) {
    // skip in sandbox environments that restrict binding
    if _, err := net.Listen("tcp", "127.0.0.1:0"); err != nil {
        t.Skip("skipping due to sandbox")
    }
    mux := http.NewServeMux()
    mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"result":{"sync_info":{"catching_up":false}}}`)) })
    srv := httptest.NewServer(mux)
    defer srv.Close()
    if !isSyncedQuick(srv.URL) { t.Fatal("expected synced true") }
}

func TestTailStatesync_FiltersAndSignals(t *testing.T) {
    dir := t.TempDir()
    logPath := filepath.Join(dir, "pchaind.log")
    f, err := os.Create(logPath)
    if err != nil { t.Fatal(err) }
    defer f.Close()
    ch := make(chan string, 4)
    stop := make(chan struct{})
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go tailStatesync(ctx, logPath, ch, stop)
    // Write non-matching line
    time.Sleep(50 * time.Millisecond)
    f.WriteString("random line\n")
    f.Sync()
    // Write matching snapshot line
    f.WriteString("Snapshot accepted, restoring...\n")
    f.Sync()
    // Expect to receive the filtered line
    select {
    case s := <-ch:
        if s == "" { t.Fatal("empty line") }
    case <-time.After(500 * time.Millisecond):
        t.Fatal("timeout waiting for tail")
    }
    close(stop)
}
