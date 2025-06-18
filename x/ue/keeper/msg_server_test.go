package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/x/ue/types"
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

func TestMsgServer_DeployUEA(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	validSigner := f.addrs[0]
	validUA := &types.UniversalAccount{
		Owner: "0x1234567890abcdef1234567890abcdef12345678",
		Chain: "ethereum",
	}
	validTxHash := "0xabc123"

	t.Run("fail; invalid signer address", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:           "invalid_address",
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.ErrorContains(err, "failed to parse signer address")
	})

	t.Run("fail; gateway interaction tx not verified", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgDeployUEA{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           "invalid_tx",
		}

		_, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.Error(err)
	})

	t.Run("success; valid input returns UEA", func(t *testing.T) {
		msg := &types.MsgDeployUEA{
			Signer:           validSigner.String(),
			UniversalAccount: validUA,
			TxHash:           validTxHash,
		}

		res, err := f.msgServer.DeployUEA(f.ctx, msg)
		require.NoError(err)
		require.NotNil(res)
		require.NotEmpty(res.UEA)
	})
}
