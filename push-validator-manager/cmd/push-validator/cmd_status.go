package main

import (
    "context"
    "fmt"
    "net/url"
    "os/exec"
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
    JailedUntil  string `json:"jailed_until,omitempty"`     // RFC3339 timestamp
    MissedBlocks int64  `json:"missed_blocks,omitempty"`
    Tombstoned   bool   `json:"tombstoned,omitempty"`

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

                // Add detailed jail information
                if myVal.SlashingInfo.JailedUntil != "" {
                    res.JailedUntil = myVal.SlashingInfo.JailedUntil
                }
                if myVal.SlashingInfo.MissedBlocks > 0 {
                    res.MissedBlocks = myVal.SlashingInfo.MissedBlocks
                }
                res.Tombstoned = myVal.SlashingInfo.Tombstoned

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

            // Fetch peer list for detailed display (best-effort, 2s timeout)
            peerCtx, peerCancel := context.WithTimeout(context.Background(), 2*time.Second)
            peers, _ := cli.Peers(peerCtx)
            peerCancel()
            if len(peers) > 0 {
                for _, p := range peers {
                    res.PeerList = append(res.PeerList, p.ID)
                }
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

    // Fetch binary version (best-effort)
    res.BinaryVer = getBinaryVersion(cfg)

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

// getBinaryVersion fetches the binary version string from pchaind
func getBinaryVersion(cfg config.Config) string {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    cmd := exec.CommandContext(ctx, "pchaind", "version", "--long")
    output, err := cmd.Output()
    if err != nil {
        return ""
    }

    // Parse version from output
    // Format is usually "version: v0.x.x-..." on first line
    lines := strings.Split(string(output), "\n")
    for _, line := range lines {
        if strings.HasPrefix(strings.TrimSpace(line), "version") {
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                return strings.TrimSpace(parts[1])
            }
        }
    }

    return ""
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
        Width(80)

    titleStyle := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("39")). // Bright cyan
        Width(76).
        Align(lipgloss.Center)

    // Build NODE STATUS box - Enhanced with system metrics and version
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
    if result.BinaryVer != "" {
        nodeLines = append(nodeLines, fmt.Sprintf("  Version: %s", result.BinaryVer))
    }
    nodeBox := boxStyle.Render(
        titleStyle.Render("NODE STATUS") + "\n" + strings.Join(nodeLines, "\n"),
    )

    // Build CHAIN STATUS box - Dashboard-style with progress bar and block counts
    chainLines := []string{}

    if result.RPCListening && result.RemoteHeight > 0 {
        // Use dashboard-style progress rendering with block counts
        syncLine := renderSyncProgressDashboard(result.Height, result.RemoteHeight, result.CatchingUp)
        chainLines = append(chainLines, syncLine)
    } else {
        // Fallback to simple format if RPC not available
        chainLines = append(chainLines, fmt.Sprintf("%s %s", syncIcon, syncVal))
        if result.Height > 0 {
            chainLines = append(chainLines, fmt.Sprintf("Height: %s", heightVal))
        }
    }

    chainBox := boxStyle.Render(
        titleStyle.Render("CHAIN STATUS") + "\n" + strings.Join(chainLines, "\n"),
    )

    // Top row: NODE STATUS | CHAIN STATUS
    topRow := lipgloss.JoinHorizontal(lipgloss.Top, nodeBox, chainBox)

    // Build NETWORK STATUS box - Enhanced with full peer list
    networkLines := []string{}

    if len(result.PeerList) > 0 {
        networkLines = append(networkLines, fmt.Sprintf("Connected to %d peers:", len(result.PeerList)))
        maxDisplay := 3  // Show first 3 peers like dashboard
        for i, peer := range result.PeerList {
            if i >= maxDisplay {
                networkLines = append(networkLines, fmt.Sprintf("  ... and %d more", len(result.PeerList)-maxDisplay))
                break
            }
            networkLines = append(networkLines, fmt.Sprintf("  %s", peer))
        }
    } else {
        networkLines = append(networkLines, fmt.Sprintf("%s %s", c.Info("•"), peers))
    }

    if result.LatencyMS > 0 {
        networkLines = append(networkLines, fmt.Sprintf("Latency: %dms", result.LatencyMS))
    }
    if result.Network != "" {
        networkLines = append(networkLines, fmt.Sprintf("Chain: %s", result.Network))
    }
    if result.NodeID != "" {
        networkLines = append(networkLines, fmt.Sprintf("Node: %s", result.NodeID))
    }
    if result.Moniker != "" {
        networkLines = append(networkLines, fmt.Sprintf("Name: %s", result.Moniker))
    }

    networkBox := boxStyle.Render(
        titleStyle.Render("NETWORK STATUS") + "\n" + strings.Join(networkLines, "\n"),
    )

    // Build VALIDATOR STATUS box - Enhanced with two-column layout when jailed
    var validatorBoxContent string

    if result.IsValidator && result.IsJailed {
        // Two-column layout for jailed validators (matching dashboard)

        // LEFT column: Basic validator info and rewards
        leftLines := []string{
            fmt.Sprintf("%s %s", validatorIcon, validatorVal),
        }

        if result.ValidatorMoniker != "" {
            leftLines = append(leftLines, fmt.Sprintf("  Moniker: %s", result.ValidatorMoniker))
        }

        // Show basic status on left
        if result.ValidatorStatus != "" {
            leftLines = append(leftLines, fmt.Sprintf("  ★ Status: %s", result.ValidatorStatus))
        }

        if result.VotingPower > 0 {
            vpStr := ui.FormatNumber(result.VotingPower)
            if result.VotingPct > 0 {
                vpStr += fmt.Sprintf(" (%.3f%%)", result.VotingPct*100)
            }
            leftLines = append(leftLines, fmt.Sprintf("  Power: %s", vpStr))
        }

        if result.Commission != "" {
            leftLines = append(leftLines, fmt.Sprintf("  Commission: %s", result.Commission))
        }

        // Show rewards if available
        hasCommRewards := result.CommissionRewards != "" && result.CommissionRewards != "—" && result.CommissionRewards != "0"
        hasOutRewards := result.OutstandingRewards != "" && result.OutstandingRewards != "—" && result.OutstandingRewards != "0"

        if hasCommRewards || hasOutRewards {
            // Add reward amounts first
            if hasCommRewards {
                leftLines = append(leftLines, fmt.Sprintf("  Comm Rewards: %s", result.CommissionRewards))
            }
            if hasOutRewards {
                leftLines = append(leftLines, fmt.Sprintf("  Outstanding Rewards: %s", result.OutstandingRewards))
            }

            leftLines = append(leftLines, "")
            // Create command style for colored output
            commandStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
            leftLines = append(leftLines, fmt.Sprintf("  %s %s", c.StatusIcon("online"), commandStyle.Render("Withdraw available!")))
            leftLines = append(leftLines, commandStyle.Render("  Run: push-validator withdraw-rewards"))
        }

        // RIGHT column: Status details
        rightLines := []string{
            "STATUS DETAILS",
        }
        rightLines = append(rightLines, "")

        // Show status with jail indicator on right
        statusText := fmt.Sprintf("%s (JAILED)", result.ValidatorStatus)
        rightLines = append(rightLines, statusText)
        rightLines = append(rightLines, "")

        if result.JailReason != "" {
            rightLines = append(rightLines, fmt.Sprintf("  Reason: %s", result.JailReason))
        }

        // Add missed blocks if available
        if result.MissedBlocks > 0 {
            rightLines = append(rightLines, fmt.Sprintf("  Missed: %s blks", ui.FormatNumber(result.MissedBlocks)))
        }

        // Add tombstoned status if applicable
        if result.Tombstoned {
            rightLines = append(rightLines, fmt.Sprintf("  %s Tombstoned: Yes", c.StatusIcon("offline")))
        }

        // Add jail until time if available
        if result.JailedUntil != "" {
            formatted := formatTimestamp(result.JailedUntil)
            if formatted != "" {
                rightLines = append(rightLines, fmt.Sprintf("  Until: %s", formatted))
            }

            // Add time remaining if applicable
            remaining := timeUntil(result.JailedUntil)
            if remaining != "" && remaining != "0s" {
                rightLines = append(rightLines, fmt.Sprintf("  Remaining: %s", remaining))
            } else if remaining == "0s" || remaining == "" {
                rightLines = append(rightLines, fmt.Sprintf("  Remaining: 0s (Ready"))
                rightLines = append(rightLines, fmt.Sprintf("  now!)"))
            }
        }

        // Show unjail information
        rightLines = append(rightLines, "")
        // Create command style for colored output
        commandStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
        rightLines = append(rightLines, fmt.Sprintf("  %s %s", c.StatusIcon("online"), commandStyle.Render("Ready to unjail!")))
        rightLines = append(rightLines, commandStyle.Render("  Run: push-validator unjail"))

        // Build two-column layout
        leftContent := strings.Join(leftLines, "\n")
        rightContent := strings.Join(rightLines, "\n")

        // Calculate column widths: assume box is ~78 chars wide (80 - 2 borders)
        // Split roughly in half with 2-char spacing between
        const boxInnerWidth = 78
        leftWidth := (boxInnerWidth / 2) - 1  // ~38 chars
        rightWidth := boxInnerWidth - leftWidth - 2 // ~38 chars with 2-space separator

        // Use lipgloss to join columns horizontally
        leftStyle := lipgloss.NewStyle().Width(leftWidth)
        rightStyle := lipgloss.NewStyle().Width(rightWidth)

        leftRendered := leftStyle.Render(leftContent)
        rightRendered := rightStyle.Render(rightContent)

        validatorBoxContent = titleStyle.Render("MY VALIDATOR STATUS") + "\n" +
            lipgloss.JoinHorizontal(lipgloss.Top, leftRendered, "  ", rightRendered)
    } else {
        // Single column layout for non-jailed or non-registered validators
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
                // Add reward amounts first
                if hasCommRewards {
                    validatorLines = append(validatorLines, fmt.Sprintf("  Comm Rewards: %s PC", result.CommissionRewards))
                }
                if hasOutRewards {
                    validatorLines = append(validatorLines, fmt.Sprintf("  Outstanding Rewards: %s PC", result.OutstandingRewards))
                }

                validatorLines = append(validatorLines, "")
                // Create command style for colored output
                commandStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
                validatorLines = append(validatorLines, fmt.Sprintf("  %s %s", c.StatusIcon("online"), commandStyle.Render("Withdraw available!")))
                validatorLines = append(validatorLines, commandStyle.Render("  Run: push-validator withdraw-rewards"))
            }
        }

        validatorBoxContent = titleStyle.Render("MY VALIDATOR STATUS") + "\n" + strings.Join(validatorLines, "\n")
    }

    validatorBox := boxStyle.Render(validatorBoxContent)

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

// formatWithCommas adds comma separators to large numbers
func formatWithCommas(n int64) string {
    if n < 1000 {
        return fmt.Sprintf("%d", n)
    }
    s := fmt.Sprintf("%d", n)
    var result string
    for i, c := range s {
        if i > 0 && (len(s)-i)%3 == 0 {
            result += ","
        }
        result += string(c)
    }
    return result
}

// formatTimestamp converts RFC3339 timestamp to "Jan 02, 03:04 PM MST" format
func formatTimestamp(rfcTime string) string {
    if rfcTime == "" {
        return ""
    }
    t, err := time.Parse(time.RFC3339Nano, rfcTime)
    if err != nil {
        return ""
    }
    return t.Local().Format("Jan 02, 03:04 PM MST")
}

// timeUntil calculates human-readable time remaining until a given RFC3339 timestamp
func timeUntil(rfcTime string) string {
    if rfcTime == "" {
        return ""
    }
    t, err := time.Parse(time.RFC3339Nano, rfcTime)
    if err != nil {
        return ""
    }
    remaining := time.Until(t)
    if remaining <= 0 {
        return "0s"
    }
    return durationShort(remaining)
}

// durationShort formats duration concisely (e.g., "2h30m", "45s")
func durationShort(d time.Duration) string {
    if d < time.Minute {
        return fmt.Sprintf("%ds", int(d.Seconds()))
    }
    if d < time.Hour {
        return fmt.Sprintf("%dm", int(d.Minutes()))
    }
    if d < 24*time.Hour {
        h := int(d.Hours())
        m := int(d.Minutes()) % 60
        if m == 0 {
            return fmt.Sprintf("%dh", h)
        }
        return fmt.Sprintf("%dh%dm", h, m)
    }
    days := int(d.Hours()) / 24
    h := int(d.Hours()) % 24
    if h == 0 {
        return fmt.Sprintf("%dd", days)
    }
    return fmt.Sprintf("%dd%dh", days, h)
}

// renderSyncProgressDashboard creates dashboard-style sync progress line
func renderSyncProgressDashboard(local, remote int64, isCatchingUp bool) string {
    if remote <= 0 {
        return ""
    }

    percent := float64(local) / float64(remote) * 100
    if percent < 0 {
        percent = 0
    }
    if percent > 100 {
        percent = 100
    }

    width := 28
    filled := int(percent / 100 * float64(width))
    if filled < 0 {
        filled = 0
    }
    if filled > width {
        filled = width
    }

    // Create colored progress bar
    greenBar := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(strings.Repeat("█", filled))
    greyBar := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("░", width-filled))
    bar := greenBar + greyBar

    // Status label
    icon := "📊 Syncing"
    if !isCatchingUp {
        icon = "📊 In Sync"
    }

    result := fmt.Sprintf("%s [%s] %.2f%% | %s/%s blocks",
        icon, bar, percent,
        formatWithCommas(local),
        formatWithCommas(remote))

    // Add ETA if syncing
    if isCatchingUp && remote > local {
        blocksBehind := remote - local
        // Assume average block time of ~6 seconds (adjust if needed)
        eta := blocksBehind * 6
        result += fmt.Sprintf(" | ETA: %s", durationShort(time.Duration(eta)*time.Second))
    } else if remote > 0 {
        // In sync
        result += " | ETA: 0s"
    }

    return result
}
