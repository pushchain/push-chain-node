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

// -----------------------------------------------------------------------------
// Fund Migration Events
// -----------------------------------------------------------------------------

const (
	EventTypeFundMigrationInitiated = "fund_migration_initiated"
	EventTypeFundMigrationCompleted = "fund_migration_completed"
)

// FundMigrationInitiatedEventData represents the emitted event when fund migration is initiated.
type FundMigrationInitiatedEventData struct {
	MigrationID      uint64 `json:"migration_id"`
	OldKeyID         string `json:"old_key_id"`
	OldTssPubkey     string `json:"old_tss_pubkey"`
	CurrentKeyID     string `json:"current_key_id"`
	CurrentTssPubkey string `json:"current_tss_pubkey"`
	Chain            string `json:"chain"`
	BlockHeight      int64  `json:"block_height"`
	GasPrice         string `json:"gas_price"`
	GasLimit         uint64 `json:"gas_limit"`
	L1GasFee         string `json:"l1_gas_fee"`
}

// NewFundMigrationInitiatedEvent creates and returns a Cosmos SDK event.
func NewFundMigrationInitiatedEvent(e FundMigrationInitiatedEventData) (sdk.Event, error) {
	bz, err := json.Marshal(e)
	if err != nil {
		return sdk.Event{}, fmt.Errorf("failed to marshal event: %w", err)
	}

	event := sdk.NewEvent(
		EventTypeFundMigrationInitiated,
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", e.MigrationID)),
		sdk.NewAttribute("old_key_id", e.OldKeyID),
		sdk.NewAttribute("old_tss_pubkey", e.OldTssPubkey),
		sdk.NewAttribute("current_key_id", e.CurrentKeyID),
		sdk.NewAttribute("current_tss_pubkey", e.CurrentTssPubkey),
		sdk.NewAttribute("chain", e.Chain),
		sdk.NewAttribute("gas_price", e.GasPrice),
		sdk.NewAttribute("gas_limit", fmt.Sprintf("%d", e.GasLimit)),
		sdk.NewAttribute("l1_gas_fee", e.L1GasFee),
		sdk.NewAttribute("data", string(bz)),
	)

	return event, nil
}

// FundMigrationCompletedEventData represents the emitted event when fund migration completes.
type FundMigrationCompletedEventData struct {
	MigrationID uint64 `json:"migration_id"`
	Chain       string `json:"chain"`
	TxHash      string `json:"tx_hash"`
	Success     bool   `json:"success"`
	BlockHeight int64  `json:"block_height"`
}

// NewFundMigrationCompletedEvent creates and returns a Cosmos SDK event.
func NewFundMigrationCompletedEvent(e FundMigrationCompletedEventData) (sdk.Event, error) {
	bz, err := json.Marshal(e)
	if err != nil {
		return sdk.Event{}, fmt.Errorf("failed to marshal event: %w", err)
	}

	event := sdk.NewEvent(
		EventTypeFundMigrationCompleted,
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", e.MigrationID)),
		sdk.NewAttribute("chain", e.Chain),
		sdk.NewAttribute("tx_hash", e.TxHash),
		sdk.NewAttribute("success", fmt.Sprintf("%t", e.Success)),
		sdk.NewAttribute("data", string(bz)),
	)

	return event, nil
}
