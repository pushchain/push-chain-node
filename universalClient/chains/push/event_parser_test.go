package push

import (
	"encoding/json"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestParseEvent_TSSEvent(t *testing.T) {
	tests := []struct {
		name        string
		event       abci.Event
		blockHeight uint64
		wantEventID string
		wantType    string
		wantExpiry  uint64
		wantErr     bool
		errContains string
	}{
		{
			name: "valid keygen event",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessID, Value: "123"},
					{Key: AttrKeyProcessType, Value: ChainProcessTypeKeygen},
					{Key: AttrKeyExpiryHeight, Value: "1000"},
					{Key: AttrKeyParticipants, Value: `["val1","val2","val3"]`},
				},
			},
			blockHeight: 500,
			wantEventID: "123",
			wantType:    common.EventTypeKeygen,
			wantExpiry:  1000,
			wantErr:     false,
		},
		{
			name: "valid refresh event",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessID, Value: "456"},
					{Key: AttrKeyProcessType, Value: ChainProcessTypeRefresh},
				},
			},
			blockHeight: 600,
			wantEventID: "456",
			wantType:    common.EventTypeKeyrefresh,
			wantExpiry:  0,
			wantErr:     false,
		},
		{
			name: "valid quorum change event",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessID, Value: "789"},
					{Key: AttrKeyProcessType, Value: ChainProcessTypeQuorumChange},
					{Key: AttrKeyExpiryHeight, Value: "2000"},
				},
			},
			blockHeight: 700,
			wantEventID: "789",
			wantType:    common.EventTypeQuorumChange,
			wantExpiry:  2000,
			wantErr:     false,
		},
		{
			name: "missing process_id",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessType, Value: ChainProcessTypeKeygen},
				},
			},
			blockHeight: 500,
			wantErr:     true,
			errContains: "process_id",
		},
		{
			name: "missing process_type",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessID, Value: "123"},
				},
			},
			blockHeight: 500,
			wantErr:     true,
			errContains: "process_type",
		},
		{
			name: "invalid process_id",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessID, Value: "not-a-number"},
					{Key: AttrKeyProcessType, Value: ChainProcessTypeKeygen},
				},
			},
			blockHeight: 500,
			wantErr:     true,
			errContains: "process_id",
		},
		{
			name: "invalid expiry_height",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessID, Value: "123"},
					{Key: AttrKeyProcessType, Value: ChainProcessTypeKeygen},
					{Key: AttrKeyExpiryHeight, Value: "invalid"},
				},
			},
			blockHeight: 500,
			wantErr:     true,
			errContains: "expiry_height",
		},
		{
			name: "invalid participants json",
			event: abci.Event{
				Type: EventTypeTSSProcessInitiated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyProcessID, Value: "123"},
					{Key: AttrKeyProcessType, Value: ChainProcessTypeKeygen},
					{Key: AttrKeyParticipants, Value: "not-valid-json"},
				},
			},
			blockHeight: 500,
			wantErr:     true,
			errContains: "participants",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseEvent(tt.event, tt.blockHeight)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.wantEventID, result.EventID)
			assert.Equal(t, tt.wantType, result.Type)
			assert.Equal(t, tt.wantExpiry, result.ExpiryBlockHeight)
			assert.Equal(t, tt.blockHeight, result.BlockHeight)
			assert.Equal(t, "CONFIRMED", result.Status)
			assert.Equal(t, "INSTANT", result.ConfirmationType)
		})
	}
}

func TestParseEvent_OutboundEvent(t *testing.T) {
	tests := []struct {
		name        string
		event       abci.Event
		blockHeight uint64
		wantEventID string
		wantExpiry  uint64
		wantErr     bool
		errContains string
	}{
		{
			name: "valid outbound event",
			event: abci.Event{
				Type: EventTypeOutboundCreated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyTxID, Value: "0x123abc"},
					{Key: AttrKeyUniversalTxID, Value: "utx-001"},
					{Key: AttrKeyOutboundID, Value: "out-001"},
					{Key: AttrKeyDestinationChain, Value: "ethereum"},
					{Key: AttrKeyRecipient, Value: "0xrecipient"},
					{Key: AttrKeyAmount, Value: "1000000"},
					{Key: AttrKeyAssetAddr, Value: "0xtoken"},
					{Key: AttrKeySender, Value: "0xsender"},
					{Key: AttrKeyPayload, Value: "0x"},
					{Key: AttrKeyGasLimit, Value: "21000"},
					{Key: AttrKeyTxType, Value: "TRANSFER"},
					{Key: AttrKeyPcTxHash, Value: "0xpctxhash"},
					{Key: AttrKeyLogIndex, Value: "0"},
					{Key: AttrKeyRevertMsg, Value: ""},
				},
			},
			blockHeight: 1000,
			wantEventID: "0x123abc",
			wantExpiry:  1000 + OutboundExpiryOffset, // blockHeight + 400
			wantErr:     false,
		},
		{
			name: "minimal outbound event (only tx_id)",
			event: abci.Event{
				Type: EventTypeOutboundCreated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyTxID, Value: "0xminimal"},
				},
			},
			blockHeight: 500,
			wantEventID: "0xminimal",
			wantExpiry:  500 + OutboundExpiryOffset,
			wantErr:     false,
		},
		{
			name: "missing tx_id",
			event: abci.Event{
				Type: EventTypeOutboundCreated,
				Attributes: []abci.EventAttribute{
					{Key: AttrKeyUniversalTxID, Value: "utx-001"},
				},
			},
			blockHeight: 500,
			wantErr:     true,
			errContains: "tx_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseEvent(tt.event, tt.blockHeight)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.wantEventID, result.EventID)
			assert.Equal(t, common.EventTypeSign, result.Type)
			assert.Equal(t, tt.wantExpiry, result.ExpiryBlockHeight)
			assert.Equal(t, tt.blockHeight, result.BlockHeight)
			assert.Equal(t, "CONFIRMED", result.Status)
			assert.Equal(t, "INSTANT", result.ConfirmationType)
		})
	}
}

func TestParseEvent_OutboundEventData(t *testing.T) {
	event := abci.Event{
		Type: EventTypeOutboundCreated,
		Attributes: []abci.EventAttribute{
			{Key: AttrKeyTxID, Value: "0x123abc"},
			{Key: AttrKeyUniversalTxID, Value: "utx-001"},
			{Key: AttrKeyOutboundID, Value: "out-001"},
			{Key: AttrKeyDestinationChain, Value: "ethereum"},
			{Key: AttrKeyRecipient, Value: "0xrecipient"},
			{Key: AttrKeyAmount, Value: "1000000"},
			{Key: AttrKeyAssetAddr, Value: "0xtoken"},
			{Key: AttrKeySender, Value: "0xsender"},
			{Key: AttrKeyPayload, Value: "0xpayload"},
			{Key: AttrKeyGasLimit, Value: "21000"},
			{Key: AttrKeyTxType, Value: "TRANSFER"},
			{Key: AttrKeyPcTxHash, Value: "0xpctxhash"},
			{Key: AttrKeyLogIndex, Value: "5"},
			{Key: AttrKeyRevertMsg, Value: "revert reason"},
		},
	}

	result, err := ParseEvent(event, 1000)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Unmarshal event data and verify all fields
	var data uexecutortypes.OutboundCreatedEvent
	err = json.Unmarshal(result.EventData, &data)
	require.NoError(t, err)

	assert.Equal(t, "0x123abc", data.TxID)
	assert.Equal(t, "utx-001", data.UniversalTxId)
	assert.Equal(t, "ethereum", data.DestinationChain)
	assert.Equal(t, "0xrecipient", data.Recipient)
	assert.Equal(t, "1000000", data.Amount)
	assert.Equal(t, "0xtoken", data.AssetAddr)
	assert.Equal(t, "0xsender", data.Sender)
	assert.Equal(t, "0xpayload", data.Payload)
	assert.Equal(t, "21000", data.GasLimit)
	assert.Equal(t, "TRANSFER", data.TxType)
	assert.Equal(t, "0xpctxhash", data.PcTxHash)
	assert.Equal(t, "5", data.LogIndex)
	assert.Equal(t, "revert reason", data.RevertMsg)
}

func TestParseEvent_UnknownEventType(t *testing.T) {
	event := abci.Event{
		Type: "unknown_event_type",
		Attributes: []abci.EventAttribute{
			{Key: "some_key", Value: "some_value"},
		},
	}

	result, err := ParseEvent(event, 1000)
	require.NoError(t, err)
	assert.Nil(t, result, "unknown event types should return nil without error")
}

func TestConvertProcessType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{ChainProcessTypeKeygen, common.EventTypeKeygen},
		{ChainProcessTypeRefresh, common.EventTypeKeyrefresh},
		{ChainProcessTypeQuorumChange, common.EventTypeQuorumChange},
		{"UNKNOWN_TYPE", "UNKNOWN_TYPE"}, // Unknown types returned as-is
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := convertProcessType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractAttributes(t *testing.T) {
	event := abci.Event{
		Type: "test",
		Attributes: []abci.EventAttribute{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key3", Value: ""},
		},
	}

	attrs := extractAttributes(event)

	assert.Len(t, attrs, 3)
	assert.Equal(t, "value1", attrs["key1"])
	assert.Equal(t, "value2", attrs["key2"])
	assert.Equal(t, "", attrs["key3"])
}

func TestBuildTSSEventData(t *testing.T) {
	t.Run("with participants", func(t *testing.T) {
		data, err := buildTSSEventData(123, []string{"val1", "val2"})
		require.NoError(t, err)
		require.NotNil(t, data)

		var result map[string]interface{}
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)

		assert.Equal(t, float64(123), result["process_id"])
		participants := result["participants"].([]interface{})
		assert.Len(t, participants, 2)
		assert.Equal(t, "val1", participants[0])
		assert.Equal(t, "val2", participants[1])
	})

	t.Run("without participants", func(t *testing.T) {
		data, err := buildTSSEventData(123, nil)
		require.NoError(t, err)
		assert.Nil(t, data)

		data, err = buildTSSEventData(123, []string{})
		require.NoError(t, err)
		assert.Nil(t, data)
	})
}

func TestOutboundExpiryOffset(t *testing.T) {
	// Verify the constant is set correctly
	assert.Equal(t, uint64(400), uint64(OutboundExpiryOffset))

	// Verify expiry calculation
	blockHeight := uint64(1000)
	event := abci.Event{
		Type: EventTypeOutboundCreated,
		Attributes: []abci.EventAttribute{
			{Key: AttrKeyTxID, Value: "0xtest"},
		},
	}

	result, err := ParseEvent(event, blockHeight)
	require.NoError(t, err)
	assert.Equal(t, blockHeight+OutboundExpiryOffset, result.ExpiryBlockHeight)
}
