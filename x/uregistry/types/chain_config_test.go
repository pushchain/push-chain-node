package types_test

import (
	"testing"

	"github.com/rollchains/pchain/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestChainConfig_ValidateBasic(t *testing.T) {
	validMethod := &types.GatewayMethods{
		Name:             "add_funds",
		Identifier:       "84ed4c39500ab38a",
		EventIdentifier:  "7f1f6cffbb134644",
		ConfirmationType: types.ConfirmationType_CONFIRMATION_TYPE_STANDARD,
	}
	validBlockConfirmation := &types.BlockConfirmation{
		FastInbound:     3,
		StandardInbound: 10,
	}

	tests := []struct {
		name      string
		config    types.ChainConfig
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid config",
			config: types.ChainConfig{
				Chain:             "solana:devnet",
				VmType:            types.VmType_SVM,
				PublicRpcUrl:      "https://api.devnet.solana.com",
				GatewayAddress:    "3zrWaMknHTRQpZSxY4BvQxw9TStSXiHcmcp3NMPTFkke",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{validMethod},
				Enabled: &types.ChainEnabled{
					IsInboundEnabled:  true,
					IsOutboundEnabled: true,
				},
			},
			expectErr: false,
		},
		{
			name: "invalid - empty chain",
			config: types.ChainConfig{
				Chain:             "",
				VmType:            types.VmType_SVM,
				PublicRpcUrl:      "https://api.devnet.solana.com",
				GatewayAddress:    "addr",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{validMethod},
			},
			expectErr: true,
			errMsg:    "chain cannot be empty",
		},
		{
			name: "invalid - chain missing ':' (not CAIP-2)",
			config: types.ChainConfig{
				Chain:             "solana",
				VmType:            types.VmType_SVM,
				PublicRpcUrl:      "https://api.devnet.solana.com",
				GatewayAddress:    "addr",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{validMethod},
			},
			expectErr: true,
			errMsg:    "chain must be in CAIP-2 format",
		},
		{
			name: "invalid - empty public RPC URL",
			config: types.ChainConfig{
				Chain:             "solana:devnet",
				VmType:            types.VmType_SVM,
				PublicRpcUrl:      "",
				GatewayAddress:    "addr",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{validMethod},
			},
			expectErr: true,
			errMsg:    "public_rpc_url cannot be empty",
		},
		{
			name: "invalid - empty gateway address",
			config: types.ChainConfig{
				Chain:             "solana:devnet",
				VmType:            types.VmType_SVM,
				PublicRpcUrl:      "url",
				GatewayAddress:    "",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{validMethod},
			},
			expectErr: true,
			errMsg:    "gateway_address cannot be empty",
		},
		{
			name: "invalid - vm_type out of range",
			config: types.ChainConfig{
				Chain:             "solana:devnet",
				VmType:            -1,
				PublicRpcUrl:      "url",
				GatewayAddress:    "addr",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{validMethod},
			},
			expectErr: true,
			errMsg:    "invalid vm_type",
		},
		{
			name: "invalid - vm_type out of range",
			config: types.ChainConfig{
				Chain:             "solana:devnet",
				VmType:            types.VmType_OTHER_VM + 1,
				PublicRpcUrl:      "url",
				GatewayAddress:    "addr",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{validMethod},
			},
			expectErr: true,
			errMsg:    "invalid vm_type",
		},
		{
			name: "invalid - empty gateway methods",
			config: types.ChainConfig{
				Chain:             "solana:devnet",
				VmType:            types.VmType_SVM,
				PublicRpcUrl:      "url",
				GatewayAddress:    "addr",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods:    []*types.GatewayMethods{},
			},
			expectErr: true,
			errMsg:    "gateway_methods cannot be empty",
		},
		{
			name: "invalid - bad method inside gateway_methods",
			config: types.ChainConfig{
				Chain:             "solana:devnet",
				VmType:            types.VmType_SVM,
				PublicRpcUrl:      "url",
				GatewayAddress:    "addr",
				BlockConfirmation: validBlockConfirmation,
				GatewayMethods: []*types.GatewayMethods{
					{
						Name:            "bad_method",
						Identifier:      "zzznothex", // invalid
						EventIdentifier: "7f1f6cffbb134644",
					},
				},
			},
			expectErr: true,
			errMsg:    "invalid method in gateway_methods",
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
