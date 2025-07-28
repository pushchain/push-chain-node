package types_test

import (
	"testing"

	"github.com/rollchains/pchain/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestMsgUpdateTokenConfig_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "bad_bech32"

	validTokenConfig := &types.TokenConfig{
		Chain:        "eip155:1",
		Address:      "0xabc123",
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
		Address:      "0xabc123",
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
		msg       *types.MsgUpdateTokenConfig
		expectErr bool
	}{
		{
			name: "valid message",
			msg: &types.MsgUpdateTokenConfig{
				Signer:      validSigner,
				TokenConfig: validTokenConfig,
			},
			expectErr: false,
		},
		{
			name: "invalid signer",
			msg: &types.MsgUpdateTokenConfig{
				Signer:      invalidSigner,
				TokenConfig: validTokenConfig,
			},
			expectErr: true,
		},
		{
			name: "invalid token config",
			msg: &types.MsgUpdateTokenConfig{
				Signer:      validSigner,
				TokenConfig: invalidTokenConfig,
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
