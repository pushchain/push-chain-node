package dashboard

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NodeStatus component shows node process status
type NodeStatus struct {
	BaseComponent
	data  DashboardData
	icons Icons
}

// NewNodeStatus creates a new node status component
func NewNodeStatus(noEmoji bool) *NodeStatus {
	return &NodeStatus{
		BaseComponent: BaseComponent{},
		icons:         NewIcons(noEmoji),
	}
}

// ID returns component identifier
func (c *NodeStatus) ID() string {
	return "node_status"
}

// Title returns component title
func (c *NodeStatus) Title() string {
	return "Node Status"
}

// MinWidth returns minimum width
func (c *NodeStatus) MinWidth() int {
	return 25
}

// MinHeight returns minimum height
func (c *NodeStatus) MinHeight() int {
	return 8
}

// Update receives dashboard data
func (c *NodeStatus) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	c.data = data
	return c, nil
}

// View renders the component with caching
func (c *NodeStatus) View(w, h int) string {
	// Render with styling
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1)

	content := c.renderContent(w)

	// Check cache
	if c.CheckCacheWithSize(content, w, h) {
		return c.GetCached()
	}

	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	// Account for border width (2 chars: left + right) to prevent overflow
	borderWidth := 2
	contentWidth := w - borderWidth
	if contentWidth < 0 {
		contentWidth = 0
	}

	rendered := style.Width(contentWidth).MaxHeight(h).Render(content)
	c.UpdateCache(rendered)
	return rendered
}

// renderContent builds plain text content
func (c *NodeStatus) renderContent(w int) string {
	var lines []string

	// Interior width after accounting for rounded border (2 chars) and padding (2 chars).
	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	// Status
	icon := c.icons.Err
	status := "Stopped"
	if c.data.NodeInfo.Running {
		icon = c.icons.OK
		status = "Running"
		if c.data.NodeInfo.PID != 0 {
			status = fmt.Sprintf("Running (pid %d)", c.data.NodeInfo.PID)
		}
	}
	lines = append(lines, fmt.Sprintf("%s %s", icon, status))

	// RPC Status
	rpcIcon := c.icons.Err
	rpcStatus := "Not listening"
	if c.data.Metrics.Node.RPCListening {
		rpcIcon = c.icons.OK
		rpcStatus = "Listening"
	}
	lines = append(lines, fmt.Sprintf("%s RPC: %s", rpcIcon, rpcStatus))

	// Uptime
	if c.data.NodeInfo.Uptime > 0 {
		lines = append(lines, fmt.Sprintf("Uptime: %s", DurationShort(c.data.NodeInfo.Uptime)))
	}

	// System metrics
	if c.data.Metrics.System.CPUPercent >= 0 {
		lines = append(lines, fmt.Sprintf("CPU: %.1f%%", c.data.Metrics.System.CPUPercent))
	}
	if c.data.Metrics.System.MemTotal > 0 {
		memPct := float64(c.data.Metrics.System.MemUsed) / float64(c.data.Metrics.System.MemTotal)
		lines = append(lines, fmt.Sprintf("Memory: %s", Percent(memPct)))
	}
	if c.data.Metrics.System.DiskTotal > 0 {
		diskPct := float64(c.data.Metrics.System.DiskUsed) / float64(c.data.Metrics.System.DiskTotal)
		lines = append(lines, fmt.Sprintf("Disk: %s", Percent(diskPct)))
	}

	// Binary Version
	if c.data.NodeInfo.BinaryVer != "" {
		lines = append(lines, fmt.Sprintf("Version: %s", c.data.NodeInfo.BinaryVer))
	}

	return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
}
