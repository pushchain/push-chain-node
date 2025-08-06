package integrationtest

import (
	// "math/big"
	"testing"

	uekeeper "github.com/rollchains/pchain/x/ue/keeper"
	uetypes "github.com/rollchains/pchain/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestMintPC(t *testing.T) {
	app, ctx, _ := SetAppWithValidators(t)
	chainConfigTest := uetypes.ChainConfig{
		Chain:             "eip155:11155111",
		VmType:            uetypes.VM_TYPE_EVM,
		PublicRpcUrl:      "https://1rpc.io/sepolia",
		GatewayAddress:    "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: 12,
		GatewayMethods: []*uetypes.MethodConfig{&uetypes.MethodConfig{
			Name:            "addFunds",
			Identifier:      "0xf9bfe8a7",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: true,
	}

	app.UeKeeper.AddChainConfig(ctx, &chainConfigTest)
	ms := uekeeper.NewMsgServerImpl(app.UeKeeper)

	t.Run("Success", func(t *testing.T) {
		validUA := &uetypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uetypes.MsgMintPC{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.NoError(t, err)

	})

	t.Run("Tokens Already Minted!", func(t *testing.T) {
		validUA := &uetypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uetypes.MsgMintPC{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.ErrorContains(t, err, "evm tx verification failed: tokens already minted for txHash 0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2 on chain eip155:11155111")

	})
	t.Run("Invalid Signer!", func(t *testing.T) {
		validUA := &uetypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x59d9c4b86fb9cf62bc857bc9c0463f3bfd11ca6ec00b7e7021db1f660908bdbf"

		msg := &uetypes.MsgMintPC{
			Signer:             "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.ErrorContains(t, err, "contract call failed: method 'computeUEA', contract '0x00000000000000000000000000000000000000eA'")

	})

	t.Run("fail: Invalid TxHash", func(t *testing.T) {
		validUA := &uetypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}
		validTxHash := "0xabc123"
		msg := &uetypes.MsgMintPC{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.ErrorContains(t, err, "rpc call failed after retries")
	})

}
