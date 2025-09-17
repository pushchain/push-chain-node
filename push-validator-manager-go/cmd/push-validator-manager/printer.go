package main

import (
    "encoding/json"
    "fmt"
    "os"
)

// Printer is a tiny helper that centralizes output formatting
// across subcommands. It respects the root-level --output flag
// (text|json) and provides JSON and printf-style helpers.
type Printer struct{ format string }

// getPrinter returns a Printer bound to the current --output value.
func getPrinter() Printer { return Printer{format: flagOutput} }

// Textf prints formatted text to stdout.
func (p Printer) Textf(format string, a ...any) { fmt.Printf(format, a...) }

// JSON pretty-prints a JSON value to stdout. Errors are ignored to
// keep command flows simple; commands already control exit codes.
func (p Printer) JSON(v any) {
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    _ = enc.Encode(v)
}
