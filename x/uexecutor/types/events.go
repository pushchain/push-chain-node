package types

import (
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypeOutboundCreated = "outbound_created"
)

// OutboundCreatedEvent represents an emitted outbound transaction.
type OutboundCreatedEvent struct {
	UniversalTxId    string `json:"utx_id"`
	OutboundId       string `json:"outbound_id"`
	DestinationChain string `json:"destination_chain"`
	Recipient        string `json:"recipient"`
	Amount           string `json:"amount"`
	AssetAddr        string `json:"asset_addr"`
	Sender           string `json:"sender"`
	Payload          string `json:"payload"`
	GasLimit         string `json:"gas_limit"`
	TxType           string `json:"tx_type"`
	PcTxHash         string `json:"pc_tx_hash"`
	LogIndex         string `json:"log_index"`
}

// NewOutboundCreatedEvent creates a Cosmos SDK event for outbound creation.
func NewOutboundCreatedEvent(e OutboundCreatedEvent) (sdk.Event, error) {
	bz, err := json.Marshal(e)
	if err != nil {
		return sdk.Event{}, fmt.Errorf("failed to marshal outbound event: %w", err)
	}

	event := sdk.NewEvent(
		EventTypeOutboundCreated,
		sdk.NewAttribute("utx_id", e.UniversalTxId),
		sdk.NewAttribute("outbound_id", e.OutboundId),
		sdk.NewAttribute("destination_chain", e.DestinationChain),
		sdk.NewAttribute("recipient", e.Recipient),
		sdk.NewAttribute("amount", e.Amount),
		sdk.NewAttribute("asset_addr", e.AssetAddr),
		sdk.NewAttribute("sender", e.Sender),
		sdk.NewAttribute("payload", e.Payload),
		sdk.NewAttribute("gas_limit", e.GasLimit),
		sdk.NewAttribute("tx_type", e.TxType),
		sdk.NewAttribute("pc_tx_hash", e.PcTxHash),
		sdk.NewAttribute("log_index", e.LogIndex),
		sdk.NewAttribute("data", string(bz)), // full JSON payload for indexers
	)

	return event, nil
}
