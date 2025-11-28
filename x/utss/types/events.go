package types

import (
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypeTssProcessInitiated = "tss_process_initiated"
	EventTypeTssKeyFinalized     = "tss_key_finalized"
)

// TssProcessInitiatedEvent represents the emitted event when a new TSS key process starts.
type TssProcessInitiatedEvent struct {
	ProcessID    uint64   `json:"process_id"`
	ProcessType  string   `json:"process_type"`
	Participants []string `json:"participants"`
	ExpiryHeight int64    `json:"expiry_height"`
}

// NewTssProcessInitiatedEvent creates and returns a Cosmos SDK event.
func NewTssProcessInitiatedEvent(e TssProcessInitiatedEvent) (sdk.Event, error) {
	bz, err := json.Marshal(e)
	if err != nil {
		return sdk.Event{}, fmt.Errorf("failed to marshal event: %w", err)
	}

	participantsJSON, err := json.Marshal(e.Participants)
	if err != nil {
		return sdk.Event{}, fmt.Errorf("failed to marshal participants: %w", err)
	}

	event := sdk.NewEvent(
		EventTypeTssProcessInitiated,
		sdk.NewAttribute("process_id", fmt.Sprintf("%d", e.ProcessID)),
		sdk.NewAttribute("process_type", e.ProcessType),
		sdk.NewAttribute("participants", string(participantsJSON)),
		sdk.NewAttribute("expiry_height", fmt.Sprintf("%d", e.ExpiryHeight)),
		sdk.NewAttribute("data", string(bz)), // full JSON payload for off-chain consumption
	)

	return event, nil
}

// String returns a readable log for CLI.
func (e TssProcessInitiatedEvent) String() string {
	return fmt.Sprintf(
		"TSS process initiated | ID: %d | Type: %s | Participants: %v | ExpiryHeight: %d",
		e.ProcessID, e.ProcessType, e.Participants, e.ExpiryHeight,
	)
}

// -----------------------------------------------------------------------------
// Finalized Event
// -----------------------------------------------------------------------------

// TssKeyFinalizedEvent represents when a TSS keygen or reshare process completes successfully.
type TssKeyFinalizedEvent struct {
	ProcessID uint64 `json:"process_id"`
	KeyID     string `json:"key_id"`
	TssPubKey string `json:"tss_pubkey"`
}

// NewTssKeyFinalizedEvent creates and returns a Cosmos SDK event.
func NewTssKeyFinalizedEvent(e TssKeyFinalizedEvent) (sdk.Event, error) {
	bz, err := json.Marshal(e)
	if err != nil {
		return sdk.Event{}, fmt.Errorf("failed to marshal event: %w", err)
	}

	event := sdk.NewEvent(
		EventTypeTssKeyFinalized,
		sdk.NewAttribute("process_id", fmt.Sprintf("%d", e.ProcessID)),
		sdk.NewAttribute("key_id", e.KeyID),
		sdk.NewAttribute("tss_pubkey", e.TssPubKey),
		sdk.NewAttribute("data", string(bz)),
	)

	return event, nil
}

// String returns a readable log for CLI.
func (e TssKeyFinalizedEvent) String() string {
	return fmt.Sprintf(
		"TSS key finalized | ProcessID: %d | KeyID: %s | PubKey: %s",
		e.ProcessID, e.KeyID, e.TssPubKey,
	)
}
