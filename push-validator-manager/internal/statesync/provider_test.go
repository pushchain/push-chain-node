package statesync

import (
    "context"
    "encoding/json"
    "net"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestProvider_ComputeTrust(t *testing.T) {
    // Simulate /block latest height and /block?height=<trust>
    // Some sandboxes restrict binding; detect and skip.
    probe, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Skipf("skipping: cannot bind due to sandbox: %v", err) }
    probe.Close()

    mux := http.NewServeMux()
    mux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
        if h := r.URL.Query().Get("height"); h != "" {
            // Return block_id.hash for requested height
            resp := map[string]any{"result": map[string]any{"block_id": map[string]any{"hash": "abc123"}}}
            _ = json.NewEncoder(w).Encode(resp)
            return
        }
        // Latest height = 5000
        resp := map[string]any{"result": map[string]any{"block": map[string]any{"header": map[string]any{"height": "5000"}}}}
        _ = json.NewEncoder(w).Encode(resp)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    p := New()
    tp, err := p.ComputeTrust(context.Background(), srv.URL)
    if err != nil { t.Fatal(err) }
    if want := int64(4000); tp.Height != want { t.Fatalf("trust height: got %d want %d", tp.Height, want) }
    if tp.Hash != "ABC123" { t.Fatalf("trust hash uppercased: got %s", tp.Hash) }
}
