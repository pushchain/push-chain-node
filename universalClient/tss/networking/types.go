package networking

import "context"

// MessageHandler is called when a message is received from a peer.
// peerID: The ID of the peer that sent the message
// data: The raw message data
type MessageHandler func(peerID string, data []byte)

// Network provides peer-to-peer networking capabilities.
// It is protocol-agnostic and only handles raw bytes.
type Network interface {
	// ID returns the local peer identifier.
	ID() string

	// ListenAddrs returns the addresses that other peers can use to connect to this node.
	ListenAddrs() []string

	// RegisterHandler registers a callback for incoming messages.
	// The handler will be called for all incoming messages.
	RegisterHandler(handler MessageHandler) error

	// EnsurePeer registers a peer's address information.
	// peerID: The peer's identifier
	// addrs: List of multiaddrs where the peer can be reached
	EnsurePeer(peerID string, addrs []string) error

	// Send sends data to a peer.
	// peerID: The target peer's identifier
	// data: The raw data to send
	Send(ctx context.Context, peerID string, data []byte) error

	// Close releases all resources.
	Close() error
}
