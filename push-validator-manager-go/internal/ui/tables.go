package ui

import (
    "regexp"
    "strings"
)

// Table renders a simple monospaced table with optional colorization using ColorConfig.
// widths optionally fixes column widths; when 0, width is computed from data (capped at maxWidth per col).
func Table(c *ColorConfig, headers []string, rows [][]string, widths []int) string {
    const maxWidth = 40
    // compute widths
    w := make([]int, len(headers))
    for i := range headers {
        w[i] = len(headers[i])
    }
    for _, r := range rows {
        for i := range r {
            if i >= len(w) { continue }
            if l := len(r[i]); l > w[i] {
                if l > maxWidth { l = maxWidth }
                w[i] = l
            }
        }
    }
    if len(widths) == len(w) {
        for i := range w {
            if widths[i] > 0 { w[i] = widths[i] }
        }
    }
    // header line
    var b strings.Builder
    // top title separator not included; caller can add
    // headers
    for i, h := range headers {
        if i > 0 { b.WriteString(" ") }
        b.WriteString(padCell(c.Label(h), w[i]))
    }
    b.WriteString("\n")
    // separator
    sepLen := 0
    for i := range w { sepLen += w[i]; if i < len(w)-1 { sepLen++ } }
    b.WriteString(strings.Repeat("-", sepLen))
    b.WriteString("\n")
    // rows
    for _, r := range rows {
        for i := range w {
            if i > 0 { b.WriteString(" ") }
            cell := ""
            if i < len(r) { cell = r[i] }
            if len(cell) > maxWidth { cell = cell[:maxWidth-1] + "…" }
            b.WriteString(padCell(c.Value(cell), w[i]))
        }
        b.WriteString("\n")
    }
    return b.String()
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleLen(s string) int {
    // strip ANSI escapes then measure
    plain := ansiRE.ReplaceAllString(s, "")
    return len([]rune(plain))
}

func padCell(s string, width int) string {
    v := visibleLen(s)
    if v >= width { return s }
    return s + strings.Repeat(" ", width-v)
}
