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
func (c *ChainStatus) renderContent(w int) string {
	var lines []string

	// Interior width after accounting for rounded border (2 chars) and padding (2 chars).
	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	localHeight := c.data.Metrics.Chain.LocalHeight
	remoteHeight := c.data.Metrics.Chain.RemoteHeight

	// Check if node is running and RPC is available
	if !c.data.NodeInfo.Running || !c.data.Metrics.Node.RPCListening {
		lines = append(lines, fmt.Sprintf("%s Unknown", c.icons.Err))
		if remoteHeight > 0 {
			lines = append(lines, fmt.Sprintf("%s/%s", formatWithCommas(localHeight), formatWithCommas(remoteHeight)))
		} else {
			lines = append(lines, fmt.Sprintf("Height: %s", formatWithCommas(localHeight)))
		}
	} else {
		// Always show sync-monitor-style progress bar
		isCatchingUp := c.data.Metrics.Chain.CatchingUp
		syncLine := renderSyncProgress(localHeight, remoteHeight, c.noEmoji, isCatchingUp)

		// Add ETA: calculated when syncing, "0s" when in sync
		if isCatchingUp && remoteHeight > localHeight {
			eta := c.etaCalc.Calculate()
			if eta != "" && eta != "calculating..." {
				syncLine += " | ETA: " + eta
			}
		} else if remoteHeight > 0 {
			syncLine += " | ETA: 0s"
		}

		lines = append(lines, syncLine)
	}

	// Use inner width for title centering
	return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
}

// renderSyncProgress creates sync-monitor-style progress line
func renderSyncProgress(local, remote int64, noEmoji bool, isCatchingUp bool) string {
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

	bar := fmt.Sprintf("%s%s",
		lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(fmt.Sprintf("%s", repeatStr("█", filled))),
		lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("%s", repeatStr("░", width-filled))))

	// Use different label based on sync state
	icon := "📊 Syncing"
	if !isCatchingUp {
		icon = "📊 In Sync"
	}
	if noEmoji {
		if isCatchingUp {
			icon = "Syncing"
		} else {
			icon = "In Sync"
		}
	}

	return fmt.Sprintf("%s [%s] %.2f%% | %s/%s blocks",
		icon, bar, percent,
		formatWithCommas(local),
		formatWithCommas(remote))
}

// formatWithCommas adds comma separators to large numbers
func formatWithCommas(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	// Convert to string and add commas
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

// repeatStr repeats a string n times
func repeatStr(s string, n int) string {
	var result string
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
