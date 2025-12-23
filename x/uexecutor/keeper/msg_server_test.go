package keeper_test

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	ue "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
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

func TestMsgServer_ExecutePayload(t *testing.T) {
	f := SetupTest(t)

	validSigner := f.addrs[0]
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}
	validUP := &types.UniversalPayload{
		To:                   "0x1234567890abcdef1234567890abcdef12345670",
		Value:                "10",
		Data:                 "0x",
		GasLimit:             "1000000000000",
		MaxFeePerGas:         "10",
		MaxPriorityFeePerGas: "10",
		Nonce:                "1",
		Deadline:             "some-deadline",
	}
	invalidUP := &types.UniversalPayload{
		To:                   "0x1234567890abcdef1234567890abcdef12345670",
		Value:                "10",
		Data:                 "wrong-data",
		GasLimit:             "1000000000000",
		MaxFeePerGas:         "10",
		MaxPriorityFeePerGas: "10",
		Nonce:                "1",
		Deadline:             "some-deadline",
	}

	t.Run("fail; invalid signer address", func(t *testing.T) {
		msg := &types.MsgExecutePayload{
			Signer:             "invalid_address",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "0x",
		}

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "failed to parse signer address")
	})

	t.Run("Fail : ChainConfig for Universal Accout not set", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "0x",
		}

		f.mockUregistryKeeper.EXPECT().GetChainConfig(gomock.Any(), "eip155:11155111").Return(uregistrytypes.ChainConfig{}, errors.New("failed to get chain config for chain eip155:11155111"))

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "failed to get chain config")
	})

	t.Run("Fail: CallFactoryToComputeUEAAddress", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "0x",
		}

		chainConfigTest := uregistrytypes.ChainConfig{
			Chain:          "eip155:11155111",
			VmType:         uregistrytypes.VmType_EVM, // replace with appropriate VM_TYPE enum value
			PublicRpcUrl:   "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
			GatewayAddress: "0x1234567890abcdef1234567890abcdef12345678",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				FastInbound:     3,
				StandardInbound: 10,
			},
			GatewayMethods: []*uregistrytypes.GatewayMethods{},
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}

		f.mockUregistryKeeper.EXPECT().GetChainConfig(gomock.Any(), "eip155:11155111").Return(chainConfigTest, nil)

		f.mockEVMKeeper.EXPECT().CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("CallFactoryToComputeUEAAddress Failed"))

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "CallFactoryToComputeUEAAddress Failed")
	})

	t.Run("Fail : Invalid UniversalPayload", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   invalidUP,
			VerificationData:   "0x",
		}

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "invalid universal payload")
	})

	t.Run("Fail : Invalid Signature", func(t *testing.T) {
		avalidUP := &types.UniversalPayload{
			To:                   "0x8ba1f109551bD432803012645Ac136ddd64DBA72", // 20‑byte address
			Value:                "0",                                          // wei, decimal string
			Data:                 "0xdeadbeef",                                 // <- EVEN‑length hex → []byte{0xde, 0xad, 0xbe, 0xef}
			GasLimit:             "21000",                                      // decimal
			MaxFeePerGas:         "1000000000",                                 // 1 gwei
			MaxPriorityFeePerGas: "2000000000",                                 // 2 gwei
			Nonce:                "0",
			Deadline:             "0",
			VType:                ue.VerificationType_signedVerification,
		}
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgExecutePayload{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			UniversalPayload:   avalidUP,
			VerificationData:   "test-signature",
		}

		_, err := f.msgServer.ExecutePayload(f.ctx, msg)
		require.ErrorContains(t, err, "invalid verificationData format")
	})

}

func TestMsgServer_MigrateUEA(t *testing.T) {
	f := SetupTest(t)

	validSigner := f.addrs[0]
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}
	validMP := &types.MigrationPayload{
		Migration: "0x1234567890abcdef1234567890abcdef12345670",
		Nonce:     "1",
		Deadline:  "some-deadline",
	}

	t.Run("fail; invalid signer address", func(t *testing.T) {
		msg := &types.MsgMigrateUEA{
			Signer:             "invalid_address",
			UniversalAccountId: validUA,
			MigrationPayload:   validMP,
			Signature:          "0x",
		}

		_, err := f.msgServer.MigrateUEA(f.ctx, msg)
		require.ErrorContains(t, err, "failed to parse signer address")
	})

	t.Run("Fail : ChainConfig for Universal Accout not set", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgMigrateUEA{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			MigrationPayload:   validMP,
			Signature:          "0x",
		}

		f.mockUregistryKeeper.EXPECT().GetChainConfig(gomock.Any(), "eip155:11155111").Return(uregistrytypes.ChainConfig{}, errors.New("failed to get chain config for chain eip155:11155111"))

		_, err := f.msgServer.MigrateUEA(f.ctx, msg)
		require.ErrorContains(t, err, "failed to get chain config")
	})

	t.Run("Fail: CallFactoryToComputeUEAAddress", func(t *testing.T) {
		// You can inject failure in f.app or f.k.utvKeeper if mockable
		msg := &types.MsgMigrateUEA{
			Signer:             validSigner.String(),
			UniversalAccountId: validUA,
			MigrationPayload:   validMP,
			Signature:          "0x",
		}

		chainConfigTest := uregistrytypes.ChainConfig{
			Chain:          "eip155:11155111",
			VmType:         uregistrytypes.VmType_EVM, // replace with appropriate VM_TYPE enum value
			PublicRpcUrl:   "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
			GatewayAddress: "0x1234567890abcdef1234567890abcdef12345678",
			BlockConfirmation: &uregistrytypes.BlockConfirmation{
				FastInbound:     3,
				StandardInbound: 10,
			},
			GatewayMethods: []*uregistrytypes.GatewayMethods{},
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}

		f.mockUregistryKeeper.EXPECT().GetChainConfig(gomock.Any(), "eip155:11155111").Return(chainConfigTest, nil)

		f.mockEVMKeeper.EXPECT().CallEVM(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("CallFactoryToComputeUEAAddress Failed")).AnyTimes()

		_, err := f.msgServer.MigrateUEA(f.ctx, msg)
		require.ErrorContains(t, err, "CallFactoryToComputeUEAAddress Failed")
	})
}
