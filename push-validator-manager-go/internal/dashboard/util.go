package dashboard

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// HumanInt formats integers with thousands separators (handles negatives)
func HumanInt(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}

	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return sign + s
	}

	var result strings.Builder
	for i, c := range reverse(s) {
		if i > 0 && i%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return sign + reverse(result.String())
}

// Percent formats percentage - takes fraction in [0,1], returns formatted %
// IMPORTANT: Input convention is [0,1], not [0,100]
// Example: Percent(0.123) → "12.3%"
func Percent(fraction float64) string {
	if fraction < 0 {
		return "0.0%"
	}
	if fraction > 1 {
		return "100.0%"
	}
	return fmt.Sprintf("%.1f%%", fraction*100)
}

// truncateWithEllipsis caps string length to prevent overflow in fixed-width cells
func truncateWithEllipsis(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if maxLen == 1 {
		return "…"
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// ProgressBar creates ASCII/Unicode progress bar
func ProgressBar(fraction float64, width int, noEmoji bool) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	if width < 3 {
		// Too narrow for meaningful bar
		return fmt.Sprintf("%.0f%%", fraction*100)
	}

	// Calculate bar width - ASCII mode needs room for brackets
	barWidth := width
	if noEmoji {
		barWidth = width - 2 // Account for [ ] in ASCII mode only
	}

	filled := int(float64(barWidth) * fraction)
	if filled > barWidth {
		filled = barWidth
	}

	if noEmoji {
		// ASCII-only mode with brackets
		return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled) + "]"
	}

	// Unicode mode uses full width (no brackets)
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}

// DurationShort formats duration concisely
func DurationShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	if h == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%dh", days, h)
}

// ETACalculator maintains moving average for stable ETA
type ETACalculator struct {
	samples []struct {
		blocksBehind int64
		timestamp    time.Time
	}
	maxSamples int
}

// NewETACalculator creates a new ETA calculator
func NewETACalculator() *ETACalculator {
	return &ETACalculator{maxSamples: 10}
}

// AddSample adds a new sample point
func (e *ETACalculator) AddSample(blocksBehind int64) {
	e.samples = append(e.samples, struct {
		blocksBehind int64
		timestamp    time.Time
	}{blocksBehind, time.Now()})

	if len(e.samples) > e.maxSamples {
		e.samples = e.samples[1:]
	}
}

// Calculate returns ETA as formatted string
func (e *ETACalculator) Calculate() string {
	if len(e.samples) < 2 {
		return "calculating..."
	}

	// Compute rate from first to last sample
	first := e.samples[0]
	last := e.samples[len(e.samples)-1]

	blocksDelta := first.blocksBehind - last.blocksBehind
	timeDelta := last.timestamp.Sub(first.timestamp).Seconds()

	if timeDelta < 1 || blocksDelta <= 0 {
		return "calculating..."
	}

	rate := float64(blocksDelta) / timeDelta
	if rate < 0.01 { // Less than 1 block per 100 seconds
		return "calculating..."
	}

	seconds := float64(last.blocksBehind) / rate
	if seconds < 0 {
		return "—"
	}

	return DurationShort(time.Duration(seconds * float64(time.Second)))
}

// Icons struct for consistent emoji/ASCII fallback
type Icons struct {
	OK      string
	Warn    string
	Err     string
	Peer    string
	Block   string
	Unknown string // Neutral icon for unknown/indeterminate states
}

// NewIcons creates icon set based on emoji preference
func NewIcons(noEmoji bool) Icons {
	if noEmoji {
		return Icons{
			OK:      "[OK]",
			Warn:    "[!]",
			Err:     "[X]",
			Peer:    "#",
			Block:   "#",
			Unknown: "[?]",
		}
	}
	return Icons{
		OK:      "✓",
		Warn:    "⚠",
		Err:     "✗",
		Peer:    "🔗",
		Block:   "📦",
		Unknown: "◯",
	}
}

// reverse reverses a string
func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// joinLines joins string slice efficiently using strings.Builder
func joinLines(lines []string, sep string) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(line)
	}
	return b.String()
}

// innerWidthForBox calculates usable content width after accounting for border and padding
// total: allocated width from layout
// hasBorder: whether component has a border (adds 2 chars for left+right)
// padLeftRight: horizontal padding value
func innerWidthForBox(total int, hasBorder bool, padLeftRight int) int {
	border := 0
	if hasBorder {
		border = 2 // left + right border chars
	}
	w := total - border - 2*padLeftRight
	if w < 1 {
		w = 1
	}
	return w
}
