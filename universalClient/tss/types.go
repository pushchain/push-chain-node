package tss

// ProtocolType enumerates the supported DKLS protocol flows.
type ProtocolType string

const (
	ProtocolKeygen     ProtocolType = "keygen"
	ProtocolKeyrefresh ProtocolType = "keyrefresh"
	ProtocolSign       ProtocolType = "sign"
)

// Universal Validator status
type UVStatus string

const (
	UVStatusUnspecified  UVStatus = "UV_STATUS_UNSPECIFIED"
	UVStatusActive       UVStatus = "UV_STATUS_ACTIVE"
	UVStatusPendingJoin  UVStatus = "UV_STATUS_PENDING_JOIN"
	UVStatusPendingLeave UVStatus = "UV_STATUS_PENDING_LEAVE"
	UVStatusInactive     UVStatus = "UV_STATUS_INACTIVE"
)

// Validator network metadata (optional sub-message)
type NetworkInfo struct {
	PeerID     string   `json:"peer_id"`
	Multiaddrs []string `json:"multiaddrs"`
}

// Core Universal Validator object
type UniversalValidator struct {
	ValidatorAddress string      `json:"validator_address"` // Core validator address (NodeID)
	Status           UVStatus    `json:"status"`            // Current lifecycle status
	Network          NetworkInfo `json:"network"`           // Metadata for networking
	JoinedAtBlock    int64       `json:"joined_at_block"`   // Block height when added to UV set
}

// PartyID returns the validator address (used as PartyID in TSS sessions)
func (uv *UniversalValidator) PartyID() string {
	return uv.ValidatorAddress
}

// PeerID returns the peer ID from the network info
func (uv *UniversalValidator) PeerID() string {
	return uv.Network.PeerID
}

// Multiaddrs returns the multiaddresses from the network info
func (uv *UniversalValidator) Multiaddrs() []string {
	return uv.Network.Multiaddrs
}
