package main

import (
    "context"
    "fmt"
    "net/url"
    "strings"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/metrics"
    ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
)

// statusResult models the key process and RPC fields shown by the
// `status` command. It is also used for JSON output when --output=json.
type statusResult struct {
    // Process information
    Running      bool   `json:"running"`
    PID          int    `json:"pid,omitempty"`

    // RPC connectivity
    RPCListening bool   `json:"rpc_listening"`
    RPCURL       string `json:"rpc_url,omitempty"`

    // Sync status
    CatchingUp   bool   `json:"catching_up"`
    Height       int64  `json:"height"`
    RemoteHeight int64  `json:"remote_height,omitempty"`
    SyncProgress float64 `json:"sync_progress,omitempty"` // Percentage (0-100)

    // Network information
    Peers        int    `json:"peers,omitempty"`
    LatencyMS    int64  `json:"latency_ms,omitempty"`

    // Node identity (when available)
    NodeID       string `json:"node_id,omitempty"`
    Moniker      string `json:"moniker,omitempty"`
    Network      string `json:"network,omitempty"` // chain-id

    // Errors
    Error        string `json:"error,omitempty"`
}

// computeStatus gathers process state, RPC listening, catching_up and
// height into a statusResult. It performs a short-timeout RPC call.
func computeStatus(cfg config.Config, sup process.Supervisor) statusResult {
    res := statusResult{}
    res.Running = sup.IsRunning()
    if pid, ok := sup.PID(); ok { res.PID = pid }
    rpc := cfg.RPCLocal
    if rpc == "" { rpc = "http://127.0.0.1:26657" }
    res.RPCURL = rpc
    hostport := "127.0.0.1:26657"
    if u, err := url.Parse(rpc); err == nil && u.Host != "" { hostport = u.Host }
    res.RPCListening = process.IsRPCListening(hostport, 500*time.Millisecond)
    if res.RPCListening {
        cli := node.New(rpc)
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        st, err := cli.Status(ctx)
        if err == nil {
            res.CatchingUp = st.CatchingUp
            res.Height = st.Height
            // Extract node identity from status
            if st.NodeID != "" { res.NodeID = st.NodeID }
            if st.Moniker != "" { res.Moniker = st.Moniker }
            if st.Network != "" { res.Network = st.Network }
            // Enrich with remote height and peers (best-effort)
            remote := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
            col := metrics.New()
            ctx2, cancel2 := context.WithTimeout(context.Background(), 1500*time.Millisecond)
            snap := col.Collect(ctx2, rpc, remote)
            cancel2()
            if snap.Chain.RemoteHeight > 0 {
                res.RemoteHeight = snap.Chain.RemoteHeight
                // Calculate sync progress percentage
                if res.Height > 0 && res.RemoteHeight > 0 {
                    pct := float64(res.Height) / float64(res.RemoteHeight) * 100
                    if pct > 100 { pct = 100 }
                    res.SyncProgress = pct
                }
            }
            if snap.Network.Peers > 0 { res.Peers = snap.Network.Peers }
            if snap.Network.LatencyMS > 0 { res.LatencyMS = snap.Network.LatencyMS }
        } else {
            res.Error = fmt.Sprintf("RPC status error: %v", err)
        }
    }
    return res
}

// printStatusText prints a human-friendly status summary.
func printStatusText(result statusResult) {
    c := ui.NewColorConfig()
    // Build lines with labels and values
    nodeIcon := c.StatusIcon("stopped")
    nodeVal := "Stopped"
    if result.Running {
        nodeIcon = c.StatusIcon("running")
        if result.PID != 0 { nodeVal = fmt.Sprintf("Running (pid %d)", result.PID) } else { nodeVal = "Running" }
    }

    rpcIcon := c.StatusIcon("offline")
    rpcVal := "Not listening"
    if result.RPCListening {
        rpcIcon = c.StatusIcon("online")
        rpcVal = result.RPCURL
    }

    syncIcon := c.StatusIcon("success")
    syncVal := "In Sync"
    if result.CatchingUp {
        syncIcon = c.StatusIcon("syncing")
        syncVal = "Catching Up"
    }

    heightVal := ui.FormatNumber(result.Height)
    if result.Error != "" {
        heightVal = c.Error(result.Error)
    }
    // Progress bar only when catching up
    prog := ""
    if result.CatchingUp && result.RemoteHeight > 0 && result.Height > 0 {
        pct := float64(result.Height) / float64(result.RemoteHeight) * 100
        if pct > 100 { pct = 100 }
        prog = fmt.Sprintf("%s %5.1f%% (%s/%s)", c.ProgressBar(pct, 18), pct,
            ui.FormatNumber(result.Height), ui.FormatNumber(result.RemoteHeight))
    }
    peers := "0"
    if result.Peers == 1 {
        peers = "1 peer connected"
    } else if result.Peers > 1 {
        peers = fmt.Sprintf("%d peers connected", result.Peers)
    }

    // Render a simple header box
    title := c.Header(" PUSH VALIDATOR STATUS ")
    sep := c.Separator(50)

    // Build body with optional Network/NodeID/Moniker
    lines := []string{
        sep,
        fmt.Sprintf("%s %s", nodeIcon, c.FormatKeyValue("Node", nodeVal)),
        fmt.Sprintf("%s %s", rpcIcon, c.FormatKeyValue("RPC", rpcVal)),
        fmt.Sprintf("%s %s", syncIcon, c.FormatKeyValue("Sync", strings.TrimSpace(syncVal+" "+prog))),
        fmt.Sprintf("%s %s", c.Info("ℹ"), c.FormatKeyValue("Height", heightVal)),
    }
    if result.Network != "" {
        lines = append(lines, fmt.Sprintf("%s %s", c.Info("•"), c.FormatKeyValue("Network", result.Network)))
    }
    if result.NodeID != "" {
        lines = append(lines, fmt.Sprintf("%s %s", c.Info("•"), c.FormatKeyValue("NodeID", result.NodeID)))
    }
    if result.Moniker != "" {
        lines = append(lines, fmt.Sprintf("%s %s", c.Info("•"), c.FormatKeyValue("Moniker", result.Moniker)))
    }
    lines = append(lines,
        fmt.Sprintf("%s %s", c.Info("•"), c.FormatKeyValue("Peers", peers)),
    )

    fmt.Println(title)
    fmt.Println(strings.Join(lines, "\n"))

    // Add hint when no peers connected
    if result.Peers == 0 && result.Running && result.RPCListening {
        fmt.Printf("\n%s Check connectivity: push-validator-manager doctor\n", c.Info("ℹ"))
    }
}
