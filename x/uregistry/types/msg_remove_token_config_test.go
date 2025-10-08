package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestMsgRemoveTokenConfig_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "invalid_bech32"

	validMsg := &types.MsgRemoveTokenConfig{
		Signer:       validSigner,
		Chain:        "eip155:1",
		TokenAddress: "0x1234567890abcdef1234567890abcdef12345678",
	}

	invalidEmptyChain := &types.MsgRemoveTokenConfig{
		Signer:       validSigner,
		Chain:        "",
		TokenAddress: "0x1234567890abcdef1234567890abcdef12345678",
	}

	invalidChainFormat := &types.MsgRemoveTokenConfig{
		Signer:       validSigner,
		Chain:        "eip155", // missing ":" → invalid CAIP-2 format
		TokenAddress: "0x1234567890abcdef1234567890abcdef12345678",
	}

	invalidEmptyToken := &types.MsgRemoveTokenConfig{
		Signer:       validSigner,
		Chain:        "eip155:1",
		TokenAddress: "",
	}

	tests := []struct {
		name      string
		msg       *types.MsgRemoveTokenConfig
		expectErr bool
	}{
		{
			name:      "valid message",
			msg:       validMsg,
			expectErr: false,
		},
		{
			name: "invalid signer",
			msg: &types.MsgRemoveTokenConfig{
				Signer:       invalidSigner,
				Chain:        "eip155:1",
				TokenAddress: "0x1234567890abcdef1234567890abcdef12345678",
			},
			expectErr: true,
		},
		{
			name:      "empty chain",
			msg:       invalidEmptyChain,
			expectErr: true,
		},
		{
			name:      "invalid chain format",
			msg:       invalidChainFormat,
			expectErr: true,
		},
		{
			name:      "empty token address",
			msg:       invalidEmptyToken,
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
