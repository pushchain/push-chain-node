package ui

import (
	"fmt"
	"os"
	"strings"
)

// Color codes for terminal output
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Italic = "\033[3m"
	Underline = "\033[4m"

	// Primary colors
	Black   = "\033[30m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"

	// Bright colors
	BrightBlack   = "\033[90m"
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"

	// Background colors
	BgBlack   = "\033[40m"
	BgRed     = "\033[41m"
	BgGreen   = "\033[42m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgWhite   = "\033[47m"
)

// Theme defines the color scheme for different UI elements
type Theme struct {
	// Status indicators
	Success     string
	Warning     string
	Error       string
	Info        string

	// UI elements
	Header      string
	SubHeader   string
	Label       string
	Value       string
	Command     string
	Flag        string
	Description string
	Separator   string

	// Interactive elements
	Prompt      string
	Input       string
	Selection   string

	// Progress indicators
	Progress    string
	Complete    string
	Pending     string

	// Special elements
	Logo        string
	Version     string
	Timestamp   string
}

// DefaultTheme returns the default color theme
func DefaultTheme() *Theme {
	return &Theme{
		// Status indicators - Clear semantic colors
		Success:     BrightGreen,
		Warning:     BrightYellow,
		Error:       BrightRed,
		Info:        BrightCyan,

		// UI elements - Professional and readable
		Header:      Bold + BrightCyan,
		SubHeader:   Bold + Cyan,
		Label:       Bold + White,
		Value:       BrightWhite,
		Command:     Bold + BrightGreen,
		Flag:        BrightYellow,
		Description: BrightBlack,
		Separator:   BrightBlack,

		// Interactive elements
		Prompt:      Bold + BrightMagenta,
		Input:       BrightWhite,
		Selection:   Bold + BrightCyan,

		// Progress indicators
		Progress:    BrightYellow,
		Complete:    BrightGreen,
		Pending:     BrightBlack,

		// Special elements
		Logo:        Bold + BrightMagenta,
		Version:     BrightBlack,
		Timestamp:   BrightBlack,
	}
}

// ColorConfig manages color output settings
type ColorConfig struct {
	Enabled bool
	Theme   *Theme
}

// NewColorConfig creates a new color configuration
func NewColorConfig() *ColorConfig {
	// Check if colors should be disabled
	noColor := os.Getenv("NO_COLOR") != ""
	term := os.Getenv("TERM")

	// Disable colors if NO_COLOR is set or TERM is dumb
	enabled := !noColor && term != "dumb" && term != ""

	return &ColorConfig{
		Enabled: enabled,
		Theme:   DefaultTheme(),
	}
}

// Apply applies a color to text if colors are enabled
func (c *ColorConfig) Apply(color, text string) string {
	if !c.Enabled {
		return text
	}
	return color + text + Reset
}

// Success formats success messages
func (c *ColorConfig) Success(text string) string {
	return c.Apply(c.Theme.Success, text)
}

// Warning formats warning messages
func (c *ColorConfig) Warning(text string) string {
	return c.Apply(c.Theme.Warning, text)
}

// Error formats error messages
func (c *ColorConfig) Error(text string) string {
	return c.Apply(c.Theme.Error, text)
}

// Info formats info messages
func (c *ColorConfig) Info(text string) string {
	return c.Apply(c.Theme.Info, text)
}

// Header formats header text
func (c *ColorConfig) Header(text string) string {
	return c.Apply(c.Theme.Header, text)
}

// SubHeader formats sub-header text
func (c *ColorConfig) SubHeader(text string) string {
	return c.Apply(c.Theme.SubHeader, text)
}

// Label formats label text
func (c *ColorConfig) Label(text string) string {
	return c.Apply(c.Theme.Label, text)
}

// Value formats value text
func (c *ColorConfig) Value(text string) string {
	return c.Apply(c.Theme.Value, text)
}

// Command formats command text
func (c *ColorConfig) Command(text string) string {
	return c.Apply(c.Theme.Command, text)
}

// Flag formats flag text
func (c *ColorConfig) Flag(text string) string {
	return c.Apply(c.Theme.Flag, text)
}

// Description formats description text
func (c *ColorConfig) Description(text string) string {
	return c.Apply(c.Theme.Description, text)
}

// FormatKeyValue formats a key-value pair with proper colors
func (c *ColorConfig) FormatKeyValue(key, value string) string {
	return fmt.Sprintf("%s: %s", c.Label(key), c.Value(value))
}

// FormatCommand formats a command with its description
func (c *ColorConfig) FormatCommand(cmd, desc string) string {
	return fmt.Sprintf("  %s  %s", c.Command(cmd), c.Description(desc))
}

// FormatFlag formats a flag with its description
func (c *ColorConfig) FormatFlag(flag, desc string) string {
	return fmt.Sprintf("  %s  %s", c.Flag(flag), c.Description(desc))
}

// Separator returns a colored separator line
func (c *ColorConfig) Separator(width int) string {
	sep := strings.Repeat("─", width)
	return c.Apply(c.Theme.Separator, sep)
}

// Box creates a colored box around text
func (c *ColorConfig) Box(text string, width int) string {
	if !c.Enabled {
		return text
	}

	topBorder := c.Apply(c.Theme.Separator, "┌" + strings.Repeat("─", width-2) + "┐")
	bottomBorder := c.Apply(c.Theme.Separator, "└" + strings.Repeat("─", width-2) + "┘")

	lines := strings.Split(text, "\n")
	var boxed []string
	boxed = append(boxed, topBorder)

	for _, line := range lines {
		padding := width - len(line) - 4
		if padding < 0 {
			padding = 0
		}
		boxedLine := c.Apply(c.Theme.Separator, "│ ") + line + strings.Repeat(" ", padding) + c.Apply(c.Theme.Separator, " │")
		boxed = append(boxed, boxedLine)
	}

	boxed = append(boxed, bottomBorder)
	return strings.Join(boxed, "\n")
}

// StatusIcon returns a colored status icon
func (c *ColorConfig) StatusIcon(status string) string {
	switch strings.ToLower(status) {
	case "success", "running", "active", "online":
		return c.Success("✓")
	case "warning", "syncing", "pending":
		return c.Warning("⚠")
	case "error", "failed", "stopped", "offline":
		return c.Error("✗")
	case "info":
		return c.Info("ℹ")
	default:
		return c.Apply(c.Theme.Pending, "○")
	}
}

// ProgressBar creates a colored progress bar
func (c *ColorConfig) ProgressBar(percent float64, width int) string {
	if width < 10 {
		width = 10
	}

	filled := int(float64(width) * percent / 100)
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	if percent >= 100 {
		return c.Apply(c.Theme.Complete, bar)
	} else if percent >= 50 {
		return c.Apply(c.Theme.Progress, bar)
	}
	return c.Apply(c.Theme.Pending, bar)
}

// Spinner returns a colored spinner character for the given frame
func (c *ColorConfig) Spinner(frame int) string {
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return c.Apply(c.Theme.Progress, spinners[frame%len(spinners)])
}