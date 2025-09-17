package ui

import (
    "encoding/json"
    "fmt"
    "os"
)

// Printer centralizes output formatting and supports text|json for now.
// It can be extended later to integrate color and table helpers.
type Printer struct{ format string }

func NewPrinter(format string) Printer { return Printer{format: format} }

// Textf prints formatted text to stdout.
func (p Printer) Textf(format string, a ...any) { fmt.Printf(format, a...) }

// JSON pretty-prints a JSON value to stdout. Errors are ignored to keep flows simple.
func (p Printer) JSON(v any) {
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    _ = enc.Encode(v)
}

