package ui

import (
	"fmt"
	"strings"
)

// FormatNumber formats an integer with thousands separators
// Example: 1234567 -> "1,234,567"
func FormatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	// Insert commas from right to left
	var result strings.Builder
	for i, c := range reverse(s) {
		if i > 0 && i%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return reverse(result.String())
}

func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
