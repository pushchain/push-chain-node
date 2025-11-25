package libp2p

import "time"

// Config controls the libp2p network behaviour.
type Config struct {
	// ListenAddrs is the list of multiaddrs to bind to. Defaults to /ip4/0.0.0.0/tcp/0.
	ListenAddrs []string
	// ProtocolID is the stream protocol identifier. Defaults to /push/tss/1.0.0.
	ProtocolID string
	// PrivateKeyBase64 optionally contains a base64-encoded libp2p private key.
	// If empty, a fresh Ed25519 keypair is generated.
	PrivateKeyBase64 string
	// DialTimeout bounds outbound dial operations.
	DialTimeout time.Duration
	// IOTimeout bounds stream read/write operations.
	IOTimeout time.Duration
}

// setDefaults sets default values for unset fields.
func (c *Config) setDefaults() {
	if len(c.ListenAddrs) == 0 {
		c.ListenAddrs = []string{"/ip4/0.0.0.0/tcp/0"}
	}
	if c.ProtocolID == "" {
		c.ProtocolID = "/push/tss/1.0.0"
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 10 * time.Second
	}
	if c.IOTimeout == 0 {
		c.IOTimeout = 15 * time.Second
	}
}

