package push

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// hashEventID generates a unique event ID by hashing eventType + ":" + rawID.
// This prevents collisions between different event types that may share numeric IDs.
func hashEventID(eventType, rawID string) string {
	h := sha256.Sum256([]byte(eventType + ":" + rawID))
	return hex.EncodeToString(h[:])
}

// DefaultExpiryOffset is the number of blocks after event detection
// before an event expires (~10 minutes at ~1s block time).
// Used for outbound and fund migration events.
const DefaultExpiryOffset = 600

// convertTssEvent converts a gRPC TssEvent to a store.Event.
func convertTssEvent(tssEvent *utsstypes.TssEvent) (*store.Event, error) {
	if tssEvent == nil {
		return nil, fmt.Errorf("tss event is nil")
	}

	var protocolType string
	switch tssEvent.ProcessType {
	case utsstypes.TssProcessType_TSS_PROCESS_KEYGEN.String():
		protocolType = store.EventTypeKeygen
	case utsstypes.TssProcessType_TSS_PROCESS_REFRESH.String():
		protocolType = store.EventTypeKeyrefresh
	case utsstypes.TssProcessType_TSS_PROCESS_QUORUM_CHANGE.String():
		protocolType = store.EventTypeQuorumChange
	default:
		return nil, fmt.Errorf("unknown process type: %s", tssEvent.ProcessType)
	}

	var eventData []byte
	if len(tssEvent.Participants) > 0 {
		var err error
		eventData, err = json.Marshal(map[string]interface{}{
			"process_id":   tssEvent.ProcessId,
			"participants": tssEvent.Participants,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal event data: %w", err)
		}
	}

	return &store.Event{
		EventID:           hashEventID(protocolType, fmt.Sprintf("%d", tssEvent.ProcessId)),
		BlockHeight:       uint64(tssEvent.BlockHeight),
		ExpiryBlockHeight: uint64(tssEvent.ExpiryHeight),
		Type:              protocolType,
		ConfirmationType:  store.ConfirmationInstant,
		Status:            store.StatusConfirmed,
		EventData:         eventData,
	}, nil
}


// convertFundMigrationEvent converts a FundMigration to a store.Event.
func convertFundMigrationEvent(migration *utsstypes.FundMigration) (*store.Event, error) {
	if migration == nil {
		return nil, fmt.Errorf("fund migration is nil")
	}

	eventData, err := json.Marshal(utsstypes.FundMigrationInitiatedEventData{
		MigrationID:      migration.Id,
		OldKeyID:         migration.OldKeyId,
		OldTssPubkey:     migration.OldTssPubkey,
		CurrentKeyID:     migration.CurrentKeyId,
		CurrentTssPubkey: migration.CurrentTssPubkey,
		Chain:            migration.Chain,
		BlockHeight:      migration.InitiatedBlock,
		GasPrice:         migration.GasPrice,
		GasLimit:         migration.GasLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fund migration event data: %w", err)
	}

	blockHeight := uint64(migration.InitiatedBlock)

	return &store.Event{
		EventID:           hashEventID(store.EventTypeSignFundMigrate, fmt.Sprintf("%d", migration.Id)),
		BlockHeight:       blockHeight,
		ExpiryBlockHeight: blockHeight + DefaultExpiryOffset,
		Type:              store.EventTypeSignFundMigrate,
		ConfirmationType:  store.ConfirmationInstant,
		Status:            store.StatusConfirmed,
		EventData:         eventData,
	}, nil
}

// convertOutboundToEvent converts a PendingOutboundEntry + OutboundTx to a store.Event.
func convertOutboundToEvent(entry *uexecutortypes.PendingOutboundEntry, outbound *uexecutortypes.OutboundTx) (*store.Event, error) {
	if entry == nil || outbound == nil {
		return nil, fmt.Errorf("entry or outbound is nil")
	}

	blockHeight := uint64(entry.CreatedAt)

	// Extract revert fund recipient if present
	var revertMsg string
	if outbound.RevertInstructions != nil {
		revertMsg = outbound.RevertInstructions.FundRecipient
	}

	// Extract originating PC tx fields
	var pcTxHash, logIndex string
	if outbound.PcTx != nil {
		pcTxHash = outbound.PcTx.TxHash
		logIndex = outbound.PcTx.LogIndex
	}

	outboundData := uexecutortypes.OutboundCreatedEvent{
		UniversalTxId:    entry.UniversalTxId,
		TxID:             outbound.Id,
		DestinationChain: outbound.DestinationChain,
		Recipient:        outbound.Recipient,
		Amount:           outbound.Amount,
		AssetAddr:        outbound.ExternalAssetAddr,
		Sender:           outbound.Sender,
		Payload:          outbound.Payload,
		GasFee:           outbound.GasFee,
		GasLimit:         outbound.GasLimit,
		GasPrice:         outbound.GasPrice,
		GasToken:         outbound.GasToken,
		TxType:           outbound.TxType.String(),
		PcTxHash:         pcTxHash,
		LogIndex:         logIndex,
		RevertMsg:        revertMsg,
	}

	eventData, err := json.Marshal(outboundData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal outbound event data: %w", err)
	}

	return &store.Event{
		EventID:           outbound.Id,
		BlockHeight:       blockHeight,
		ExpiryBlockHeight: blockHeight + DefaultExpiryOffset,
		Type:              store.EventTypeSignOutbound,
		ConfirmationType:  store.ConfirmationInstant,
		Status:            store.StatusConfirmed,
		EventData:         eventData,
	}, nil
}
