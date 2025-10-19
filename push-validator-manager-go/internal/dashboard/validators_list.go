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
	data        DashboardData
	icons       Icons
	currentPage int // Current page (0-based)
	pageSize    int // Items per page
}

// NewValidatorsList creates a new validators list component
func NewValidatorsList(noEmoji bool) *ValidatorsList {
	return &ValidatorsList{
		BaseComponent: BaseComponent{},
		icons:         NewIcons(noEmoji),
		currentPage:   0,
		pageSize:      5,
	}
}

// ID returns component identifier
func (c *ValidatorsList) ID() string {
	return "validators_list"
}

// Title returns component title
func (c *ValidatorsList) Title() string {
	totalValidators := len(c.data.NetworkValidators.Validators)
	if totalValidators == 0 {
		return "Network Validators"
	}

	totalPages := (totalValidators + c.pageSize - 1) / c.pageSize
	if totalPages > 1 {
		return fmt.Sprintf("Network Validators (Page %d/%d)", c.currentPage+1, totalPages)
	}
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

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return c.handleKey(msg)
	}

	return c, nil
}

// handleKey processes keyboard input for pagination
func (c *ValidatorsList) handleKey(msg tea.KeyMsg) (Component, tea.Cmd) {
	totalValidators := len(c.data.NetworkValidators.Validators)
	if totalValidators == 0 {
		return c, nil
	}

	totalPages := (totalValidators + c.pageSize - 1) / c.pageSize

	switch msg.String() {
	case "left", "p":
		// Previous page
		if c.currentPage > 0 {
			c.currentPage--
		}
	case "right", "n":
		// Next page
		if c.currentPage < totalPages-1 {
			c.currentPage++
		}
	}

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

	rendered := style.Width(contentWidth).Render(content)
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

	// Sort validators by status first, then by voting power (matching CLI logic)
	validators := make([]struct {
		Moniker     string
		Status      string
		VotingPower int64
		Commission  string
		Address     string
	}, len(c.data.NetworkValidators.Validators))
	copy(validators, c.data.NetworkValidators.Validators)

	// Helper to get status sort order
	statusOrder := func(status string) int {
		switch status {
		case "BONDED":
			return 1
		case "UNBONDING":
			return 2
		case "UNBONDED":
			return 3
		default:
			return 4
		}
	}

	sort.Slice(validators, func(i, j int) bool {
		// Sort by status first (BONDED < UNBONDING < UNBONDED)
		iOrder := statusOrder(validators[i].Status)
		jOrder := statusOrder(validators[j].Status)
		if iOrder != jOrder {
			return iOrder < jOrder
		}
		// Within same status, sort by voting power (highest first)
		return validators[i].VotingPower > validators[j].VotingPower
	})

	// Table header
	lines = append(lines, "MONIKER           STATUS    STAKE (PC) COMMISSION ADDRESS")
	lines = append(lines, "──────────────────────────────────────────────────────────────")

	// Calculate pagination bounds
	totalValidators := len(validators)
	startIdx := c.currentPage * c.pageSize
	endIdx := startIdx + c.pageSize
	if endIdx > totalValidators {
		endIdx = totalValidators
	}

	// Bounds check
	if startIdx >= totalValidators {
		startIdx = 0
		c.currentPage = 0
		endIdx = c.pageSize
		if endIdx > totalValidators {
			endIdx = totalValidators
		}
	}

	// Display validators for current page
	for i := startIdx; i < endIdx; i++ {
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

	// Add pagination footer
	totalPages := (totalValidators + c.pageSize - 1) / c.pageSize
	if totalPages > 1 {
		footer := fmt.Sprintf("← / →: change page | Total: %d validators", c.data.NetworkValidators.Total)
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(footer))
	} else {
		lines = append(lines, fmt.Sprintf("Total: %d validators", c.data.NetworkValidators.Total))
	}

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
