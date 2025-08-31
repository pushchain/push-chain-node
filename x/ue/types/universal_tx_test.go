package types_test

import (
	"testing"

	"github.com/rollchains/pchain/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestUniversalTx_ValidateBasic(t *testing.T) {
	validUniversal := types.UniversalTx{
		InboundTx: &types.Inbound{
			SourceChain: "eip155:11155111",
			TxHash:      "0x123abc",
			Sender:      "0x000000000000000000000000000000000000dead",
			Recipient:   "0x000000000000000000000000000000000000beef",
			Amount:      "1000",
			AssetAddr:   "0x000000000000000000000000000000000000cafe",
			LogIndex:    "1",
			TxType:      types.InboundTxType_SYNTHETIC,
		},
		PcTx: &types.PCTx{
			TxHash:      "0xabc123",
			Sender:      "0x000000000000000000000000000000000000dead",
			GasUsed:     21000,
			BlockHeight: 100,
			Status:      "SUCCESS",
		},
		OutboundTx: &types.OutboundTx{
			DestinationChain: "eip155:11155111",
			TxHash:           "0x456def",
			Recipient:        "0x000000000000000000000000000000000000beef",
			Amount:           "500",
			AssetAddr:        "0x000000000000000000000000000000000000cafe",
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
				utx.PcTx = &types.PCTx{} // BlockHeight = 0
				return utx
			}(),
			expectError: true,
			errContains: "invalid pc_tx",
		},
		{
			name: "invalid outbound",
			universal: func() types.UniversalTx {
				utx := validUniversal
				utx.OutboundTx = &types.OutboundTx{} // Recipient empty
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
