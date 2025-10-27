package ui

import (
    "fmt"
    "strings"
)

// ErrorMessage represents a structured, actionable error to present to users.
type ErrorMessage struct {
    Problem string   // one-line problem statement
    Causes  []string // possible causes
    Actions []string // actionable steps to resolve
    Hints   []string // optional hints (e.g., commands to try)
}

// Format renders the error using the color theme. It does not include ANSI
// codes when colors are disabled (NO_COLOR or dumb terminal).
func (e ErrorMessage) Format(c *ColorConfig) string {
    var b strings.Builder
    // Header
    b.WriteString(c.Error("✗ "))
    b.WriteString(c.Header("Error"))
    b.WriteString("\n")
    if e.Problem != "" {
        b.WriteString("  ")
        b.WriteString(c.Label("Problem"))
        b.WriteString(": ")
        b.WriteString(e.Problem)
        b.WriteString("\n")
    }
    if len(e.Causes) > 0 {
        b.WriteString("  ")
        b.WriteString(c.Label("Possible causes"))
        b.WriteString(":\n")
        for _, it := range e.Causes {
            b.WriteString("   • ")
            b.WriteString(it)
            b.WriteString("\n")
        }
    }
    if len(e.Actions) > 0 {
        b.WriteString("  ")
        b.WriteString(c.Label("Try"))
        b.WriteString(":\n")
        for _, it := range e.Actions {
            b.WriteString("   → ")
            b.WriteString(it)
            b.WriteString("\n")
        }
    }
    if len(e.Hints) > 0 {
        b.WriteString("  ")
        b.WriteString(c.Label("Hints"))
        b.WriteString(":\n")
        for _, it := range e.Hints {
            b.WriteString("   · ")
            b.WriteString(c.Description(it))
            b.WriteString("\n")
        }
    }
    return b.String()
}

// PrintError prints the structured error to stdout using the current theme.
func PrintError(e ErrorMessage) {
    c := NewColorConfig()
    fmt.Println(e.Format(c))
}

