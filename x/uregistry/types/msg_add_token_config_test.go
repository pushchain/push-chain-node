package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestMsgAddTokenConfig_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "bad_bech32"

	validTokenConfig := &types.TokenConfig{
		Chain:        "eip155:1",
		Address:      "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		Name:         "USD Coin",
		Symbol:       "USDC",
		Decimals:     6,
		Enabled:      true,
		LiquidityCap: "1000000000000000000000000",
		TokenType:    types.TokenType_ERC20,
		NativeRepresentation: &types.NativeRepresentation{
			Denom:           "uusdc",
			ContractAddress: "0x1234567890abcdef1234567890abcdef12345678",
		},
	}

	invalidTokenConfig := &types.TokenConfig{
		Chain:        "", // invalid
		Address:      "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		Name:         "USD Coin",
		Symbol:       "USDC",
		Decimals:     6,
		LiquidityCap: "1000000000000000000000000",
		TokenType:    types.TokenType_ERC20,
		NativeRepresentation: &types.NativeRepresentation{
			ContractAddress: "not0xhex",
		},
	}

	tests := []struct {
		name      string
		msg       *types.MsgAddTokenConfig
		expectErr bool
	}{
		{
			name: "valid message",
			msg: &types.MsgAddTokenConfig{
				Signer:      validSigner,
				TokenConfig: validTokenConfig,
			},
			expectErr: false,
		},
		{
			name: "invalid signer",
			msg: &types.MsgAddTokenConfig{
				Signer:      invalidSigner,
				TokenConfig: validTokenConfig,
			},
			expectErr: true,
		},
		{
			name: "invalid token config",
			msg: &types.MsgAddTokenConfig{
				Signer:      validSigner,
				TokenConfig: invalidTokenConfig,
			},
			expectErr: true,
		},
		{
			// F-2026-17024: nil nested config used to panic in ValidateBasic.
			name: "nil token config returns typed error, no panic",
			msg: &types.MsgAddTokenConfig{
				Signer:      validSigner,
				TokenConfig: nil,
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				err := tc.msg.ValidateBasic()
				if tc.expectErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		})
	}
}
