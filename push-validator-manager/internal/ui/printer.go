package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	// Don't add extra space if message already starts with whitespace
	space := " "
	if len(msg) > 0 && (msg[0] == ' ' || msg[0] == '\t') {
		space = ""
	}
	if c.EmojiEnabled {
		fmt.Printf("%s%s%s\n", c.Success("✓"), space, msg)
	} else {
		fmt.Printf("%s%s%s\n", c.Success("[OK]"), space, msg)
	}
}

// Info prints an informational line.
func (p Printer) Info(msg string) {
    c := p.Colors
    if c.EmojiEnabled {
        fmt.Println(c.Info("ℹ"), msg)
    } else {
        fmt.Println(c.Info("[INFO]"), msg)
    }
}

// Warn prints a warning line.
func (p Printer) Warn(msg string) {
    c := p.Colors
    if c.EmojiEnabled {
        fmt.Println(c.Warning("!"), msg)
    } else {
        fmt.Println(c.Warning("[WARN]"), msg)
    }
}

// Error prints an error line.
func (p Printer) Error(msg string) {
    c := p.Colors
    if c.EmojiEnabled {
        fmt.Println(c.Error("✗"), msg)
    } else {
        fmt.Println(c.Error("[ERR]"), msg)
    }
}

// Header prints a section header.
func (p Printer) Header(title string) {
    fmt.Println(p.Colors.Header(" " + title + " "))
}

// Separator prints a themed separator line of n characters.
func (p Printer) Separator(n int) { fmt.Println(p.Colors.Separator(n)) }

// Section prints a section header with separator
func (p Printer) Section(title string) {
    fmt.Println()
    fmt.Println(p.Colors.SubHeader(title))
    fmt.Println(p.Colors.Separator(40))
}

// MnemonicBox prints a mnemonic phrase with bold underlined title and clean formatting
func (p Printer) MnemonicBox(mnemonic string) {
    fmt.Println()

    // Bold + Underlined title in green
    title := "Recovery Mnemonic Phrase"
    boldUnderlineGreen := "\033[1m\033[4m" + p.Colors.Theme.Success
    fmt.Println(p.Colors.Apply(boldUnderlineGreen, title))

    // Separator line
    fmt.Println(p.Colors.Separator(len(title)))
    fmt.Println()

    // Split mnemonic into 3 lines (8 words per line for standard 24-word phrase)
    words := strings.Fields(mnemonic)
    wordsPerLine := 8

    for i := 0; i < len(words); i += wordsPerLine {
        end := i + wordsPerLine
        if end > len(words) {
            end = len(words)
        }
        line := strings.Join(words[i:end], " ")
        fmt.Println(p.Colors.Apply(p.Colors.Theme.Success, line))
    }

    fmt.Println()
}

// KeyValueLine prints a key-value pair with proper formatting
func (p Printer) KeyValueLine(key, value, colorType string) {
    var coloredValue string
    switch colorType {
    case "blue":
        coloredValue = p.Colors.Apply(p.Colors.Theme.Info, value)
    case "yellow":
        coloredValue = p.Colors.Apply(p.Colors.Theme.Warning, value)
    case "green":
        coloredValue = p.Colors.Apply(p.Colors.Theme.Success, value)
    case "dim":
        coloredValue = p.Colors.Apply(p.Colors.Theme.Description, value)
    default:
        coloredValue = p.Colors.Value(value)
    }
    fmt.Printf("%s %s\n", p.Colors.Label(key+":"), coloredValue)
}

