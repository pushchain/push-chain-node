package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestOutboundTx_ValidateBasic(t *testing.T) {
	validOutbound := types.OutboundTx{
		DestinationChain: "eip155:11155111",
		TxHash:           "0x123abc",
		Recipient:        "0x000000000000000000000000000000000000beef",
		Amount:           "1000",
		AssetAddr:        "0x000000000000000000000000000000000000cafe",
	}

	tests := []struct {
		name        string
		outbound    types.OutboundTx
		expectError bool
		errContains string
	}{
		{
			name:        "valid outbound",
			outbound:    validOutbound,
			expectError: false,
		},
		{
			name: "empty destination chain",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.DestinationChain = ""
				return ob
			}(),
			expectError: true,
			errContains: "destination_chain cannot be empty",
		},
		{
			name: "invalid destination chain format",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.DestinationChain = "eip155" // missing ":"
				return ob
			}(),
			expectError: true,
			errContains: "CAIP-2 format",
		},
		{
			name: "empty tx_hash",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.TxHash = ""
				return ob
			}(),
			expectError: true,
			errContains: "tx_hash cannot be empty",
		},
		{
			name: "empty recipient",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.Recipient = ""
				return ob
			}(),
			expectError: true,
			errContains: "recipient cannot be empty",
		},
		{
			name: "invalid recipient address",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.Recipient = "0xzzzzzzzz"
				return ob
			}(),
			expectError: true,
			errContains: "invalid recipient address",
		},
		{
			name: "empty amount",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.Amount = ""
				return ob
			}(),
			expectError: true,
			errContains: "amount cannot be empty",
		},
		{
			name: "negative amount",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.Amount = "-100"
				return ob
			}(),
			expectError: true,
			errContains: "amount must be a valid positive uint256",
		},
		{
			name: "empty asset_addr",
			outbound: func() types.OutboundTx {
				ob := validOutbound
				ob.AssetAddr = ""
				return ob
			}(),
			expectError: true,
			errContains: "asset_addr cannot be empty",
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
