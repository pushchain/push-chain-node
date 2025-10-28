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

	rendered := style.Width(contentWidth).Render(content)
	c.UpdateCache(rendered)
	return rendered
}

// renderContent builds plain text content
func (c *NetworkStatus) renderContent(w int) string {
	var lines []string

	// Interior width after accounting for rounded border (2 chars) and padding (2 chars).
	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	// Peers list
	if len(c.data.PeerList) > 0 {
		lines = append(lines, fmt.Sprintf("Connected to %d peers:", len(c.data.PeerList)))
		maxDisplay := 5
		for i, peer := range c.data.PeerList {
			if i >= maxDisplay {
				lines = append(lines, fmt.Sprintf("  ... and %d more", len(c.data.PeerList)-maxDisplay))
				break
			}
			// Show full ID
			lines = append(lines, fmt.Sprintf("  %s", peer.ID))
		}
	} else {
		lines = append(lines, fmt.Sprintf("%s 0 peers", c.icons.Warn))
	}

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
		// Show full node ID
		lines = append(lines, fmt.Sprintf("Node ID: %s", c.data.Metrics.Node.NodeID))
	}

	// Moniker
	if c.data.Metrics.Node.Moniker != "" {
		lines = append(lines, fmt.Sprintf("Name: %s", c.data.Metrics.Node.Moniker))
	}

	return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
}
