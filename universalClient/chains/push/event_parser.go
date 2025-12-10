package push

import (
	"encoding/json"
	"fmt"
	"strconv"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/eventstore"
)

// ParseTSSProcessInitiatedEvent parses a tss_process_initiated event from ABCI events.
// Returns nil if the event is not a tss_process_initiated event.
func ParseTSSProcessInitiatedEvent(events []abci.Event, blockHeight uint64, txHash string) (*TSSProcessEvent, error) {
	for _, event := range events {
		if event.Type != EventTypeTssProcessInitiated {
			continue
		}

		parsed := &TSSProcessEvent{
			BlockHeight: blockHeight,
			TxHash:      txHash,
		}

		// Track which required fields were found (process_id=0 is valid!)
		foundProcessID := false

		for _, attr := range event.Attributes {
			switch attr.Key {
			case AttrKeyProcessID:
				id, err := strconv.ParseUint(attr.Value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse process_id: %w", err)
				}
				parsed.ProcessID = id
				foundProcessID = true

			case AttrKeyProcessType:
				parsed.ProcessType = convertProcessType(attr.Value)

			case AttrKeyParticipants:
				var participants []string
				if err := json.Unmarshal([]byte(attr.Value), &participants); err != nil {
					return nil, fmt.Errorf("failed to parse participants: %w", err)
				}
				parsed.Participants = participants

			case AttrKeyExpiryHeight:
				height, err := strconv.ParseUint(attr.Value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse expiry_height: %w", err)
				}
				parsed.ExpiryHeight = height
			}
		}

		// Validate required fields
		if !foundProcessID {
			return nil, fmt.Errorf("missing process_id in event")
		}
		if parsed.ProcessType == "" {
			return nil, fmt.Errorf("missing process_type in event")
		}

		return parsed, nil
	}

	return nil, nil // No tss_process_initiated event found
}

// convertProcessType converts chain process type to internal protocol type.
func convertProcessType(chainType string) string {
	switch chainType {
	case ProcessTypeKeygen:
		return ProtocolTypeKeygen
	case ProcessTypeRefresh:
		return ProtocolTypeKeyrefresh
	case ProcessTypeQuorumChange:
		return ProtocolTypeQuorumChange
	default:
		return chainType // Return as-is if unknown
	}
}

// ToTSSEventRecord converts the parsed event to a TSSEvent database record.
func (e *TSSProcessEvent) ToTSSEventRecord() *store.TSSEvent {
	// Serialize participants as event data
	var eventData []byte
	if len(e.Participants) > 0 {
		data := map[string]interface{}{
			"process_id": e.ProcessID,
			// TODO: Maybe while tss process participants can be read from this rather than chain
			"participants": e.Participants,
			"tx_hash":      e.TxHash,
		}
		eventData, _ = json.Marshal(data)
	}

	return &store.TSSEvent{
		EventID:      e.EventID(),
		BlockNumber:  e.BlockHeight,
		ProtocolType: e.ProcessType,
		Status:       eventstore.StatusPending,
		ExpiryHeight: e.ExpiryHeight,
		EventData:    eventData,
	}
}

// EventID returns the unique event ID for this process.
// Format: "{process_id}"
func (e *TSSProcessEvent) EventID() string {
	return fmt.Sprintf("%d", e.ProcessID)
}
