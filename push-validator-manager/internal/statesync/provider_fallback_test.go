package statesync

import (
    "context"
    "encoding/json"
    "net"
    "net/http"
    "net/http/httptest"
    "testing"
)

// Test fallback across candidate heights and endpoint (/commit).
func TestProvider_ComputeTrust_Fallbacks(t *testing.T) {
    // Some sandboxes restrict binding; detect and skip.
    probe, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Skipf("skipping: cannot bind due to sandbox: %v", err) }
    probe.Close()
    // Latest height via /status (35000 for conservative offsets 10-25)
    mux := http.NewServeMux()
    mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{"result": map[string]any{"sync_info": map[string]any{"latest_block_height": "35000"}}}
        _ = json.NewEncoder(w).Encode(resp)
    })
    // With snapshotInterval=1000 and offsets 10-25, candidates are: 25000, 24000, 23000, ...
    // /block?height=25000 -> simulate pruned (404)
    // /block?height=24000 -> simulate decode error to force /commit fallback
    // /commit?height=24000 -> success with hash
    mux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
        h := r.URL.Query().Get("height")
        switch h {
        case "": // not used in this test
            resp := map[string]any{"result": map[string]any{"block": map[string]any{"header": map[string]any{"height": "35000"}}}}
            _ = json.NewEncoder(w).Encode(resp)
        case "25000":
            http.NotFound(w, r)
        case "24000":
            // Malformed body to force fallback to /commit
            _, _ = w.Write([]byte("{bad json"))
        default:
            resp := map[string]any{"result": map[string]any{"block_id": map[string]any{"hash": "zzz"}}}
            _ = json.NewEncoder(w).Encode(resp)
        }
    })
    mux.HandleFunc("/commit", func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Query().Get("height") == "24000" {
            resp := map[string]any{
                "result": map[string]any{
                    "signed_header": map[string]any{
                        "commit": map[string]any{
                            "block_id": map[string]any{"hash": "def456"},
                        },
                    },
                },
            }
            _ = json.NewEncoder(w).Encode(resp)
            return
        }
        http.NotFound(w, r)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    p := New()
    tp, err := p.ComputeTrust(context.Background(), srv.URL)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    // With latest=35000, offset=10 gives 25000 (fails), offset=11 gives 24000 (succeeds via /commit)
    if tp.Height != 24000 {
        t.Fatalf("expected fallback trust height 24000, got %d", tp.Height)
    }
    if tp.Hash != "DEF456" {
        t.Fatalf("expected hash DEF456, got %s", tp.Hash)
    }
}
