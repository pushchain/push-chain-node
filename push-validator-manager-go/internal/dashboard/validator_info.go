package dashboard

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ValidatorInfo component shows validator-specific information
type ValidatorInfo struct {
	BaseComponent
	data  DashboardData
	icons Icons
}

// NewValidatorInfo creates a new validator info component
func NewValidatorInfo(noEmoji bool) *ValidatorInfo {
	return &ValidatorInfo{
		BaseComponent: BaseComponent{},
		icons:         NewIcons(noEmoji),
	}
}

// ID returns component identifier
func (c *ValidatorInfo) ID() string {
	return "validator_info"
}

// Title returns component title
func (c *ValidatorInfo) Title() string {
	return "Validator (Consensus Power)"
}

// MinWidth returns minimum width
func (c *ValidatorInfo) MinWidth() int {
	return 30
}

// MinHeight returns minimum height
func (c *ValidatorInfo) MinHeight() int {
	return 10
}

// Update receives dashboard data
func (c *ValidatorInfo) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	c.data = data
	return c, nil
}

// View renders the component with caching
func (c *ValidatorInfo) View(w, h int) string {
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
func (c *ValidatorInfo) renderContent() string {
	// Check if validator data is available
	// Note: Validator info fetching is not yet implemented (Phase 1C)
	if c.data.ValidatorInfo.Address == "" {
		return fmt.Sprintf("%s\n\n%s Feature coming soon\n\nValidator metrics will be\navailable in Phase 1C", c.Title(), c.icons.Warn)
	}

	var lines []string

	// Address (truncated)
	addr := c.data.ValidatorInfo.Address
	if len(addr) > 20 {
		addr = addr[:17] + "..."
	}
	lines = append(lines, fmt.Sprintf("Addr: %s", addr))

	// Status
	statusIcon := c.icons.OK
	if c.data.ValidatorInfo.Jailed {
		statusIcon = c.icons.Err
	}
	lines = append(lines, fmt.Sprintf("%s Status: %s", statusIcon, c.data.ValidatorInfo.Status))

	// Voting Power
	vpText := HumanInt(c.data.ValidatorInfo.VotingPower)
	if c.data.ValidatorInfo.VotingPct > 0 {
		vpText += fmt.Sprintf(" (%s)", Percent(c.data.ValidatorInfo.VotingPct))
	}
	lines = append(lines, fmt.Sprintf("Power: %s", vpText))

	// Commission
	if c.data.ValidatorInfo.Commission != "" {
		lines = append(lines, fmt.Sprintf("Commission: %s", c.data.ValidatorInfo.Commission))
	}

	// Jailed status
	if c.data.ValidatorInfo.Jailed {
		lines = append(lines, fmt.Sprintf("%s JAILED", c.icons.Err))
	}

	// Delegators count
	if c.data.ValidatorInfo.Delegators > 0 {
		lines = append(lines, fmt.Sprintf("Delegators: %d", c.data.ValidatorInfo.Delegators))
	}

	return fmt.Sprintf("%s\n%s", c.Title(), joinLines(lines, "\n"))
}
