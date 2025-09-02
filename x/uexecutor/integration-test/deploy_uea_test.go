package integrationtest

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	utils "github.com/pushchain/push-chain-node/testutils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestDeployUEA(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://1rpc.io/sepolia",
		GatewayAddress: "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     5,
			StandardInbound: 12,
		},
		GatewayMethods: []*uregistrytypes.GatewayMethods{&uregistrytypes.GatewayMethods{
			Name:            "addFunds",
			Identifier:      "",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: &uregistrytypes.ChainEnabled{
			IsInboundEnabled:  true,
			IsOutboundEnabled: true,
		},
	}

	app.UregistryKeeper.AddChainConfig(ctx, &chainConfigTest)
	ms := uexecutorkeeper.NewMsgServerImpl(app.UexecutorKeeper)

	t.Run("Success!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uexecutortypes.MsgDeployUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}
		resp, err := ms.DeployUEA(ctx, msg)
		require.NoError(t, err)

		addr := common.HexToAddress("0x8669BeD121FefA3d9CF2821273f489e717cca95d").Bytes()

		var expected [32]byte
		copy(expected[32-len(addr):], addr)

		require.True(t, bytes.Equal(expected[:], resp.UEA), "address bytes do not match")

	})
	t.Run("Repeat transaction!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uexecutortypes.MsgDeployUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.DeployUEA(ctx, msg)
		require.ErrorContains(t, err, "contract call failed: method 'deployUEA', contract '0x00000000000000000000000000000000000000eA'")
	})
	t.Run("Repeat transaction!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0xinvalidhash"

		msg := &uexecutortypes.MsgDeployUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.DeployUEA(ctx, msg)
		require.ErrorContains(t, err, "failed to verify gateway interaction transaction")
	})

}
