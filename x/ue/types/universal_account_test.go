package types_test

import (
	"strings"
	"testing"

	"github.com/rollchains/pchain/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestUniversalAccount_ValidateBasic(t *testing.T) {
	tests := []struct {
		name      string
		account   types.UniversalAccount
		expectErr bool
		errType   string
	}{
		{
			name: "valid - Ethereum address (20 bytes)",
			account: types.UniversalAccount{
				Chain: "eip155:11155111",
				Owner: "0x000000000000000000000000000000000000dead",
			},
			expectErr: false,
		},
		{
			name: "valid - Solana public key (32 bytes)",
			account: types.UniversalAccount{
				Chain: "solana:3zrWaMknHTRQpZSxY4BvQxw9TStSXiHcmcp3NMPTFkke",
				Owner: "0x" + strings.Repeat("ab", 32), // 32 bytes
			},
			expectErr: false,
		},
		{
			name: "valid - Cosmos pubkey (33 bytes)",
			account: types.UniversalAccount{
				Chain: "cosmos:cosmoshub-4",
				Owner: "0x" + strings.Repeat("11", 33), // 33 bytes
			},
			expectErr: false,
		},
		{
			name: "invalid - malformed CAIP-2 chain",
			account: types.UniversalAccount{
				Chain: "solana", // missing ':'
				Owner: "0x" + strings.Repeat("ab", 32),
			},
			expectErr: true,
			errType:   "chain must be in CAIP-2 format <namespace>:<reference>",
		},
		{
			name: "invalid - empty owner",
			account: types.UniversalAccount{
				Chain: "eip155:1",
				Owner: "",
			},
			expectErr: true,
			errType:   "owner cannot be empty",
		},
		{
			name: "invalid - non-hex owner",
			account: types.UniversalAccount{
				Chain: "eip155:1",
				Owner: "0xzzzzzzzz",
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
