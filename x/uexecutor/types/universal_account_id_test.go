package types_test

import (
	"strings"
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestUniversalAccountId_ValidateBasic(t *testing.T) {
	tests := []struct {
		name      string
		account   types.UniversalAccountId
		expectErr bool
		errType   string
	}{
		{
			name: "valid - Ethereum address (20 bytes)",
			account: types.UniversalAccountId{
				ChainNamespace: "eip155",
				ChainId:        "11155111",
				Owner:          "0x000000000000000000000000000000000000dead",
			},
			expectErr: false,
		},
		{
			name: "valid - Solana public key (32 bytes)",
			account: types.UniversalAccountId{
				ChainNamespace: "solana",
				ChainId:        "3zrWaMknHTRQpZSxY4BvQxw9TStSXiHcmcp3NMPTFkke",
				Owner:          "0x" + strings.Repeat("ab", 32), // 32 bytes
			},
			expectErr: false,
		},
		{
			name: "valid - Cosmos pubkey (33 bytes)",
			account: types.UniversalAccountId{
				ChainNamespace: "cosmos",
				ChainId:        "cosmoshub-4",
				Owner:          "0x" + strings.Repeat("11", 33), // 33 bytes
			},
			expectErr: false,
		},
		{
			name: "invalid - empty ChainNamespace",
			account: types.UniversalAccountId{
				ChainNamespace: "",
				ChainId:        "solana",
				Owner:          "0x" + strings.Repeat("ab", 32),
			},
			expectErr: true,
			errType:   "chain namespace cannot be empty",
		},
		{
			name: "invalid - empty ChainId",
			account: types.UniversalAccountId{
				ChainNamespace: "solana",
				ChainId:        "",
				Owner:          "0x" + strings.Repeat("ab", 32),
			},
			expectErr: true,
			errType:   "chain ID cannot be empty",
		},
		{
			name: "invalid - empty owner",
			account: types.UniversalAccountId{
				ChainNamespace: "eip155",
				ChainId:        "1",
				Owner:          "",
			},
			expectErr: true,
			errType:   "owner cannot be empty",
		},
		{
			name: "invalid - non-hex owner",
			account: types.UniversalAccountId{
				ChainNamespace: "eip155",
				ChainId:        "1",
				Owner:          "0xzzzzzzzz",
			},
			expectErr: true,
			errType:   "owner must be valid hex string",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.account.ValidateBasic()

			if tc.expectErr {
				require.Error(t, err)
				if tc.errType != "" {
					require.True(t, ErrContains(err, tc.errType), "expected error type: %v, got: %v", tc.errType, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
