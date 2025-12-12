package networking

import (
	"fmt"
	"strings"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/libp2p/go-libp2p/core/peer"
)

// PeerInfo contains information about a peer.
type PeerInfo struct {
	ID    string   // Peer identifier
	Addrs []string // List of multiaddrs
}

// ValidatePeerInfo validates peer information.
func ValidatePeerInfo(peerID string, addrs []string) error {
	if peerID == "" {
		return fmt.Errorf("peer ID cannot be empty")
	}
	if len(addrs) == 0 {
		return fmt.Errorf("peer must have at least one address")
	}

	// Validate multiaddrs
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			return fmt.Errorf("invalid multiaddr %q: %w", addr, err)
		}
		// Check if it's a valid network address
		if _, err := manet.ToNetAddr(maddr); err != nil {
			// Not a network address, might be a protocol-only address
			// This is okay, continue
		}
	}

	return nil
}

// ExtractPeerIDFromMultiaddr extracts the peer ID from a multiaddr if present.
func ExtractPeerIDFromMultiaddr(maddrStr string) (string, error) {
	maddr, err := ma.NewMultiaddr(maddrStr)
	if err != nil {
		return "", err
	}

	// Try to extract peer ID using peer.AddrInfoFromP2pAddr
	// This will work if the multiaddr contains /p2p/ component
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return "", nil // No peer ID in multiaddr
	}

	return info.ID.String(), nil
}

// NormalizeMultiaddr normalizes a multiaddr string.
func NormalizeMultiaddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("empty address")
	}

	maddr, err := ma.NewMultiaddr(addr)
	if err != nil {
		return "", fmt.Errorf("invalid multiaddr: %w", err)
	}

	return maddr.String(), nil
}

