package types_test

import (
	"testing"

	"github.com/rollchains/pchain/x/registry/types"
	"github.com/stretchr/testify/require"
)

func TestGatewayMethods_ValidateBasic(t *testing.T) {
	tests := []struct {
		name      string
		config    types.GatewayMethods
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid - EVM style",
			config: types.GatewayMethods{
				Name:            "addFunds",
				Identifier:      "0xf9bfe8a7",
				EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
			},
			expectErr: false,
		},
		{
			name: "valid - Solana style",
			config: types.GatewayMethods{
				Name:            "add_funds",
				Identifier:      "84ed4c39500ab38a",
				EventIdentifier: "7f1f6cffbb134644",
			},
			expectErr: false,
		},
		{
			name: "invalid - empty name",
			config: types.GatewayMethods{
				Name:            "",
				Identifier:      "a9059cbb",
				EventIdentifier: "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a5f8d0b2f29",
			},
			expectErr: true,
			errMsg:    "method name cannot be empty",
		},
		{
			name: "invalid - empty identifier",
			config: types.GatewayMethods{
				Name:            "transfer",
				Identifier:      "",
				EventIdentifier: "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a5f8d0b2f29",
			},
			expectErr: true,
			errMsg:    "method identifier cannot be empty",
		},
		{
			name: "invalid - malformed identifier",
			config: types.GatewayMethods{
				Name:            "transfer",
				Identifier:      "0xzzzzzz",
				EventIdentifier: "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a5f8d0b2f29",
			},
			expectErr: true,
			errMsg:    "method selector must be valid hex",
		},
		{
			name: "invalid - empty event_identifier",
			config: types.GatewayMethods{
				Name:            "transfer",
				Identifier:      "a9059cbb",
				EventIdentifier: "",
			},
			expectErr: true,
			errMsg:    "method event_identifier cannot be empty",
		},
		{
			name: "invalid - malformed event_identifier",
			config: types.GatewayMethods{
				Name:            "transfer",
				Identifier:      "a9059cbb",
				EventIdentifier: "not_hex_topic!",
			},
			expectErr: true,
			errMsg:    "method event_identifier must be valid hex",
		},
		{
			name: "valid - with 0x prefix",
			config: types.GatewayMethods{
				Name:            "mint",
				Identifier:      "0xa0712d68",
				EventIdentifier: "0x3c4e6c56cc5f2c26c92b91ee2f8bdc4e844b407bd1402b34ac1ef1f875d3c4b5",
			},
			expectErr: false,
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
