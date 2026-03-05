package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestVaultMethods_ValidateBasic(t *testing.T) {
	tests := []struct {
		name      string
		config    types.VaultMethods
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid - EVM style",
			config: types.VaultMethods{
				Name:             "deposit",
				Identifier:       "0xb6b55f25",
				EventIdentifier:  "0x3c4e6c56cc5f2c26c92b91ee2f8bdc4e844b407bd1402b34ac1ef1f875d3c4b5",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
			},
			expectErr: false,
		},
		{
			name: "valid - Solana style",
			config: types.VaultMethods{
				Name:             "deposit_funds",
				Identifier:       "84ed4c39500ab38a",
				EventIdentifier:  "7f1f6cffbb134644",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_FAST,
			},
			expectErr: false,
		},
		{
			name: "invalid - empty name",
			config: types.VaultMethods{
				Name:             "",
				Identifier:       "a9059cbb",
				EventIdentifier:  "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a5f8d0b2f29",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
			},
			expectErr: true,
			errMsg:    "vault method name cannot be empty",
		},
		{
			name: "invalid - empty identifier",
			config: types.VaultMethods{
				Name:             "deposit",
				Identifier:       "",
				EventIdentifier:  "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a5f8d0b2f29",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
			},
			expectErr: true,
			errMsg:    "vault method identifier cannot be empty",
		},
		{
			name: "invalid - malformed identifier",
			config: types.VaultMethods{
				Name:             "deposit",
				Identifier:       "0xzzzzzz",
				EventIdentifier:  "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a5f8d0b2f29",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
			},
			expectErr: true,
			errMsg:    "vault method selector must be valid hex",
		},
		{
			name: "invalid - empty event_identifier",
			config: types.VaultMethods{
				Name:             "deposit",
				Identifier:       "a9059cbb",
				EventIdentifier:  "",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
			},
			expectErr: true,
			errMsg:    "vault method event_identifier cannot be empty",
		},
		{
			name: "invalid - malformed event_identifier",
			config: types.VaultMethods{
				Name:             "deposit",
				Identifier:       "a9059cbb",
				EventIdentifier:  "not_hex_topic!",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
			},
			expectErr: true,
			errMsg:    "vault method event_identifier must be valid hex",
		},
		{
			name: "invalid - unknown confirmation type",
			config: types.VaultMethods{
				Name:             "deposit",
				Identifier:       "a9059cbb",
				EventIdentifier:  "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a5f8d0b2f29",
				ConfirmationType: types.ConfirmationType_CONFIRMATION_UNKNOWN,
			},
			expectErr: true,
			errMsg:    "invalid vault method confirmation type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
