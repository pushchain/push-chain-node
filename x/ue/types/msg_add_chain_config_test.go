package types_test

import (
	"testing"

	"github.com/rollchains/pchain/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestMsgAddChainConfig_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "invalid_bech32"

	validChainConfig := types.ChainConfig{
		Chain:             "eip155:1",
		VmType:            types.VM_TYPE_EVM,
		PublicRpcUrl:      "https://mainnet.infura.io/v3/123",
		GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: 5,
		GatewayMethods: []*types.MethodConfig{
			{
				Name:            "mint",
				Identifier:      "aabbccdd",
				EventIdentifier: "eeff1122",
			},
		},
		Enabled: true,
	}

	invalidChainConfig := types.ChainConfig{
		Chain: "", // missing chain field
		GatewayMethods: []*types.MethodConfig{
			{
				Name:            "mint",
				Identifier:      "aabbccdd",
				EventIdentifier: "eeff1122",
			},
		},
	}

	tests := []struct {
		name      string
		msg       *types.MsgAddChainConfig
		expectErr bool
	}{
		{
			name: "valid message",
			msg: &types.MsgAddChainConfig{
				Signer:      validSigner,
				ChainConfig: &validChainConfig,
			},
			expectErr: false,
		},
		{
			name: "invalid signer address",
			msg: &types.MsgAddChainConfig{
				Signer:      invalidSigner,
				ChainConfig: &validChainConfig,
			},
			expectErr: true,
		},
		{
			name: "invalid chain config",
			msg: &types.MsgAddChainConfig{
				Signer:      validSigner,
				ChainConfig: &invalidChainConfig,
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
