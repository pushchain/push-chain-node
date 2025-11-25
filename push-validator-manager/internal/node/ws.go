package node

import (
    "context"
    "encoding/json"
    "fmt"
    "net/url"
    "time"

    "github.com/gorilla/websocket"
)

// DialAndSubscribeHeaders uses gorilla/websocket to subscribe to NewBlockHeader events and stream heights.
func DialAndSubscribeHeaders(ctx context.Context, wsURL string) (<-chan Header, error) {
    u, err := url.Parse(wsURL)
    if err != nil { return nil, err }
    if u.Path == "" { u.Path = "/websocket" }

    d := websocket.Dialer{
        Subprotocols:   []string{"jsonrpc"},
        HandshakeTimeout: 5 * time.Second,
        EnableCompression: false,
    }
    // nolint:bodyclose
    conn, _, err := d.DialContext(ctx, u.String(), map[string][]string{"Origin": {"http://localhost"}})
    if err != nil { return nil, err }

    // Send subscribe request
    sub := map[string]any{
        "jsonrpc": "2.0",
        "method":  "subscribe",
        // Prefer cometbft.event key for 0.38+; servers typically support both.
        "params":  map[string]string{"query": "cometbft.event='NewBlockHeader'"},
        "id":      1,
    }
    if err := conn.WriteJSON(sub); err != nil { _ = conn.Close(); return nil, err }

    out := make(chan Header, 32)
    go func() {
        defer close(out)
        defer func() {
            // attempt proper close handshake
            deadline := time.Now().Add(1500 * time.Millisecond)
            _ = conn.SetWriteDeadline(deadline)
            _ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), deadline)
            // best-effort wait for server close
            _ = conn.SetReadDeadline(deadline)
            _, _, _ = conn.ReadMessage()
            _ = conn.Close()
        }()
        for {
            select {
            case <-ctx.Done():
                return
            default:
            }
            // Read next message
            // Read next message (gorilla handles ping/pong)
            _, msg, err := conn.ReadMessage()
            if err != nil {
                // graceful exits on normal closure or going away
                if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
                    return
                }
                // any other error: return and let defer handle close
                return
            }
            if h, ok := parseHeaderHeight(msg); ok { out <- h; continue }
            // handle pong/ping implicitly via gorilla; ignore others
        }
    }()
    return out, nil
}

func parseHeaderHeight(b []byte) (Header, bool) {
    var payload struct {
        Result struct {
            Data struct {
                Value struct {
                    Header struct {
                        Height string    `json:"height"`
                        Time   time.Time `json:"time"`
                    } `json:"header"`
                } `json:"value"`
            } `json:"data"`
        } `json:"result"`
    }
    if err := json.Unmarshal(b, &payload); err != nil { return Header{}, false }
    if payload.Result.Data.Value.Header.Height == "" { return Header{}, false }
    // Accept both tm.event and cometbft.event streams; height parse only
    h, err := strconvParseInt(payload.Result.Data.Value.Header.Height)
    if err != nil { return Header{}, false }
    return Header{Height: h, Time: payload.Result.Data.Value.Header.Time}, true
}

func strconvParseInt(s string) (int64, error) {
    var n int64
    var sign int64 = 1
    if s == "" { return 0, fmt.Errorf("empty") }
    if s[0] == '-' { sign = -1; s = s[1:] }
    for i := 0; i < len(s); i++ {
        c := s[i]
        if c < '0' || c > '9' { return 0, fmt.Errorf("invalid") }
        n = n*10 + int64(c-'0')
    }
    return sign * n, nil
}
