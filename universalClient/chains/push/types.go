package push

import "time"

// Event type constants from the utss module.
const (
	// EventTypeTssProcessInitiated is emitted when a TSS key process is initiated on-chain.
	EventTypeTssProcessInitiated = "tss_process_initiated"

	// Event attribute keys
	AttrKeyProcessID    = "process_id"
	AttrKeyProcessType  = "process_type"
	AttrKeyParticipants = "participants"
	AttrKeyExpiryHeight = "expiry_height"

	// Process type values from the chain
	ProcessTypeKeygen       = "TSS_PROCESS_KEYGEN"
	ProcessTypeRefresh      = "TSS_PROCESS_REFRESH"
	ProcessTypeQuorumChange = "TSS_PROCESS_QUORUM_CHANGE"
)

// Protocol type values for PCEvent.Type field.
const (
	ProtocolTypeKeygen       = "KEYGEN"
	ProtocolTypeKeyrefresh   = "KEYREFRESH"
	ProtocolTypeQuorumChange = "QUORUM_CHANGE"
	ProtocolTypeSign         = "SIGN"
)

// Default configuration values.
const (
	DefaultPollInterval = 5 * time.Second
	DefaultEventQuery   = EventTypeTssProcessInitiated + ".process_id>=0"
)

// TSSProcessEvent represents a parsed tss_process_initiated event from the chain.
type TSSProcessEvent struct {
	ProcessID    uint64   // Process ID from the event
	ProcessType  string   // "keygen" or "keyrefresh"
	Participants []string // List of validator addresses
	ExpiryHeight uint64   // Block height when this process expires
	BlockHeight  uint64   // Block height when the event occurred
	TxHash       string   // Transaction hash containing this event
}
