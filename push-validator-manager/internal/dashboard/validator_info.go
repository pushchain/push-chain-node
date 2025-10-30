package dashboard

import (
	"fmt"
	"time"

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

	// Check if validator exists by moniker but consensus pubkey doesn't match
	// (may have been created with a different key/node) - check this FIRST before other IsValidator checks
	if !c.data.MyValidator.IsValidator && c.data.MyValidator.Moniker != "" && c.data.MyValidator.Status != "" {
		var lines []string

		// Warn that consensus pubkey doesn't match
		lines = append(lines, fmt.Sprintf("%s Validator found by moniker", c.icons.Warn))
		lines = append(lines, "but running with different key/node")
		lines = append(lines, "")

		// Show the actual validator status
		statusIcon := c.icons.OK
		if c.data.MyValidator.Jailed {
			statusIcon = c.icons.Err
		} else if c.data.MyValidator.Status == "UNBONDING" || c.data.MyValidator.Status == "UNBONDED" {
			statusIcon = c.icons.Warn
		}
		lines = append(lines, fmt.Sprintf("%s Status: %s", statusIcon, c.data.MyValidator.Status))

		// Show voting power
		vpText := HumanInt(c.data.MyValidator.VotingPower)
		if c.data.MyValidator.VotingPct > 0 {
			vpText += fmt.Sprintf(" (%s)", Percent(c.data.MyValidator.VotingPct))
		}
		lines = append(lines, fmt.Sprintf("Power: %s", vpText))

		// Show commission if available
		if c.data.MyValidator.Commission != "" {
			lines = append(lines, fmt.Sprintf("Commission: %s", c.data.MyValidator.Commission))
		}

		// Show jailed status with reason if applicable
		if c.data.MyValidator.Jailed {
			jailReason := c.data.MyValidator.SlashingInfo.JailReason
			if jailReason == "" {
				jailReason = "Unknown"
			}
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("%s Jailed: %s", c.icons.Err, jailReason))
		}

		lines = append(lines, "")
		lines = append(lines, "To control this validator, run:")
		lines = append(lines, "push-validator register")

		return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
	}

	// Check if this node is a validator
	if !c.data.MyValidator.IsValidator {
		// Check for moniker conflict
		if c.data.MyValidator.ValidatorExistsWithSameMoniker {
			return fmt.Sprintf("%s\n\n%s Not registered\n\n%s Moniker conflict detected!\nA different validator is using\nmoniker '%s'\n\nUse a different moniker to register:\npush-validator register",
				FormatTitle(c.Title(), inner),
				c.icons.Warn,
				c.icons.Err,
				truncateWithEllipsis(c.data.MyValidator.ConflictingMoniker, 20))
		}

		return fmt.Sprintf("%s\n\n%s Not registered as validator\n\nTo register, run:\npush-validator register", FormatTitle(c.Title(), inner), c.icons.Warn)
	}

	// Build left column
	var leftLines []string

	// Moniker
	if c.data.MyValidator.Moniker != "" {
		leftLines = append(leftLines, fmt.Sprintf("Moniker: %s", truncateWithEllipsis(c.data.MyValidator.Moniker, 22)))
	}

	// Status
	statusIcon := c.icons.OK
	if c.data.MyValidator.Jailed {
		statusIcon = c.icons.Err
	} else if c.data.MyValidator.Status == "UNBONDING" || c.data.MyValidator.Status == "UNBONDED" {
		statusIcon = c.icons.Warn
	}
	leftLines = append(leftLines, fmt.Sprintf("%s Status: %s", statusIcon, c.data.MyValidator.Status))

	// Voting Power
	vpText := HumanInt(c.data.MyValidator.VotingPower)
	if c.data.MyValidator.VotingPct > 0 {
		vpText += fmt.Sprintf(" (%s)", Percent(c.data.MyValidator.VotingPct))
	}
	leftLines = append(leftLines, fmt.Sprintf("Power: %s", vpText))

	// Commission
	if c.data.MyValidator.Commission != "" {
		leftLines = append(leftLines, fmt.Sprintf("Commission: %s", c.data.MyValidator.Commission))
	}

	// Commission Rewards
	if c.data.MyValidator.CommissionRewards != "" && c.data.MyValidator.CommissionRewards != "—" {
		leftLines = append(leftLines, fmt.Sprintf("Commission Rewards: %s PC", FormatFloat(c.data.MyValidator.CommissionRewards)))
	}

	// Outstanding Rewards
	if c.data.MyValidator.OutstandingRewards != "" && c.data.MyValidator.OutstandingRewards != "—" {
		leftLines = append(leftLines, fmt.Sprintf("Outstanding Rewards: %s PC", FormatFloat(c.data.MyValidator.OutstandingRewards)))
	}

	// Check if validator has any rewards to withdraw
	hasCommRewards := c.data.MyValidator.CommissionRewards != "" &&
		c.data.MyValidator.CommissionRewards != "—" &&
		c.data.MyValidator.CommissionRewards != "0"
	hasOutRewards := c.data.MyValidator.OutstandingRewards != "" &&
		c.data.MyValidator.OutstandingRewards != "—" &&
		c.data.MyValidator.OutstandingRewards != "0"

	if hasCommRewards || hasOutRewards {
		leftLines = append(leftLines, "")
		withdrawStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
		leftLines = append(leftLines, withdrawStyle.Render("Rewards available!"))
		leftLines = append(leftLines, withdrawStyle.Render("Run: push-validator restake"))
		leftLines = append(leftLines, withdrawStyle.Render("Run: push-validator withdraw-rewards"))
	}

	// If jailed, create two-column layout with jail details on the right
	if c.data.MyValidator.Jailed {
		var rightLines []string

		// Right column header
		rightLines = append(rightLines, "STATUS DETAILS")
		rightLines = append(rightLines, "")

		// Add status with jail indicator
		statusText := c.data.MyValidator.Status
		if c.data.MyValidator.Jailed {
			statusText = fmt.Sprintf("%s (JAILED)", c.data.MyValidator.Status)
		}
		rightLines = append(rightLines, statusText)
		rightLines = append(rightLines, "")

		// Jail Reason
		jailReason := c.data.MyValidator.SlashingInfo.JailReason
		if jailReason == "" {
			jailReason = "Unknown"
		}
		rightLines = append(rightLines, fmt.Sprintf("Reason: %s", jailReason))

		// Missed Blocks
		if c.data.MyValidator.SlashingInfo.MissedBlocks > 0 {
			rightLines = append(rightLines, fmt.Sprintf("Missed: %s blks", HumanInt(c.data.MyValidator.SlashingInfo.MissedBlocks)))
		}

		// Tombstoned Status
		if c.data.MyValidator.SlashingInfo.Tombstoned {
			rightLines = append(rightLines, fmt.Sprintf("%s Tombstoned: Yes", c.icons.Err))
		}

		// Jailed Until Time
		if c.data.MyValidator.SlashingInfo.JailedUntil != "" {
			formatted := FormatTimestamp(c.data.MyValidator.SlashingInfo.JailedUntil)
			if formatted != "" {
				rightLines = append(rightLines, fmt.Sprintf("Until: %s", formatted))
			}

			// Time remaining
			timeLeft := TimeUntil(c.data.MyValidator.SlashingInfo.JailedUntil)
			if timeLeft != "" && timeLeft != "0s" {
				rightLines = append(rightLines, fmt.Sprintf("Remaining: %s", timeLeft))
			}

			// Check if jail period has expired
			if parseTimeExpired(c.data.MyValidator.SlashingInfo.JailedUntil) {
				rightLines = append(rightLines, "")
				unjailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
				rightLines = append(rightLines, unjailStyle.Render(fmt.Sprintf("%s Ready to unjail!", c.icons.OK)))
				rightLines = append(rightLines, unjailStyle.Render("Run: push-validator unjail"))
			}
		}

		// Create two-column layout
		leftContent := joinLines(leftLines, "\n")
		rightContent := joinLines(rightLines, "\n")

		// Calculate column widths (simple split)
		midWidth := inner / 2
		leftWidth := midWidth
		rightWidth := inner - midWidth - 2 // Account for spacing

		leftStyle := lipgloss.NewStyle().Width(leftWidth)
		rightStyle := lipgloss.NewStyle().Width(rightWidth)

		leftRendered := leftStyle.Render(leftContent)
		rightRendered := rightStyle.Render(rightContent)

		twoColumnContent := lipgloss.JoinHorizontal(lipgloss.Top, leftRendered, "  ", rightRendered)

		return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), twoColumnContent)
	}

	// Single column layout for non-jailed validators
	lines := leftLines
	return fmt.Sprintf("%s\n%s", FormatTitle(c.Title(), inner), joinLines(lines, "\n"))
}

// parseTimeExpired checks if an RFC3339 timestamp is in the past
func parseTimeExpired(timeStr string) bool {
	if timeStr == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		return false
	}
	return time.Now().After(t)
}
