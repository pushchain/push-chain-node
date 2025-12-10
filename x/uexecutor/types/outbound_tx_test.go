package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func baseValidOutbound() types.OutboundTx {
	return types.OutboundTx{
		DestinationChain: "eip155:11155111",
		Recipient:        "0x000000000000000000000000000000000000beef",
		Sender:           "0x000000000000000000000000000000000000dead",
		Amount:           "1000",
		AssetAddr:        "0x000000000000000000000000000000000000cafe",
		Payload:          "0xabcdef",
		GasLimit:         "21000",
		TxType:           types.TxType_FUNDS_AND_PAYLOAD,
		PcTx: &types.OriginatingPcTx{
			TxHash:   "0xpc123",
			LogIndex: "1",
		},
		Id: "0",
	}
}

func TestOutboundTx_ValidateBasic(t *testing.T) {
	tests := []struct {
		name        string
		outbound    types.OutboundTx
		expectError bool
		errContains string
	}{
		{
			name: "valid FUNDS tx",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.TxType = types.TxType_FUNDS
				ob.Payload = ""
				return ob
			}(),
			expectError: false,
		},
		{
			name: "valid PAYLOAD tx",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.TxType = types.TxType_PAYLOAD
				ob.Amount = ""
				ob.AssetAddr = ""
				return ob
			}(),
			expectError: false,
		},
		{
			name: "empty destination_chain",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.DestinationChain = ""
				return ob
			}(),
			expectError: true,
			errContains: "destination_chain cannot be empty",
		},
		{
			name: "invalid CAIP-2 chain",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.DestinationChain = "eip155"
				return ob
			}(),
			expectError: true,
			errContains: "CAIP-2",
		},
		{
			name: "empty sender",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.Sender = ""
				return ob
			}(),
			expectError: true,
			errContains: "sender cannot be empty",
		},
		{
			name: "unsupported tx type",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.TxType = types.TxType_GAS
				return ob
			}(),
			expectError: true,
			errContains: "unsupported tx_type",
		},
		{
			name: "FUNDS tx missing amount",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.TxType = types.TxType_FUNDS
				ob.Amount = ""
				return ob
			}(),
			expectError: true,
			errContains: "amount cannot be empty",
		},
		{
			name: "PAYLOAD tx missing payload",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.TxType = types.TxType_PAYLOAD
				ob.Payload = ""
				return ob
			}(),
			expectError: true,
			errContains: "payload cannot be empty",
		},
		{
			name: "FUNDS tx missing asset_addr",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.TxType = types.TxType_FUNDS
				ob.AssetAddr = ""
				return ob
			}(),
			expectError: true,
			errContains: "asset_addr cannot be empty",
		},
		{
			name: "empty pc_tx hash",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.PcTx.TxHash = ""
				return ob
			}(),
			expectError: true,
			errContains: "pc_tx.tx_hash cannot be empty",
		},
		{
			name: "empty pc_tx log_index",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.PcTx.LogIndex = ""
				return ob
			}(),
			expectError: true,
			errContains: "pc_tx.log_index cannot be empty",
		},
		{
			name: "empty index",
			outbound: func() types.OutboundTx {
				ob := baseValidOutbound()
				ob.Id = ""
				return ob
			}(),
			expectError: true,
			errContains: "id cannot be empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.outbound.ValidateBasic()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
