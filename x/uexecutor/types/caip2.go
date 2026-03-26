package types

import (
	"fmt"
	"strings"
)

// ParseCAIP2 parses a CAIP-2 chain identifier (e.g., "eip155:11155111")
// into its namespace and chain reference components.
// Returns an error if the format is invalid.
func ParseCAIP2(chain string) (namespace string, chainId string, err error) {
	parts := strings.SplitN(chain, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid CAIP-2 identifier %q: missing ':'", chain)
	}
	if parts[0] == "" {
		return "", "", fmt.Errorf("invalid CAIP-2 identifier %q: empty namespace", chain)
	}
	if parts[1] == "" {
		return "", "", fmt.Errorf("invalid CAIP-2 identifier %q: empty chain reference", chain)
	}
	return parts[0], parts[1], nil
}
