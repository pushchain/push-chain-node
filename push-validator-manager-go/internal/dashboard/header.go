package dashboard

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// timeNow is a variable for time.Now to enable deterministic testing
var timeNow = time.Now

// Header component shows dashboard title, timestamp, and status
type Header struct {
	BaseComponent
	data DashboardData // Dashboard data with error info
}

// NewHeader creates a new header component
func NewHeader() *Header {
	return &Header{
		BaseComponent: BaseComponent{},
	}
}

// ID returns component identifier
func (c *Header) ID() string {
	return "header"
}

// Title returns component title
func (c *Header) Title() string {
	return "PUSH VALIDATOR DASHBOARD"
}

// MinWidth returns minimum width
func (c *Header) MinWidth() int {
	return 40
}

// MinHeight returns minimum height
func (c *Header) MinHeight() int {
	return 3
}

// Update receives dashboard data and updates internal state
func (c *Header) Update(msg tea.Msg, data DashboardData) (Component, tea.Cmd) {
	// Store entire data for access in View
	c.data = data
	return c, nil
}

// View renders the header matching canonical signature View(width, height int)
func (c *Header) View(w, h int) string {
	// Guard against invalid dimensions
	if w <= 0 || h <= 0 {
		return ""
	}

	// Build plain text content
	timestamp := timeNow().Format("15:04:05 MST")

	staleSuffix := ""
	if !c.data.LastUpdate.IsZero() && time.Since(c.data.LastUpdate) > 10*time.Second {
		staleSuffix = " (STALE)"
	}

	line1 := fmt.Sprintf("%s%s", c.Title(), staleSuffix)
	line2 := fmt.Sprintf("Last update: %s", timestamp)

	var lines []string
	lines = append(lines, line1)
	lines = append(lines, line2)

	if c.data.Err != nil {
		errLine := fmt.Sprintf("⚠ %s", c.data.Err.Error())
		lines = append(lines, errLine)
	}

	content := strings.Join(lines, "\n")

	// Match the exact styling pattern of data components for full compatibility
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1)

	return style.Height(h).Render(content)
}
