package dashboard

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ValidatorsList component shows network validators
type ValidatorsList struct {
	BaseComponent
	data  DashboardData
	icons Icons
}

// NewValidatorsList creates a new validators list component
func NewValidatorsList(noEmoji bool) *ValidatorsList {
	return &ValidatorsList{
		BaseComponent: BaseComponent{},
		icons:         NewIcons(noEmoji),
	}
}

// ID returns component identifier
func (c *ValidatorsList) ID() string {
	return "validators_list"
}

// Title returns component title
func (c *ValidatorsList) Title() string {
	return "Network Validators"
}

// MinWidth returns minimum width
func (c *ValidatorsList) MinWidth() int {
	return 30
}

// MinHeight returns minimum height
func (c *ValidatorsList) MinHeight() int {
	return 16
}

// Update receives dashboard data
func (c *ValidatorsList) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	c.data = data
	return c, nil
}

// View renders the component with caching
func (c *ValidatorsList) View(w, h int) string {
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
func (c *ValidatorsList) renderContent(w int) string {
	var lines []string

	// Interior width after accounting for rounded border (2 chars) and padding (2 chars).
	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	// Check if validator data is available
	if c.data.NetworkValidators.Total == 0 {
		return fmt.Sprintf("%s\n\n%s Loading validators...", FormatTitle(c.Title(), inner), c.icons.Warn)
	}

	// Sort validators by voting power (highest first)
	validators := make([]struct {
		Moniker     string
		Status      string
		VotingPower int64
		Commission  string
		Address     string
	}, len(c.data.NetworkValidators.Validators))
	copy(validators, c.data.NetworkValidators.Validators)

	sort.Slice(validators, func(i, j int) bool {
		return validators[i].VotingPower > validators[j].VotingPower
	})

	// Table header
	lines = append(lines, "MONIKER           STATUS    STAKE (PC) COMMISSION ADDRESS")
	lines = append(lines, "──────────────────────────────────────────────────────────────")

	// Show top 10 validators
	maxDisplay := 10
	if len(validators) < maxDisplay {
		maxDisplay = len(validators)
	}

	for i := 0; i < maxDisplay; i++ {
		v := validators[i]

		// Truncate moniker to 17 chars
		moniker := truncateWithEllipsis(v.Moniker, 17)

		// Format status to 9 chars max
		status := v.Status
		if len(status) > 9 {
			status = status[:9]
		}

		// Format voting power with comma separators
		powerStr := fmt.Sprintf("%s.0", HumanInt(v.VotingPower))

		// Show full address (no truncation)
		address := v.Address

		// Build row with fixed-width columns
		line := fmt.Sprintf("%-17s %-9s %-10s %-10s %s",
			moniker, status, powerStr, v.Commission, address)
		lines = append(lines, line)
	}

	lines = append(lines, "")

	// Add total count only
	lines = append(lines, fmt.Sprintf("Total: %d validators", c.data.NetworkValidators.Total))

	return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
}

// getStatusIcon returns appropriate icon for validator status
func (c *ValidatorsList) getStatusIcon(status string) string {
	switch status {
	case "BONDED":
		return c.icons.OK
	case "UNBONDING":
		return c.icons.Warn
	case "UNBONDED":
		return c.icons.Err
	default:
		return c.icons.Warn
	}
}
