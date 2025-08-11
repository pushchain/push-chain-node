package integrationtest

import (
	"testing"

	utils "github.com/pushchain/push-chain-node/testutils"
	uekeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uetypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestDeployUEA(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	chainConfigTest := uetypes.ChainConfig{
		Chain:             "eip155:11155111",
		VmType:            uetypes.VM_TYPE_EVM,
		PublicRpcUrl:      "https://1rpc.io/sepolia",
		GatewayAddress:    "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: 12,
		GatewayMethods: []*uetypes.MethodConfig{&uetypes.MethodConfig{
			Name:            "addFunds",
			Identifier:      "",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: true,
	}

	app.UexecutorKeeper.AddChainConfig(ctx, &chainConfigTest)
	ms := uekeeper.NewMsgServerImpl(app.UexecutorKeeper)

	t.Run("Success!", func(t *testing.T) {
		validUA := &uetypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uetypes.MsgDeployUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}
		resp, err := ms.DeployUEA(ctx, msg)
		require.NoError(t, err)
		expected := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 134, 105, 190, 209, 33, 254, 250, 61, 156, 242, 130, 18, 115, 244, 137, 231, 23, 204, 169, 93}
		require.Equal(t, expected, resp.UEA)

	})
	t.Run("Repeat transaction!", func(t *testing.T) {
		validUA := &uetypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uetypes.MsgDeployUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.DeployUEA(ctx, msg)
		require.ErrorContains(t, err, "contract call failed: method 'deployUEA', contract '0x00000000000000000000000000000000000000eA'")
	})
	t.Run("Repeat transaction!", func(t *testing.T) {
		validUA := &uetypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0xinvalidhash"

		msg := &uetypes.MsgDeployUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.DeployUEA(ctx, msg)
		require.ErrorContains(t, err, "failed to verify gateway interaction transaction")
	})

}
