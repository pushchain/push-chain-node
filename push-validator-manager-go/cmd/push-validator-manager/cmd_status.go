package main

import (
    "context"
    "fmt"
    "net/url"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/process"
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

    // Render a simple header box
    title := c.Header(" PUSH VALIDATOR STATUS ")
    sep := c.Separator(44)
    body := fmt.Sprintf(
        "%s\n%s %s\n%s %s\n%s %s\n%s %s\n",
        sep,
        nodeIcon, c.FormatKeyValue("Node", nodeVal),
        rpcIcon, c.FormatKeyValue("RPC", rpcVal),
        syncIcon, c.FormatKeyValue("Sync", syncVal),
        c.Info("ℹ"), c.FormatKeyValue("Height", heightVal),
    )
    fmt.Println(title)
    fmt.Println(body)
}
