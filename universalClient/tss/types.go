package tss

// ProtocolType enumerates the supported DKLS protocol flows.
type ProtocolType string

const (
	ProtocolKeygen     ProtocolType = "keygen"
	ProtocolKeyrefresh ProtocolType = "keyrefresh"
	ProtocolSign       ProtocolType = "sign"
)

// Participant represents a validator that can take part in a TSS session.
type Participant struct {
	PartyID    string
	PeerID     string
	Multiaddrs []string
}
