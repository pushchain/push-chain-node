package utils

import (
	"fmt"
	"time"
)

// FormatDuration converts a duration to a human-readable string.
// For durations >= 1 second, it uses the standard time.Duration string format (e.g., "30s", "1m30s").
// For durations < 1 second, it shows milliseconds (e.g., "500ms").
func FormatDuration(d time.Duration) string {
	if d < 0 {
		return fmt.Sprintf("-%s", FormatDuration(-d))
	}
	
	// time.Duration.String() already provides good human-readable format
	// Examples: "2h45m30s", "30s", "500ms", "100µs"
	return d.String()
}

// FormatMilliseconds converts milliseconds to a human-readable duration string.
func FormatMilliseconds(ms int64) string {
	return FormatDuration(time.Duration(ms) * time.Millisecond)
}

// FormatSeconds converts seconds to a human-readable duration string.
func FormatSeconds(seconds int64) string {
	return FormatDuration(time.Duration(seconds) * time.Second)
}