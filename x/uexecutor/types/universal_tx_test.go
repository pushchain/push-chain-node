package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestUniversalTx_ValidateBasic(t *testing.T) {
	validUniversal := types.UniversalTx{
		Id: "1",
		InboundTx: &types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0x123abc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.TxType_FUNDS,
		},
		PcTx: []*types.PCTx{
			{
				TxHash:      "0xabc123",
				Sender:      "0x000000000000000000000000000000000000dead",
				GasUsed:     21000,
				BlockHeight: 100,
				Status:      "SUCCESS",
			},
		},
		OutboundTx: []*types.OutboundTx{
			{
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
			},
		},
		UniversalStatus: types.UniversalTxStatus_PC_EXECUTED_SUCCESS,
	}

	tests := []struct {
		name        string
		universal   types.UniversalTx
		expectError bool
		errContains string
	}{
		{
			name:        "valid universal tx",
			universal:   validUniversal,
			expectError: false,
		},
		{
			name: "invalid inbound",
			universal: func() types.UniversalTx {
				utx := validUniversal
				utx.InboundTx = &types.Inbound{} // SourceChain empty
				return utx
			}(),
			expectError: true,
			errContains: "invalid inbound_tx",
		},
		{
			name: "invalid pc_tx",
			universal: func() types.UniversalTx {
				utx := validUniversal
				utx.PcTx = []*types.PCTx{
					{}, // invalid: missing required fields like BlockHeight
				}
				return utx
			}(),
			expectError: true,
			errContains: "invalid pc_tx",
		},
		{
			name: "invalid outbound",
			universal: func() types.UniversalTx {
				utx := validUniversal
				utx.OutboundTx = []*types.OutboundTx{
					{},
				} // Recipient empty
				return utx
			}(),
			expectError: true,
			errContains: "invalid outbound_tx",
		},
		{
			name: "invalid universal_status",
			universal: func() types.UniversalTx {
				utx := validUniversal
				utx.UniversalStatus = types.UniversalTxStatus(99) // not in enum
				return utx
			}(),
			expectError: true,
			errContains: "invalid universal_status",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.universal.ValidateBasic()
			if tc.expectError {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
