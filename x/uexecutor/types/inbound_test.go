package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestInbound_ValidateBasic(t *testing.T) {
	validInbound := types.Inbound{
		SourceChain: "eip155:11155111",
		TxHash:      "0x123abc",
		Sender:      "0x000000000000000000000000000000000000dead",
		Recipient:   "0x000000000000000000000000000000000000beef",
		Amount:      "1000",
		AssetAddr:   "0x000000000000000000000000000000000000cafe",
		LogIndex:    "1",
		TxType:      types.InboundTxType_FUNDS,
	}

	tests := []struct {
		name        string
		inbound     types.Inbound
		expectError bool
		errContains string
	}{
		{
			name:        "valid inbound",
			inbound:     validInbound,
			expectError: false,
		},
		{
			name: "empty source chain",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.SourceChain = ""
				return ib
			}(),
			expectError: true,
			errContains: "source chain cannot be empty",
		},
		{
			name: "invalid source chain format",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.SourceChain = "eip155" // missing ":"
				return ib
			}(),
			expectError: true,
			errContains: "CAIP-2 format",
		},
		{
			name: "empty tx_hash",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.TxHash = ""
				return ib
			}(),
			expectError: true,
			errContains: "tx_hash cannot be empty",
		},
		{
			name: "empty sender",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.Sender = ""
				return ib
			}(),
			expectError: true,
			errContains: "sender cannot be empty",
		},
		{
			name: "empty recipient",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.Recipient = ""
				return ib
			}(),
			expectError: true,
			errContains: "recipient cannot be empty",
		},
		{
			name: "invalid recipient address",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.Recipient = "0xzzzzzzzz"
				return ib
			}(),
			expectError: true,
			errContains: "invalid recipient address",
		},
		{
			name: "empty amount",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.Amount = ""
				return ib
			}(),
			expectError: true,
			errContains: "amount cannot be empty",
		},
		{
			name: "negative amount",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.Amount = "-100"
				return ib
			}(),
			expectError: true,
			errContains: "amount must be a valid positive uint256",
		},
		{
			name: "empty asset_addr",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.AssetAddr = ""
				return ib
			}(),
			expectError: true,
			errContains: "asset_addr cannot be empty",
		},
		{
			name: "empty log_index",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.LogIndex = ""
				return ib
			}(),
			expectError: true,
			errContains: "log_index cannot be empty",
		},
		{
			name: "unspecified tx_type",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.TxType = types.InboundTxType_UNSPECIFIED_TX
				return ib
			}(),
			expectError: true,
			errContains: "invalid tx_type",
		},
		{
			name: "invalid tx_type out of range",
			inbound: func() types.Inbound {
				ib := validInbound
				ib.TxType = 99
				return ib
			}(),
			expectError: true,
			errContains: "invalid tx_type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.inbound.ValidateBasic()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
