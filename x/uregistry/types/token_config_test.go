package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestTokenConfig_ValidateBasic(t *testing.T) {
	validNative := &types.NativeRepresentation{
		Denom:           "uatom",
		ContractAddress: "0x1234567890abcdef1234567890abcdef12345678",
	}

	tests := []struct {
		name      string
		config    types.TokenConfig
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid token config",
			config: types.TokenConfig{
				Chain:                "eip155:1",
				Address:              "0xabc123",
				Name:                 "USD Coin",
				Symbol:               "USDC",
				Decimals:             6,
				Enabled:              true,
				LiquidityCap:         "1000000000000000000000000",
				TokenType:            types.TokenType_ERC20,
				NativeRepresentation: validNative,
			},
			expectErr: false,
		},
		{
			name: "missing chain",
			config: types.TokenConfig{
				Chain:                "",
				Address:              "0xabc123",
				Name:                 "USD Coin",
				Symbol:               "USDC",
				Decimals:             6,
				LiquidityCap:         "1000000000000000000000000",
				TokenType:            types.TokenType_ERC20,
				NativeRepresentation: validNative,
			},
			expectErr: true,
			errMsg:    "chain cannot be empty",
		},
		{
			name: "missing address",
			config: types.TokenConfig{
				Chain:                "eip155:1",
				Address:              "",
				Name:                 "USD Coin",
				Symbol:               "USDC",
				Decimals:             6,
				LiquidityCap:         "1000000000000000000000000",
				TokenType:            types.TokenType_ERC20,
				NativeRepresentation: validNative,
			},
			expectErr: true,
			errMsg:    "token contract address cannot be empty",
		},
		{
			name: "zero decimals",
			config: types.TokenConfig{
				Chain:                "eip155:1",
				Address:              "0xabc123",
				Name:                 "USD Coin",
				Symbol:               "USDC",
				Decimals:             0,
				LiquidityCap:         "1000000000000000000000000",
				TokenType:            types.TokenType_ERC20,
				NativeRepresentation: validNative,
			},
			expectErr: true,
			errMsg:    "decimals must be greater than zero",
		},
		{
			name: "invalid token type",
			config: types.TokenConfig{
				Chain:                "eip155:1",
				Address:              "0xabc123",
				Name:                 "USD Coin",
				Symbol:               "USDC",
				Decimals:             6,
				LiquidityCap:         "1000000000000000000000000",
				TokenType:            99,
				NativeRepresentation: validNative,
			},
			expectErr: true,
			errMsg:    "invalid token_type",
		},
		{
			name: "missing liquidity cap",
			config: types.TokenConfig{
				Chain:                "eip155:1",
				Address:              "0xabc123",
				Name:                 "USD Coin",
				Symbol:               "USDC",
				Decimals:             6,
				LiquidityCap:         "",
				TokenType:            types.TokenType_ERC20,
				NativeRepresentation: validNative,
			},
			expectErr: true,
			errMsg:    "liquidity_cap cannot be empty",
		},
		{
			name: "invalid native representation contract address",
			config: types.TokenConfig{
				Chain:        "eip155:1",
				Address:      "0xabc123",
				Name:         "USD Coin",
				Symbol:       "USDC",
				Decimals:     6,
				LiquidityCap: "1000000000000000000000000",
				TokenType:    types.TokenType_ERC20,
				NativeRepresentation: &types.NativeRepresentation{
					ContractAddress: "not_hex",
				},
			},
			expectErr: true,
			errMsg:    "contract_address must start with 0x",
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
