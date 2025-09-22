package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uregistry/types"
)

func TestParams(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	testCases := []struct {
		name    string
		request *types.MsgUpdateParams
		err     bool
	}{
		{
			name: "fail; invalid authority",
			request: &types.MsgUpdateParams{
				Authority: f.addrs[0].String(),
				Params:    types.DefaultParams(),
			},
			err: true,
		},
		{
			name: "success",
			request: &types.MsgUpdateParams{
				Authority: f.govModAddr,
				Params:    types.DefaultParams(),
			},
			err: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.msgServer.UpdateParams(f.ctx, tc.request)

			if tc.err {
				require.Error(err)
			} else {
				require.NoError(err)

				r, err := f.queryServer.Params(f.ctx, &types.QueryParamsRequest{})
				require.NoError(err)

				require.EqualValues(&tc.request.Params, r.Params)
			}

		})
	}
}

func TestMsgServer_AddChainConfig(t *testing.T) {
	f := SetupTest(t)
	validSigner := f.addrs[0]

	chainConfigTest := types.ChainConfig{
		Chain:          "eip:11155111",
		VmType:         types.VmType_EVM, // replace with appropriate VM_TYPE enum value
		PublicRpcUrl:   "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
		GatewayAddress: "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: &types.BlockConfirmation{
			FastInbound:     3,
			StandardInbound: 6,
		},
		GatewayMethods: []*types.GatewayMethods{},
		Enabled: &types.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}
	t.Run("Failed to get params", func(t *testing.T) {
		msg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}

		_, err := f.msgServer.AddChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "failed to get params")
	})

	t.Run("fail : Invalid authority", func(t *testing.T) {
		msg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, types.Params{})
		_, err := f.msgServer.AddChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "invalid authority;")
	})

	t.Run("success!", func(t *testing.T) {
		msg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, types.Params{Admin: validSigner.String()})
		_, err := f.msgServer.AddChainConfig(f.ctx, msg)
		require.NoError(t, err) // flag : need to add verify condition
	})

}

func TestMsgServer_UpdateChainConfig(t *testing.T) {
	f := SetupTest(t)
	validSigner := f.addrs[0]

	chainConfigTest := types.ChainConfig{
		Chain:          "eip:11155111",
		VmType:         types.VmType_EVM, // replace with appropriate VM_TYPE enum value
		PublicRpcUrl:   "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
		GatewayAddress: "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: &types.BlockConfirmation{
			FastInbound:     3,
			StandardInbound: 6,
		},
		GatewayMethods: []*types.GatewayMethods{},
		Enabled: &types.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}

	updatedChainConfigTest := types.ChainConfig{
		Chain:          "eip:11155111",
		VmType:         types.VmType_EVM, // replace with appropriate VM_TYPE enum value
		PublicRpcUrl:   "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
		GatewayAddress: "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: &types.BlockConfirmation{
			FastInbound:     2,
			StandardInbound: 8,
		},
		GatewayMethods: []*types.GatewayMethods{},
		Enabled: &types.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}
	t.Run("Failed to get params", func(t *testing.T) {
		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}

		_, err := f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "failed to get params")
	})
	t.Run("fail : Invalid authority", func(t *testing.T) {
		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, types.Params{})
		_, err := f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "invalid authority;")
	})

	t.Run("fail : config does not exist to update", func(t *testing.T) {
		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, types.Params{Admin: validSigner.String()})
		_, err := f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.ErrorContains(t, err, "chain config for eip:11155111 does not exist")
	})

	t.Run("success!", func(t *testing.T) {
		addConfigMsg := &types.MsgAddChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &chainConfigTest,
		}
		f.k.Params.Set(f.ctx, types.Params{Admin: validSigner.String()})
		_, err := f.msgServer.AddChainConfig(f.ctx, addConfigMsg)
		require.NoError(t, err)

		msg := &types.MsgUpdateChainConfig{
			Signer:      validSigner.String(),
			ChainConfig: &updatedChainConfigTest,
		}
		_, err = f.msgServer.UpdateChainConfig(f.ctx, msg)
		require.NoError(t, err) // flag : need to add verify condition (cross-checking)
	})
}
