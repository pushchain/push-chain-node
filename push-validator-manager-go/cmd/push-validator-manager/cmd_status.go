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
    Running      bool   `json:"running"`
    PID          int    `json:"pid,omitempty"`
    RPCListening bool   `json:"rpc_listening"`
    CatchingUp   bool   `json:"catching_up"`
    Height       int64  `json:"height"`
    RemoteHeight int64  `json:"remote_height,omitempty"`
    Peers        int    `json:"peers,omitempty"`
    LatencyMS    int64  `json:"latency_ms,omitempty"`
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
            // Enrich with remote height and peers (best-effort)
            remote := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
            col := metrics.New()
            ctx2, cancel2 := context.WithTimeout(context.Background(), 1500*time.Millisecond)
            snap := col.Collect(ctx2, rpc, remote)
            cancel2()
            if snap.Chain.RemoteHeight > 0 { res.RemoteHeight = snap.Chain.RemoteHeight }
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
        rpcVal = "Listening (:26657)"
    }

    syncIcon := c.StatusIcon("success")
    syncVal := "In Sync"
    if result.CatchingUp {
        syncIcon = c.StatusIcon("syncing")
        syncVal = "Catching Up"
    }

    heightVal := fmt.Sprintf("%d", result.Height)
    if result.Error != "" {
        heightVal = c.Error(result.Error)
    }
    // Progress bar when remote height known
    prog := ""
    if result.RemoteHeight > 0 && result.Height > 0 {
        pct := float64(result.Height) / float64(result.RemoteHeight) * 100
        if pct > 100 { pct = 100 }
        prog = fmt.Sprintf("%s %5.1f%%", c.ProgressBar(pct, 22), pct)
    }
    peers := "-"
    if result.Peers > 0 { peers = fmt.Sprintf("%d connected", result.Peers) }
    latency := "-"
    if result.LatencyMS > 0 { latency = fmt.Sprintf("%d ms", result.LatencyMS) }

    // Render a simple header box
    title := c.Header(" PUSH VALIDATOR STATUS ")
    sep := c.Separator(44)
    body := fmt.Sprintf(
        "%s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n",
        sep,
        nodeIcon, c.FormatKeyValue("Node", nodeVal),
        rpcIcon, c.FormatKeyValue("RPC", rpcVal),
        syncIcon, c.FormatKeyValue("Sync", strings.TrimSpace(syncVal+" "+prog)),
        c.Info("ℹ"), c.FormatKeyValue("Height", heightVal),
        c.Info("•"), c.FormatKeyValue("Peers", peers),
        c.Info("•"), c.FormatKeyValue("Latency", latency),
    )
    fmt.Println(title)
    fmt.Println(body)
}
