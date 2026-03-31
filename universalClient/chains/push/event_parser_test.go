package push

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/store"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

func TestConvertTssEvent(t *testing.T) {
	t.Run("nil event returns error", func(t *testing.T) {
		result, err := convertTssEvent(nil)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "tss event is nil")
	})

	t.Run("unknown process type returns error", func(t *testing.T) {
		result, err := convertTssEvent(&utsstypes.TssEvent{
			ProcessId:   999,
			ProcessType: "UNKNOWN_TYPE",
		})
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "unknown process type: UNKNOWN_TYPE")
	})

	t.Run("keygen event with participants", func(t *testing.T) {
		tssEvent := &utsstypes.TssEvent{
			Id:           1,
			EventType:    utsstypes.TssEventType_TSS_EVENT_PROCESS_INITIATED,
			Status:       utsstypes.TssEventStatus_TSS_EVENT_ACTIVE,
			ProcessId:    123,
			ProcessType:  utsstypes.TssProcessType_TSS_PROCESS_KEYGEN.String(),
			Participants: []string{"val1", "val2", "val3"},
			ExpiryHeight: 1000,
			BlockHeight:  500,
		}

		result, err := convertTssEvent(tssEvent)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, "123", result.EventID)
		assert.Equal(t, store.EventTypeKeygen, result.Type)
		assert.Equal(t, uint64(500), result.BlockHeight)
		assert.Equal(t, uint64(1000), result.ExpiryBlockHeight)
		assert.Equal(t, store.StatusConfirmed, result.Status)
		assert.Equal(t, store.ConfirmationInstant, result.ConfirmationType)

		// Verify event data JSON
		require.NotNil(t, result.EventData)
		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result.EventData, &data))
		assert.Equal(t, float64(123), data["process_id"])
		participants := data["participants"].([]interface{})
		assert.Len(t, participants, 3)
		assert.Equal(t, "val1", participants[0])
	})

	t.Run("refresh event", func(t *testing.T) {
		result, err := convertTssEvent(&utsstypes.TssEvent{
			ProcessId:   456,
			ProcessType: utsstypes.TssProcessType_TSS_PROCESS_REFRESH.String(),
			BlockHeight: 600,
		})
		require.NoError(t, err)
		assert.Equal(t, "456", result.EventID)
		assert.Equal(t, store.EventTypeKeyrefresh, result.Type)
		assert.Equal(t, uint64(600), result.BlockHeight)
		assert.Nil(t, result.EventData) // no participants
	})

	t.Run("quorum change event", func(t *testing.T) {
		result, err := convertTssEvent(&utsstypes.TssEvent{
			ProcessId:    789,
			ProcessType:  utsstypes.TssProcessType_TSS_PROCESS_QUORUM_CHANGE.String(),
			ExpiryHeight: 2000,
			BlockHeight:  700,
		})
		require.NoError(t, err)
		assert.Equal(t, "789", result.EventID)
		assert.Equal(t, store.EventTypeQuorumChange, result.Type)
		assert.Equal(t, uint64(2000), result.ExpiryBlockHeight)
	})

	t.Run("empty participants produces nil event data", func(t *testing.T) {
		result, err := convertTssEvent(&utsstypes.TssEvent{
			ProcessId:    100,
			ProcessType:  utsstypes.TssProcessType_TSS_PROCESS_KEYGEN.String(),
			Participants: []string{},
		})
		require.NoError(t, err)
		assert.Nil(t, result.EventData)
	})
}

func TestConvertOutboundToEvent(t *testing.T) {
	t.Run("nil entry returns error", func(t *testing.T) {
		result, err := convertOutboundToEvent(nil, &uexecutortypes.OutboundTx{})
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("nil outbound returns error", func(t *testing.T) {
		result, err := convertOutboundToEvent(&uexecutortypes.PendingOutboundEntry{}, nil)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("full outbound with all fields", func(t *testing.T) {
		entry := &uexecutortypes.PendingOutboundEntry{
			OutboundId:    "0x123abc",
			UniversalTxId: "utx-001",
			CreatedAt:     1000,
		}
		outbound := &uexecutortypes.OutboundTx{
			Id:                "0x123abc",
			DestinationChain:  "eip155:1",
			Recipient:         "0xrecipient",
			Amount:            "1000000",
			ExternalAssetAddr: "0xtoken",
			Sender:            "0xsender",
			Payload:           "0xpayload",
			GasFee:            "21000",
			GasLimit:          "100000",
			GasPrice:          "50",
			GasToken:          "ETH",
			TxType:            uexecutortypes.TxType_FUNDS,
			PcTx: &uexecutortypes.OriginatingPcTx{
				TxHash:   "0xpctxhash",
				LogIndex: "5",
			},
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: "0xrevert",
			},
		}

		result, err := convertOutboundToEvent(entry, outbound)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify store.Event fields
		assert.Equal(t, "0x123abc", result.EventID)
		assert.Equal(t, store.EventTypeSignOutbound, result.Type)
		assert.Equal(t, uint64(1000), result.BlockHeight)
		assert.Equal(t, uint64(1000+DefaultExpiryOffset), result.ExpiryBlockHeight)
		assert.Equal(t, store.StatusConfirmed, result.Status)
		assert.Equal(t, store.ConfirmationInstant, result.ConfirmationType)

		// Verify OutboundCreatedEvent JSON
		var data uexecutortypes.OutboundCreatedEvent
		require.NoError(t, json.Unmarshal(result.EventData, &data))
		assert.Equal(t, "0x123abc", data.TxID)
		assert.Equal(t, "utx-001", data.UniversalTxId)
		assert.Equal(t, "eip155:1", data.DestinationChain)
		assert.Equal(t, "0xrecipient", data.Recipient)
		assert.Equal(t, "1000000", data.Amount)
		assert.Equal(t, "0xtoken", data.AssetAddr)
		assert.Equal(t, "0xsender", data.Sender)
		assert.Equal(t, "0xpayload", data.Payload)
		assert.Equal(t, "21000", data.GasFee)
		assert.Equal(t, "100000", data.GasLimit)
		assert.Equal(t, "50", data.GasPrice)
		assert.Equal(t, "ETH", data.GasToken)
		assert.Equal(t, "FUNDS", data.TxType)
		assert.Equal(t, "0xpctxhash", data.PcTxHash)
		assert.Equal(t, "5", data.LogIndex)
		assert.Equal(t, "0xrevert", data.RevertMsg)
	})

	t.Run("outbound without PcTx and RevertInstructions", func(t *testing.T) {
		entry := &uexecutortypes.PendingOutboundEntry{
			OutboundId:    "0xminimal",
			UniversalTxId: "utx-002",
			CreatedAt:     500,
		}
		outbound := &uexecutortypes.OutboundTx{
			Id:               "0xminimal",
			DestinationChain: "eip155:1",
			Amount:           "100",
		}

		result, err := convertOutboundToEvent(entry, outbound)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, "0xminimal", result.EventID)
		assert.Equal(t, uint64(500), result.BlockHeight)
		assert.Equal(t, uint64(500+DefaultExpiryOffset), result.ExpiryBlockHeight)

		var data uexecutortypes.OutboundCreatedEvent
		require.NoError(t, json.Unmarshal(result.EventData, &data))
		assert.Empty(t, data.PcTxHash)
		assert.Empty(t, data.LogIndex)
		assert.Empty(t, data.RevertMsg)
		assert.Equal(t, "utx-002", data.UniversalTxId)
	})

	t.Run("outbound with RevertInstructions but no PcTx", func(t *testing.T) {
		entry := &uexecutortypes.PendingOutboundEntry{
			OutboundId: "0xrevert-only",
			CreatedAt:  300,
		}
		outbound := &uexecutortypes.OutboundTx{
			Id: "0xrevert-only",
			RevertInstructions: &uexecutortypes.RevertInstructions{
				FundRecipient: "0xfundrecipient",
			},
		}

		result, err := convertOutboundToEvent(entry, outbound)
		require.NoError(t, err)

		var data uexecutortypes.OutboundCreatedEvent
		require.NoError(t, json.Unmarshal(result.EventData, &data))
		assert.Equal(t, "0xfundrecipient", data.RevertMsg)
		assert.Empty(t, data.PcTxHash)
	})

	t.Run("outbound with PcTx but no RevertInstructions", func(t *testing.T) {
		entry := &uexecutortypes.PendingOutboundEntry{
			OutboundId: "0xpctx-only",
			CreatedAt:  400,
		}
		outbound := &uexecutortypes.OutboundTx{
			Id: "0xpctx-only",
			PcTx: &uexecutortypes.OriginatingPcTx{
				TxHash:   "0xhash",
				LogIndex: "3",
			},
		}

		result, err := convertOutboundToEvent(entry, outbound)
		require.NoError(t, err)

		var data uexecutortypes.OutboundCreatedEvent
		require.NoError(t, json.Unmarshal(result.EventData, &data))
		assert.Equal(t, "0xhash", data.PcTxHash)
		assert.Equal(t, "3", data.LogIndex)
		assert.Empty(t, data.RevertMsg)
	})
}

func TestDefaultExpiryOffset(t *testing.T) {
	assert.Equal(t, uint64(600), uint64(DefaultExpiryOffset))
}

func TestConvertFundMigrationEvent(t *testing.T) {
	t.Run("nil migration returns error", func(t *testing.T) {
		result, err := convertFundMigrationEvent(nil)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "fund migration is nil")
	})

	t.Run("valid migration converts correctly", func(t *testing.T) {
		migration := &utsstypes.FundMigration{
			Id:               1,
			OldKeyId:         "old-key-001",
			OldTssPubkey:     "0x02abc123",
			CurrentKeyId:     "new-key-002",
			CurrentTssPubkey: "0x03def456",
			Chain:            "eip155:421614",
			InitiatedBlock:   5000,
		}

		result, err := convertFundMigrationEvent(migration)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, "fm_1", result.EventID)
		assert.Equal(t, store.EventTypeSignFundMigrate, result.Type)
		assert.Equal(t, store.StatusConfirmed, result.Status)
		assert.Equal(t, store.ConfirmationInstant, result.ConfirmationType)
		assert.Equal(t, uint64(5000), result.BlockHeight)
		assert.Equal(t, uint64(5000+DefaultExpiryOffset), result.ExpiryBlockHeight)

		var data utsstypes.FundMigrationInitiatedEventData
		require.NoError(t, json.Unmarshal(result.EventData, &data))
		assert.Equal(t, uint64(1), data.MigrationID)
		assert.Equal(t, "old-key-001", data.OldKeyID)
		assert.Equal(t, "0x02abc123", data.OldTssPubkey)
		assert.Equal(t, "new-key-002", data.CurrentKeyID)
		assert.Equal(t, "0x03def456", data.CurrentTssPubkey)
		assert.Equal(t, "eip155:421614", data.Chain)
		assert.Equal(t, int64(5000), data.BlockHeight)
	})

	t.Run("event ID uses fm_ prefix", func(t *testing.T) {
		migration := &utsstypes.FundMigration{
			Id:             42,
			InitiatedBlock: 100,
		}

		result, err := convertFundMigrationEvent(migration)
		require.NoError(t, err)
		assert.Equal(t, "fm_42", result.EventID)
	})
}
