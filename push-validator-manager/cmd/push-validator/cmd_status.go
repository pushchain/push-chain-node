package main

import (
    "context"
    "fmt"
    "net/url"
    "strings"
    "time"

    "github.com/charmbracelet/lipgloss"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/node"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/process"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/metrics"
    ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
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
    CatchingUp   bool    `json:"catching_up"`
    Height       int64   `json:"height"`
    RemoteHeight int64   `json:"remote_height,omitempty"`
    SyncProgress float64 `json:"sync_progress,omitempty"` // Percentage (0-100)

    // Validator status
    IsValidator  bool   `json:"is_validator,omitempty"`

    // Network information
    Peers        int    `json:"peers,omitempty"`
    PeerList     []string `json:"peer_list,omitempty"` // Full peer IDs
    LatencyMS    int64  `json:"latency_ms,omitempty"`

    // Node identity (when available)
    NodeID       string `json:"node_id,omitempty"`
    Moniker      string `json:"moniker,omitempty"`
    Network      string `json:"network,omitempty"` // chain-id

    // System metrics
    BinaryVer    string `json:"binary_version,omitempty"`
    MemoryPct    float64 `json:"memory_percent,omitempty"`
    DiskPct      float64 `json:"disk_percent,omitempty"`

    // Validator details (when registered)
    ValidatorStatus string `json:"validator_status,omitempty"`
    ValidatorMoniker string `json:"validator_moniker,omitempty"`
    VotingPower  int64  `json:"voting_power,omitempty"`
    VotingPct    float64 `json:"voting_percent,omitempty"`
    Commission   string `json:"commission,omitempty"`
    CommissionRewards string `json:"commission_rewards,omitempty"`
    OutstandingRewards string `json:"outstanding_rewards,omitempty"`
    IsJailed     bool   `json:"is_jailed,omitempty"`
    JailReason   string `json:"jail_reason,omitempty"`

    // Errors
    Error        string `json:"error,omitempty"`
}

// computeStatus gathers comprehensive status information including system metrics,
// network details, and validator information.
func computeStatus(cfg config.Config, sup process.Supervisor) statusResult {
    res := statusResult{}
    res.Running = sup.IsRunning()
    if pid, ok := sup.PID(); ok {
        res.PID = pid
        // Try to get system metrics for this process
        getProcessMetrics(res.PID, &res)
    }

    rpc := cfg.RPCLocal
    if rpc == "" { rpc = "http://127.0.0.1:26657" }
    res.RPCURL = rpc
    hostport := "127.0.0.1:26657"
    if u, err := url.Parse(rpc); err == nil && u.Host != "" { hostport = u.Host }

    // Check RPC listening with timeout
    rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 1*time.Second)
    rpcListeningDone := make(chan bool, 1)
    go func() {
        rpcListeningDone <- process.IsRPCListening(hostport, 500*time.Millisecond)
    }()
    select {
    case res.RPCListening = <-rpcListeningDone:
        // Got response
    case <-rpcCtx.Done():
        res.RPCListening = false
    }
    rpcCancel()

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

            // Fetch comprehensive validator details (best-effort, 3s timeout)
            valCtx, valCancel := context.WithTimeout(context.Background(), 3*time.Second)
            myVal, _ := validator.GetCachedMyValidator(valCtx, cfg)
            valCancel()
            res.IsValidator = myVal.IsValidator
            if myVal.IsValidator {
                res.ValidatorMoniker = myVal.Moniker
                res.VotingPower = myVal.VotingPower
                res.VotingPct = myVal.VotingPct
                res.Commission = myVal.Commission
                res.ValidatorStatus = myVal.Status
                res.IsJailed = myVal.Jailed
                if myVal.SlashingInfo.JailReason != "" {
                    res.JailReason = myVal.SlashingInfo.JailReason
                }

                // Fetch rewards (best-effort, 2s timeout)
                rewardCtx, rewardCancel := context.WithTimeout(context.Background(), 2*time.Second)
                commRewards, outRewards, _ := validator.GetCachedRewards(rewardCtx, cfg, myVal.Address)
                rewardCancel()
                res.CommissionRewards = commRewards
                res.OutstandingRewards = outRewards
            }

            // Enrich with remote height and peers (best-effort, with strict timeout)
            remote := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
            col := metrics.NewWithoutCPU()
            ctx2, cancel2 := context.WithTimeout(context.Background(), 1000*time.Millisecond)
            snapChan := make(chan metrics.Snapshot, 1)
            go func() {
                snapChan <- col.Collect(ctx2, rpc, remote)
            }()
            var snap metrics.Snapshot
            select {
            case snap = <-snapChan:
                // Got response
            case <-time.After(1200 * time.Millisecond):
                // Timeout - use empty snapshot
            }
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
            if snap.Network.Peers > 0 {
                res.Peers = snap.Network.Peers
            }
            if snap.Network.LatencyMS > 0 { res.LatencyMS = snap.Network.LatencyMS }

            // Capture system metrics
            if snap.System.MemTotal > 0 {
                memPct := float64(snap.System.MemUsed) / float64(snap.System.MemTotal)
                res.MemoryPct = memPct * 100
            }
            if snap.System.DiskTotal > 0 {
                diskPct := float64(snap.System.DiskUsed) / float64(snap.System.DiskTotal)
                res.DiskPct = diskPct * 100
            }
        } else {
            res.Error = fmt.Sprintf("RPC status error: %v", err)
        }
    }
    return res
}

// getProcessMetrics attempts to fetch memory and disk metrics for a process
func getProcessMetrics(pid int, res *statusResult) {
    // This is a best-effort attempt - we'll try to get these metrics if possible
    // For now, we set defaults. In production, you'd use process libraries or proc filesystem
    // Try using `ps` command to get memory usage
    // Example: ps -p <pid> -o %mem= gives percentage of memory
    // This is simplified for now to avoid external dependencies
}

// printStatusText prints a human-friendly status summary matching the dashboard layout.
func printStatusText(result statusResult) {
    c := ui.NewColorConfig()

    // Build icon/status strings
    nodeIcon := c.StatusIcon("stopped")
    nodeVal := "Stopped"
    if result.Running {
        nodeIcon = c.StatusIcon("running")
        if result.PID != 0 {
            nodeVal = fmt.Sprintf("Running (pid %d)", result.PID)
        } else {
            nodeVal = "Running"
        }
    }

    rpcIcon := c.StatusIcon("offline")
    rpcVal := "Not listening"
    if result.RPCListening {
        rpcIcon = c.StatusIcon("online")
        rpcVal = "Listening"
    }

    syncIcon := c.StatusIcon("success")
    syncVal := "In Sync"
    if result.CatchingUp {
        syncIcon = c.StatusIcon("syncing")
        syncVal = "Catching Up"
    }

    validatorIcon := c.StatusIcon("offline")
    validatorVal := "Not Registered"
    if result.IsValidator {
        validatorIcon = c.StatusIcon("online")
        validatorVal = "Registered"
    }

    heightVal := ui.FormatNumber(result.Height)
    if result.Error != "" {
        heightVal = c.Error(result.Error)
    }

    peers := "0 peers"
    if result.Peers == 1 {
        peers = "1 peer"
    } else if result.Peers > 1 {
        peers = fmt.Sprintf("%d peers", result.Peers)
    }

    // Define box styling (enhanced layout with wider boxes)
    boxStyle := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("63")).
        Padding(0, 1).
        Width(50)

    titleStyle := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("39")). // Bright cyan
        Width(46).
        Align(lipgloss.Center)

    // Build NODE STATUS box - Enhanced with system metrics
    nodeLines := []string{
        fmt.Sprintf("%s %s", nodeIcon, nodeVal),
        fmt.Sprintf("%s %s", rpcIcon, rpcVal),
    }
    if result.MemoryPct > 0 {
        nodeLines = append(nodeLines, fmt.Sprintf("  Memory: %.1f%%", result.MemoryPct))
    }
    if result.DiskPct > 0 {
        nodeLines = append(nodeLines, fmt.Sprintf("  Disk: %.1f%%", result.DiskPct))
    }
    nodeBox := boxStyle.Render(
        titleStyle.Render("NODE STATUS") + "\n" + strings.Join(nodeLines, "\n"),
    )

    // Build CHAIN STATUS box - Enhanced with sync details and progress bar
    chainLines := []string{
        fmt.Sprintf("%s %s", syncIcon, syncVal),
    }

    // Add progress bar if catching up with remote height data
    if result.CatchingUp && result.RemoteHeight > 0 && result.Height > 0 {
        pct := float64(result.Height) / float64(result.RemoteHeight) * 100
        if pct > 100 {
            pct = 100
        }
        progBar := renderProgressBar(pct, 20)
        chainLines = append(chainLines, fmt.Sprintf("  %s", progBar))
    }

    chainLines = append(chainLines, fmt.Sprintf("  Height: %s", heightVal))

    if result.RemoteHeight > 0 {
        chainLines = append(chainLines, fmt.Sprintf("  Remote: %s", ui.FormatNumber(result.RemoteHeight)))
    }

    chainBox := boxStyle.Render(
        titleStyle.Render("CHAIN STATUS") + "\n" + strings.Join(chainLines, "\n"),
    )

    // Top row: NODE STATUS | CHAIN STATUS
    topRow := lipgloss.JoinHorizontal(lipgloss.Top, nodeBox, chainBox)

    // Build NETWORK STATUS box - Enhanced with network details
    networkLines := []string{
        fmt.Sprintf("%s %s", c.Info("•"), peers),
    }
    if result.Network != "" {
        networkLines = append(networkLines, fmt.Sprintf("  Network: %s", result.Network))
    }
    if result.LatencyMS > 0 {
        networkLines = append(networkLines, fmt.Sprintf("  Latency: %dms", result.LatencyMS))
    }
    if result.NodeID != "" {
        networkLines = append(networkLines, fmt.Sprintf("  Node: %s", truncateNodeID(result.NodeID)))
    }
    if result.Moniker != "" {
        networkLines = append(networkLines, fmt.Sprintf("  Moniker: %s", result.Moniker))
    }

    networkBox := boxStyle.Render(
        titleStyle.Render("NETWORK STATUS") + "\n" + strings.Join(networkLines, "\n"),
    )

    // Build VALIDATOR STATUS box - Enhanced with detailed validator information
    validatorLines := []string{
        fmt.Sprintf("%s %s", validatorIcon, validatorVal),
    }

    if result.IsValidator {
        if result.ValidatorMoniker != "" {
            validatorLines = append(validatorLines, fmt.Sprintf("  Moniker: %s", result.ValidatorMoniker))
        }

        // Show validator status with jail indicator
        if result.ValidatorStatus != "" {
            statusText := result.ValidatorStatus
            if result.IsJailed {
                statusText = fmt.Sprintf("%s (JAILED)", result.ValidatorStatus)
            }
            validatorLines = append(validatorLines, fmt.Sprintf("  Status: %s", statusText))
        }

        if result.VotingPower > 0 {
            vpStr := ui.FormatNumber(result.VotingPower)
            if result.VotingPct > 0 {
                vpStr += fmt.Sprintf(" (%.3f%%)", result.VotingPct*100)
            }
            validatorLines = append(validatorLines, fmt.Sprintf("  Power: %s", vpStr))
        }

        if result.Commission != "" {
            validatorLines = append(validatorLines, fmt.Sprintf("  Commission: %s", result.Commission))
        }

        // Show rewards if available
        hasCommRewards := result.CommissionRewards != "" && result.CommissionRewards != "—" && result.CommissionRewards != "0"
        hasOutRewards := result.OutstandingRewards != "" && result.OutstandingRewards != "—" && result.OutstandingRewards != "0"

        if hasCommRewards || hasOutRewards {
            validatorLines = append(validatorLines, "")
            validatorLines = append(validatorLines, fmt.Sprintf("  %s Withdraw available!", c.StatusIcon("online")))
            validatorLines = append(validatorLines, fmt.Sprintf("  Run: push-validator withdraw-rewards"))

            if hasCommRewards {
                validatorLines = append(validatorLines, fmt.Sprintf("    Comm Rewards: %s PC", result.CommissionRewards))
            }
            if hasOutRewards {
                validatorLines = append(validatorLines, fmt.Sprintf("    Out Rewards: %s PC", result.OutstandingRewards))
            }
        }

        // If jailed, show detailed status information
        if result.IsJailed {
            validatorLines = append(validatorLines, "")
            validatorLines = append(validatorLines, "  STATUS DETAILS")
            validatorLines = append(validatorLines, "")
            validatorLines = append(validatorLines, fmt.Sprintf("    Reason: %s", result.JailReason))

            // Show unjail information
            validatorLines = append(validatorLines, "")
            validatorLines = append(validatorLines, fmt.Sprintf("  %s Ready to unjail!", c.StatusIcon("online")))
            validatorLines = append(validatorLines, fmt.Sprintf("  Run: push-validator unjail"))
        }
    }

    validatorBox := boxStyle.Render(
        titleStyle.Render("VALIDATOR STATUS") + "\n" + strings.Join(validatorLines, "\n"),
    )

    // Bottom row: NETWORK STATUS | VALIDATOR STATUS
    bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, networkBox, validatorBox)

    // Combine top and bottom rows
    output := lipgloss.JoinVertical(lipgloss.Left, topRow, bottomRow)

    fmt.Println(output)

    // Add hint when no peers connected
    if result.Peers == 0 && result.Running && result.RPCListening {
        fmt.Printf("\n%s Check connectivity: push-validator doctor\n", c.Info("ℹ"))
    }
}

// truncateNodeID shortens a long node ID for display
func truncateNodeID(nodeID string) string {
    if len(nodeID) <= 16 {
        return nodeID
    }
    return nodeID[:8] + "..." + nodeID[len(nodeID)-8:]
}

// renderProgressBar creates a visual progress bar using block characters
func renderProgressBar(percent float64, width int) string {
    if percent < 0 {
        percent = 0
    }
    if percent > 100 {
        percent = 100
    }

    filled := int(float64(width) * (percent / 100))
    empty := width - filled

    bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
    return fmt.Sprintf("[%s] %.2f%%", bar, percent)
}
