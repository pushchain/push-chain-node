package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// Event type constants
const (
	EventTypeTSSProcessInitiated = utsstypes.EventTypeTssProcessInitiated
	EventTypeOutboundCreated     = uexecutortypes.EventTypeOutboundCreated
)

// TSS event attribute keys.
const (
	AttrKeyProcessID    = "process_id"
	AttrKeyProcessType  = "process_type"
	AttrKeyParticipants = "participants"
	AttrKeyExpiryHeight = "expiry_height"
)

// TSS process type values as defined in the Push chain.
const (
	ChainProcessTypeKeygen       = "TSS_PROCESS_KEYGEN"
	ChainProcessTypeRefresh      = "TSS_PROCESS_REFRESH"
	ChainProcessTypeQuorumChange = "TSS_PROCESS_QUORUM_CHANGE"
)

// Outbound event attribute keys.
const (
	AttrKeyTxID             = "tx_id"
	AttrKeyUniversalTxID    = "utx_id"
	AttrKeyOutboundID       = "outbound_id"
	AttrKeyDestinationChain = "destination_chain"
	AttrKeyRecipient        = "recipient"
	AttrKeyAmount           = "amount"
	AttrKeyAssetAddr        = "asset_addr"
	AttrKeySender           = "sender"
	AttrKeyPayload          = "payload"
	AttrKeyGasLimit         = "gas_limit"
	AttrKeyTxType           = "tx_type"
	AttrKeyPcTxHash         = "pc_tx_hash"
	AttrKeyLogIndex         = "log_index"
	AttrKeyRevertMsg        = "revert_msg"
	AttrKeyData             = "data"
)

// Protocol type values for internal event classification.
const (
	ProtocolTypeKeygen       = "KEYGEN"
	ProtocolTypeKeyrefresh   = "KEYREFRESH"
	ProtocolTypeQuorumChange = "QUORUM_CHANGE"
	ProtocolTypeSign         = "SIGN"
)

// Event status values.
const (
	StatusPending = "PENDING"
)

// OutboundExpiryOffset is the number of blocks after event detection
// before an outbound event expires.
const OutboundExpiryOffset = 400

// Parser errors.
var (
	ErrMissingProcessID    = errors.New("missing required attribute: process_id")
	ErrMissingProcessType  = errors.New("missing required attribute: process_type")
	ErrMissingTxID         = errors.New("missing required attribute: tx_id")
	ErrInvalidProcessID    = errors.New("invalid process_id format")
	ErrInvalidExpiryHeight = errors.New("invalid expiry_height format")
	ErrInvalidParticipants = errors.New("invalid participants format")
)

// ParseEvent parses a Push chain event from an ABCI event.
// Returns nil if the event type is not recognized.
// Sets BlockHeight and Status on successfully parsed events.
func ParseEvent(event abci.Event, blockHeight uint64) (*store.Event, error) {
	var parsed *store.Event
	var err error

	switch event.Type {
	case EventTypeTSSProcessInitiated:
		parsed, err = parseTSSEvent(event)
	case EventTypeOutboundCreated:
		parsed, err = parseOutboundEvent(event)
	default:
		// Unknown event type - not an error, just skip
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse %s event: %w", event.Type, err)
	}

	if parsed == nil {
		return nil, nil
	}

	// Set common fields
	parsed.BlockHeight = blockHeight
	parsed.Status = StatusPending

	// Set expiry for outbound events (block seen + 400)
	if event.Type == EventTypeOutboundCreated {
		parsed.ExpiryBlockHeight = blockHeight + OutboundExpiryOffset
	}

	return parsed, nil
}

// parseTSSEvent parses a tss_process_initiated event.
func parseTSSEvent(event abci.Event) (*store.Event, error) {
	attrs := extractAttributes(event)

	// Parse required fields
	processIDStr, ok := attrs[AttrKeyProcessID]
	if !ok {
		return nil, ErrMissingProcessID
	}

	processID, err := strconv.ParseUint(processIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProcessID, err)
	}

	processTypeStr, ok := attrs[AttrKeyProcessType]
	if !ok {
		return nil, ErrMissingProcessType
	}

	protocolType := convertProcessType(processTypeStr)

	// Parse optional fields
	var expiryHeight uint64
	if expiryStr, ok := attrs[AttrKeyExpiryHeight]; ok {
		expiryHeight, err = strconv.ParseUint(expiryStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidExpiryHeight, err)
		}
	}

	var participants []string
	if participantsStr, ok := attrs[AttrKeyParticipants]; ok {
		if err := json.Unmarshal([]byte(participantsStr), &participants); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidParticipants, err)
		}
	}

	// Build event data
	eventData, err := buildTSSEventData(processID, participants)
	if err != nil {
		return nil, fmt.Errorf("failed to build event data: %w", err)
	}

	return &store.Event{
		EventID:           fmt.Sprintf("%d", processID),
		ExpiryBlockHeight: expiryHeight,
		Type:              protocolType,
		EventData:         eventData,
	}, nil
}

// parseOutboundEvent parses an outbound_created event.
func parseOutboundEvent(event abci.Event) (*store.Event, error) {
	attrs := extractAttributes(event)

	// Parse required field
	txID, ok := attrs[AttrKeyTxID]
	if !ok {
		return nil, ErrMissingTxID
	}

	// Build structured event data
	outboundData := uexecutortypes.OutboundCreatedEvent{
		UniversalTxId:    attrs[AttrKeyUniversalTxID],
		TxID:             txID,
		DestinationChain: attrs[AttrKeyDestinationChain],
		Recipient:        attrs[AttrKeyRecipient],
		Amount:           attrs[AttrKeyAmount],
		AssetAddr:        attrs[AttrKeyAssetAddr],
		Sender:           attrs[AttrKeySender],
		Payload:          attrs[AttrKeyPayload],
		GasLimit:         attrs[AttrKeyGasLimit],
		TxType:           attrs[AttrKeyTxType],
		PcTxHash:         attrs[AttrKeyPcTxHash],
		LogIndex:         attrs[AttrKeyLogIndex],
		RevertMsg:        attrs[AttrKeyRevertMsg],
	}

	eventData, err := json.Marshal(outboundData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal outbound event data: %w", err)
	}

	return &store.Event{
		EventID:   txID,
		Type:      ProtocolTypeSign,
		EventData: eventData,
	}, nil
}

// extractAttributes extracts all attributes from an ABCI event into a map.
func extractAttributes(event abci.Event) map[string]string {
	attrs := make(map[string]string, len(event.Attributes))
	for _, attr := range event.Attributes {
		attrs[attr.Key] = attr.Value
	}
	return attrs
}

// buildTSSEventData constructs the JSON event data for TSS events.
func buildTSSEventData(processID uint64, participants []string) ([]byte, error) {
	if len(participants) == 0 {
		return nil, nil
	}

	data := map[string]interface{}{
		"process_id":   processID,
		"participants": participants,
	}

	return json.Marshal(data)
}

// convertProcessType converts a chain process type to an internal protocol type.
func convertProcessType(chainType string) string {
	switch chainType {
	case ChainProcessTypeKeygen:
		return ProtocolTypeKeygen
	case ChainProcessTypeRefresh:
		return ProtocolTypeKeyrefresh
	case ChainProcessTypeQuorumChange:
		return ProtocolTypeQuorumChange
	default:
		// Return as-is for unknown types to maintain forward compatibility
		return chainType
	}
}
