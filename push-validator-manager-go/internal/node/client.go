package node

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"
)

// Client defines the RPC/WS client surface area we depend on.
type Client interface {
    Status(ctx context.Context) (Status, error)
    RemoteStatus(ctx context.Context, baseURL string) (Status, error)
    BlockHash(ctx context.Context, height int64) (string, error)
    Peers(ctx context.Context) ([]Peer, error)
    SubscribeHeaders(ctx context.Context) (<-chan Header, error)
}

type Status struct {
    NodeID     string
    CatchingUp bool
    Height     int64
}

type Peer struct {
    ID   string
    Addr string // host:port
}

type Header struct {
    Height int64
    Time   time.Time
}

type httpClient struct {
    http  *http.Client
    base  string // e.g. http://127.0.0.1:26657
    wsURL string // e.g. ws://127.0.0.1:26657/websocket
}

// New constructs a JSON-RPC client with sane timeouts. If wsURL is empty, it is derived from base.
func New(base string) Client {
    base = strings.TrimRight(base, "/")
    ws := deriveWS(base)
    return &httpClient{
        http: &http.Client{Timeout: 2500 * time.Millisecond},
        base: base,
        wsURL: ws,
    }
}

func deriveWS(base string) string {
    // http://host:port -> ws://host:port/websocket
    // https:// -> wss://
    if strings.HasPrefix(base, "http://") {
        return "ws://" + strings.TrimPrefix(base, "http://") + "/websocket"
    }
    if strings.HasPrefix(base, "https://") {
        return "wss://" + strings.TrimPrefix(base, "https://") + "/websocket"
    }
    // default
    return "ws://" + base + "/websocket"
}

func (c *httpClient) Status(ctx context.Context) (Status, error) {
    return c.RemoteStatus(ctx, c.base)
}

func (c *httpClient) RemoteStatus(ctx context.Context, baseURL string) (Status, error) {
    baseURL = strings.TrimRight(baseURL, "/")
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/status", nil)
    resp, err := c.http.Do(req)
    if err != nil { return Status{}, err }
    defer resp.Body.Close()
    var payload struct {
        Result struct {
            NodeInfo struct{ ID string `json:"id"` } `json:"node_info"`
            SyncInfo struct{
                CatchingUp bool   `json:"catching_up"`
                Height     string `json:"latest_block_height"`
            } `json:"sync_info"`
        } `json:"result"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { return Status{}, err }
    h, _ := strconv.ParseInt(payload.Result.SyncInfo.Height, 10, 64)
    return Status{NodeID: payload.Result.NodeInfo.ID, CatchingUp: payload.Result.SyncInfo.CatchingUp, Height: h}, nil
}

func (c *httpClient) BlockHash(ctx context.Context, height int64) (string, error) {
    u := c.base + "/block"
    if height > 0 {
        q := url.Values{}
        q.Set("height", strconv.FormatInt(height, 10))
        u += "?" + q.Encode()
    }
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    resp, err := c.http.Do(req)
    if err != nil { return "", err }
    defer resp.Body.Close()
    var payload struct{ Result struct{ BlockID struct{ Hash string `json:"hash"` } `json:"block_id"` } `json:"result"` }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { return "", err }
    return strings.ToUpper(payload.Result.BlockID.Hash), nil
}

func (c *httpClient) Peers(ctx context.Context) ([]Peer, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/net_info", nil)
    resp, err := c.http.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    var payload struct {
        Result struct {
            Peers []struct {
                NodeInfo struct {
                    ID         string `json:"id"`
                    ListenAddr string `json:"listen_addr"`
                } `json:"node_info"`
                RemoteIP string `json:"remote_ip"`
            } `json:"peers"`
        } `json:"result"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { return nil, err }
    out := make([]Peer, 0, len(payload.Result.Peers))
    for _, p := range payload.Result.Peers {
        if strings.Contains(p.NodeInfo.ListenAddr, "0.0.0.0") { continue }
        if p.NodeInfo.ID == "" || p.RemoteIP == "" { continue }
        out = append(out, Peer{ID: p.NodeInfo.ID, Addr: fmt.Sprintf("%s:26656", p.RemoteIP)})
    }
    return out, nil
}

func (c *httpClient) SubscribeHeaders(ctx context.Context) (<-chan Header, error) {
    return DialAndSubscribeHeaders(ctx, c.wsURL)
}
