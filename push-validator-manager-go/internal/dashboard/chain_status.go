package dashboard

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChainStatus component shows chain sync status
type ChainStatus struct {
	BaseComponent
	data    DashboardData
	icons   Icons
	etaCalc *ETACalculator
	noEmoji bool
}

// NewChainStatus creates a new chain status component
func NewChainStatus(noEmoji bool) *ChainStatus {
	return &ChainStatus{
		BaseComponent: BaseComponent{},
		icons:         NewIcons(noEmoji),
		etaCalc:       NewETACalculator(),
		noEmoji:       noEmoji,
	}
}

// ID returns component identifier
func (c *ChainStatus) ID() string {
	return "chain_status"
}

// Title returns component title
func (c *ChainStatus) Title() string {
	return "Chain Status"
}

// MinWidth returns minimum width
func (c *ChainStatus) MinWidth() int {
	return 30
}

// MinHeight returns minimum height
func (c *ChainStatus) MinHeight() int {
	return 10
}

// Update receives dashboard data
func (c *ChainStatus) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	c.data = data

	// Update ETA calculator
	if data.Metrics.Chain.RemoteHeight > data.Metrics.Chain.LocalHeight {
		blocksBehind := data.Metrics.Chain.RemoteHeight - data.Metrics.Chain.LocalHeight
		c.etaCalc.AddSample(blocksBehind)
	}

	return c, nil
}

// View renders the component with caching
func (c *ChainStatus) View(w, h int) string {
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

	rendered := style.Height(h).Render(content)
	c.UpdateCache(rendered)
	return rendered
}

// renderContent builds plain text content
func (c *ChainStatus) renderContent(w int) string {
	var lines []string

	// Interior width after accounting for rounded border (2 chars) and padding (2 chars).
	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	// Current Height
	lines = append(lines, fmt.Sprintf("Height: %s", HumanInt(c.data.Metrics.Chain.LocalHeight)))

	// Remote Height
	if c.data.Metrics.Chain.RemoteHeight > 0 {
		lines = append(lines, fmt.Sprintf("Remote: %s", HumanInt(c.data.Metrics.Chain.RemoteHeight)))
	}

	// Sync Status
	syncIcon := c.icons.OK
	syncStatus := "In Sync"

	// Check if node is running and RPC is available
	if !c.data.NodeInfo.Running || !c.data.Metrics.Node.RPCListening {
		syncIcon = c.icons.Err
		syncStatus = "Unknown"
	} else if c.data.Metrics.Chain.CatchingUp {
		syncIcon = c.icons.Warn
		syncStatus = "Catching Up"
	}
	lines = append(lines, fmt.Sprintf("%s %s", syncIcon, syncStatus))

	// Progress bar when catching up
	if c.data.Metrics.Chain.CatchingUp && c.data.Metrics.Chain.RemoteHeight > 0 {
		fraction := float64(c.data.Metrics.Chain.LocalHeight) / float64(c.data.Metrics.Chain.RemoteHeight)
		if inner >= 3 {
			bar := ProgressBar(fraction, inner, c.noEmoji)
			pct := Percent(fraction)
			lines = append(lines, fmt.Sprintf("%s %s", bar, pct))
		} else {
			lines = append(lines, fmt.Sprintf("Progress: %s", Percent(fraction)))
		}
	}

	// Blocks Behind
	if c.data.Metrics.Chain.RemoteHeight > c.data.Metrics.Chain.LocalHeight {
		blocksBehind := c.data.Metrics.Chain.RemoteHeight - c.data.Metrics.Chain.LocalHeight
		lines = append(lines, fmt.Sprintf("Behind: %s blocks", HumanInt(blocksBehind)))
	}

	// ETA to sync
	if c.data.Metrics.Chain.CatchingUp {
		eta := c.etaCalc.Calculate()
		lines = append(lines, fmt.Sprintf("ETA: %s", eta))
	}

	return fmt.Sprintf("%s\n%s", c.Title(), joinLines(lines, "\n"))
}
