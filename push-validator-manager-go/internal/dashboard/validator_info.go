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
	return "My Validator Status"
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
func (c *ValidatorInfo) renderContent(w int) string {
	// Interior width after accounting for rounded border (2 chars) and padding (2 chars).
	inner := w - 4
	if inner < 0 {
		inner = 0
	}

	// Check if this node is a validator
	if !c.data.MyValidator.IsValidator {
		// Check for moniker conflict
		if c.data.MyValidator.ValidatorExistsWithSameMoniker {
			return fmt.Sprintf("%s\n\n%s Not registered\n\n%s Moniker conflict detected!\nA different validator is using\nmoniker '%s'\n\nUse a different moniker to register:\npush-validator-manager register",
				FormatTitle(c.Title(), inner),
				c.icons.Warn,
				c.icons.Err,
				truncateWithEllipsis(c.data.MyValidator.ConflictingMoniker, 20))
		}
		return fmt.Sprintf("%s\n\n%s Not registered as validator\n\nTo register, run:\npush-validator-manager register", FormatTitle(c.Title(), inner), c.icons.Warn)
	}

	var lines []string

	// Moniker
	if c.data.MyValidator.Moniker != "" {
		lines = append(lines, fmt.Sprintf("Moniker: %s", truncateWithEllipsis(c.data.MyValidator.Moniker, 22)))
	}

	// Status
	statusIcon := c.icons.OK
	if c.data.MyValidator.Jailed {
		statusIcon = c.icons.Err
	} else if c.data.MyValidator.Status == "UNBONDING" || c.data.MyValidator.Status == "UNBONDED" {
		statusIcon = c.icons.Warn
	}
	lines = append(lines, fmt.Sprintf("%s Status: %s", statusIcon, c.data.MyValidator.Status))

	// Voting Power
	vpText := HumanInt(c.data.MyValidator.VotingPower)
	if c.data.MyValidator.VotingPct > 0 {
		vpText += fmt.Sprintf(" (%s)", Percent(c.data.MyValidator.VotingPct))
	}
	lines = append(lines, fmt.Sprintf("Power: %s", vpText))

	// Commission
	if c.data.MyValidator.Commission != "" {
		lines = append(lines, fmt.Sprintf("Commission: %s", c.data.MyValidator.Commission))
	}

	// Jailed status
	if c.data.MyValidator.Jailed {
		lines = append(lines, fmt.Sprintf("%s JAILED", c.icons.Err))
	}

	return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
}
