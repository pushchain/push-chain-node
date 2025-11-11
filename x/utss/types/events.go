package types

import (
	"encoding/json"
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypeTssProcessInitiated = "tss_process_initiated"
)

// TssProcessInitiatedEvent represents the emitted event when a new TSS key process starts.
type TssProcessInitiatedEvent struct {
	ProcessID    uint64 `json:"process_id"`
	ProcessType  string `json:"process_type"`
	Participants int    `json:"participants"`
	ExpiryHeight int64  `json:"expiry_height"`
}

// NewTssProcessInitiatedEvent creates and returns a Cosmos SDK event
func NewTssProcessInitiatedEvent(e TssProcessInitiatedEvent) (sdk.Event, error) {
	bz, err := json.Marshal(e)
	if err != nil {
		return sdk.Event{}, fmt.Errorf("failed to marshal event: %w", err)
	}

	event := sdk.NewEvent(
		EventTypeTssProcessInitiated,
		sdk.NewAttribute("process_id", fmt.Sprintf("%d", e.ProcessID)),
		sdk.NewAttribute("process_type", e.ProcessType),
		sdk.NewAttribute("participants", fmt.Sprintf("%d", e.Participants)),
		sdk.NewAttribute("expiry_height", fmt.Sprintf("%d", e.ExpiryHeight)),
		sdk.NewAttribute("data", string(bz)), // full JSON payload for off-chain consumption
	)

	return event, nil
}

// String returns a readable log for CLI
func (e TssProcessInitiatedEvent) String() string {
	return fmt.Sprintf(
		"TSS process initiated | ID: %d | Type: %s | Participants: %d | ExpiryHeight: %d",
		e.ProcessID, e.ProcessType, e.Participants, e.ExpiryHeight,
	)
}
