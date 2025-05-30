package types

import (
	"fmt"
	"strings"
)

// CAIPAddress represents a parsed CAIP-19 address
// Format: namespace:reference:address
// Example: eip155:1:0x123...
type CAIPAddress struct {
	Namespace string // e.g., "eip155", "solana"
	Reference string // e.g., "1" (Ethereum mainnet), "11155111" (Sepolia)
	Address   string // The actual address on the chain
}

// ParseCAIPAddress parses a CAIP-formatted address
func ParseCAIPAddress(caip string) (*CAIPAddress, error) {
	parts := strings.Split(caip, ":")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid CAIP address format, expected 'namespace:reference:address', got %s", caip)
	}

	return &CAIPAddress{
		Namespace: parts[0],
		Reference: parts[1],
		Address:   parts[2],
	}, nil
}

// GetChainIdentifier returns the chain identifier (namespace:reference) part of the CAIP address
func (ca *CAIPAddress) GetChainIdentifier() string {
	return fmt.Sprintf("%s:%s", ca.Namespace, ca.Reference)
}

// String returns the full CAIP address
func (ca *CAIPAddress) String() string {
	return fmt.Sprintf("%s:%s:%s", ca.Namespace, ca.Reference, ca.Address)
}
