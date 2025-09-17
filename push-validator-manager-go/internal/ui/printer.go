package ui

import (
    "encoding/json"
    "fmt"
    "os"
)

// Printer centralizes output formatting for commands.
// - Respects --output (text|json)
// - Uses ColorConfig for styling when printing text
// - Provides helpers for common message types
type Printer struct{
    format string
    Colors *ColorConfig
}

func NewPrinter(format string) Printer {
    return Printer{format: format, Colors: NewColorConfig()}
}

// Textf prints formatted text to stdout (always text path).
func (p Printer) Textf(format string, a ...any) { fmt.Printf(format, a...) }

// JSON pretty-prints a JSON value to stdout.
func (p Printer) JSON(v any) {
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    _ = enc.Encode(v)
}

// Success prints a success line with themed prefix.
func (p Printer) Success(msg string) {
    c := p.Colors
    fmt.Println(c.Success("✓"), msg)
}

// Info prints an informational line.
func (p Printer) Info(msg string) {
    c := p.Colors
    fmt.Println(c.Info("ℹ"), msg)
}

// Warn prints a warning line.
func (p Printer) Warn(msg string) {
    c := p.Colors
    fmt.Println(c.Warning("!"), msg)
}

// Error prints an error line.
func (p Printer) Error(msg string) {
    c := p.Colors
    fmt.Println(c.Error("✗"), msg)
}

// Header prints a section header.
func (p Printer) Header(title string) {
    fmt.Println(p.Colors.Header(" " + title + " "))
}

// Separator prints a themed separator line of n characters.
func (p Printer) Separator(n int) { fmt.Println(p.Colors.Separator(n)) }

