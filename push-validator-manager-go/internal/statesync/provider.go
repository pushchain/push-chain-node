package statesync

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"
)

// Provider computes trust params from a remote snapshot-enabled RPC.
type Provider interface {
    ComputeTrust(ctx context.Context, rpcURL string) (TrustParams, error)
}

type TrustParams struct {
    Height int64
    Hash   string
}

type provider struct{ http *http.Client }

// New returns a provider with sane timeouts.
func New() Provider { return &provider{http: &http.Client{Timeout: 6 * time.Second}} }

func (p *provider) ComputeTrust(ctx context.Context, rpcURL string) (TrustParams, error) {
    base := strings.TrimRight(rpcURL, "/")
    // Get latest height with fallback: try /status first, then /block
    latestHeight, err := p.latestHeight(ctx, base)
    if err != nil { return TrustParams{}, err }
    if latestHeight < 2 { latestHeight = 2 }

    // Snapshots are taken at 1000-block intervals
    // We need to align trust heights with these intervals
    snapshotInterval := int64(1000)

    // Try recent snapshot intervals (1-5 intervals back from latest)
    // This ensures we target actual snapshot heights
    offsetIntervals := []int{1, 2, 3, 4, 5}
    var lastErr error

    for _, intervals := range offsetIntervals {
        // Calculate snapshot-aligned height
        // E.g., if latest is 1136240 and intervals=1: (1136240/1000 - 1) * 1000 = 1135000
        trustH := ((latestHeight / snapshotInterval) - int64(intervals)) * snapshotInterval
        if trustH < snapshotInterval { trustH = snapshotInterval }

        hash, err := p.blockHash(ctx, base, trustH)
        if err == nil && hash != "" {
            return TrustParams{Height: trustH, Hash: strings.ToUpper(hash)}, nil
        }
        lastErr = err
    }
    if lastErr == nil { lastErr = errors.New("could not determine trust hash from RPC") }
    return TrustParams{}, lastErr
}

// latestHeight attempts to read latest height via /status (preferred) then /block.
func (p *provider) latestHeight(ctx context.Context, base string) (int64, error) {
    // Try /status
    if h, err := p.latestFromStatus(ctx, base); err == nil && h > 0 {
        return h, nil
    }
    // Fallback to /block
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/block", nil)
    resp, err := p.doWithRetry(req)
    if err != nil { return 0, err }
    defer resp.Body.Close()
    var latest struct { Result struct { Block struct { Header struct { Height string `json:"height"` } `json:"header"` } `json:"block"` } `json:"result"` }
    if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil { return 0, err }
    h, _ := strconv.ParseInt(latest.Result.Block.Header.Height, 10, 64)
    return h, nil
}

func (p *provider) latestFromStatus(ctx context.Context, base string) (int64, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/status", nil)
    resp, err := p.doWithRetry(req)
    if err != nil { return 0, err }
    defer resp.Body.Close()
    var payload struct { Result struct { SyncInfo struct { Height string `json:"latest_block_height"` } `json:"sync_info"` } `json:"result"` }
    if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { return 0, err }
    if payload.Result.SyncInfo.Height == "" { return 0, errors.New("empty height") }
    h, err := strconv.ParseInt(payload.Result.SyncInfo.Height, 10, 64)
    if err != nil { return 0, err }
    return h, nil
}

// blockHash fetches the block hash for a given height, trying /block then /commit.
func (p *provider) blockHash(ctx context.Context, base string, height int64) (string, error) {
    q := url.Values{"height": []string{strconv.FormatInt(height, 10)}}.Encode()
    // Try /block?height=
    u := base + "/block?" + q
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if resp, err := p.doWithRetry(req); err == nil {
        defer resp.Body.Close()
        var blk struct { Result struct { BlockID struct { Hash string `json:"hash"` } `json:"block_id"` } `json:"result"` }
        if err := json.NewDecoder(resp.Body).Decode(&blk); err == nil && blk.Result.BlockID.Hash != "" { return blk.Result.BlockID.Hash, nil }
    }
    // Fallback to /commit?height=
    u2 := base + "/commit?" + q
    req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, u2, nil)
    resp2, err := p.doWithRetry(req2)
    if err != nil { return "", err }
    defer resp2.Body.Close()
    var cm struct { Result struct { SignedHeader struct { Commit struct { BlockID struct { Hash string `json:"hash"` } `json:"block_id"` } `json:"commit"` } `json:"signed_header"` } `json:"result"` }
    if err := json.NewDecoder(resp2.Body).Decode(&cm); err != nil { return "", err }
    return cm.Result.SignedHeader.Commit.BlockID.Hash, nil
}

// doWithRetry performs a request with small retries for transient failures.
func (p *provider) doWithRetry(req *http.Request) (*http.Response, error) {
    var lastErr error
    for i := 0; i < 3; i++ {
        resp, err := p.http.Do(req)
        if err == nil && resp != nil && resp.StatusCode == 200 {
            return resp, nil
        }
        if resp != nil && resp.Body != nil { resp.Body.Close() }
        lastErr = err
        time.Sleep(time.Duration(100*(i+1)) * time.Millisecond)
    }
    if lastErr == nil { lastErr = errors.New("request failed") }
    return nil, lastErr
}
