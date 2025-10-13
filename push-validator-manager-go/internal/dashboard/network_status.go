package dashboard

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NetworkStatus component shows network connection status
type NetworkStatus struct {
	BaseComponent
	data  DashboardData
	icons Icons
}

// NewNetworkStatus creates a new network status component
func NewNetworkStatus(noEmoji bool) *NetworkStatus {
	return &NetworkStatus{
		BaseComponent: BaseComponent{},
		icons:         NewIcons(noEmoji),
	}
}

// ID returns component identifier
func (c *NetworkStatus) ID() string {
	return "network_status"
}

// Title returns component title
func (c *NetworkStatus) Title() string {
	return "Network Status"
}

// MinWidth returns minimum width
func (c *NetworkStatus) MinWidth() int {
	return 25
}

// MinHeight returns minimum height
func (c *NetworkStatus) MinHeight() int {
	return 8
}

// Update receives dashboard data
func (c *NetworkStatus) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	c.data = data
	return c, nil
}

// View renders the component with caching
func (c *NetworkStatus) View(w, h int) string {
	// Render with styling
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1)

	content := c.renderContent()

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

	rendered := style.Height(h).Render(content)
	c.UpdateCache(rendered)
	return rendered
}

// renderContent builds plain text content
func (c *NetworkStatus) renderContent() string {
	var lines []string

	// Peers
	peersIcon := c.icons.Warn
	peersText := "0 peers"
	if c.data.Metrics.Network.Peers > 0 {
		peersIcon = c.icons.OK
		if c.data.Metrics.Network.Peers == 1 {
			peersText = "1 peer"
		} else {
			peersText = fmt.Sprintf("%d peers", c.data.Metrics.Network.Peers)
		}
	}
	lines = append(lines, fmt.Sprintf("%s %s", peersIcon, peersText))

	// Latency
	if c.data.Metrics.Network.LatencyMS > 0 {
		lines = append(lines, fmt.Sprintf("Latency: %dms", c.data.Metrics.Network.LatencyMS))
	}

	// Chain ID
	if c.data.Metrics.Node.ChainID != "" {
		lines = append(lines, fmt.Sprintf("Chain: %s", truncateWithEllipsis(c.data.Metrics.Node.ChainID, 24)))
	}

	// Node ID
	if c.data.Metrics.Node.NodeID != "" {
		// Truncate long node IDs
		nodeID := truncateWithEllipsis(c.data.Metrics.Node.NodeID, 16)
		lines = append(lines, fmt.Sprintf("Node: %s", nodeID))
	}

	// Moniker
	if c.data.Metrics.Node.Moniker != "" {
		lines = append(lines, fmt.Sprintf("Name: %s", truncateWithEllipsis(c.data.Metrics.Node.Moniker, 24)))
	}

	return fmt.Sprintf("%s\n%s", c.Title(), joinLines(lines, "\n"))
}
