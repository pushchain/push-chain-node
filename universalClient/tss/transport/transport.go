package transport

import "context"

// Handler receives decoded payloads from the underlying transport.
type Handler func(ctx context.Context, sender string, payload []byte) error

// Transport abstracts message delivery between TSS nodes.
type Transport interface {
	// ID returns the local peer identifier.
	ID() string
	// ListenAddrs returns the addresses peers can dial.
	ListenAddrs() []string
	// RegisterHandler installs the callback for inbound payloads (must be called once).
	RegisterHandler(Handler) error
	// EnsurePeer lets the transport know how to reach a remote peer.
	EnsurePeer(peerID string, addrs []string) error
	// Send delivers a payload to the given peer.
	Send(ctx context.Context, peerID string, payload []byte) error
	// Close releases any underlying resources.
	Close() error
}
