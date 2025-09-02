package integrationtest

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	utils "github.com/pushchain/push-chain-node/testutils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestMintPC(t *testing.T) {
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

	t.Run("Success", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		ethAddr := common.HexToAddress("0x8669BeD121FefA3d9CF2821273f489e717cca95d")
		cosmosAddr := sdk.AccAddress(ethAddr.Bytes())

		beforeMinting := app.BankKeeper.GetBalance(ctx, cosmosAddr, "upc")

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uexecutortypes.MsgMintPC{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.NoError(t, err)
		afterMining := app.BankKeeper.GetBalance(ctx, cosmosAddr, "upc")
		require.True(t, afterMining.Amount.GT(beforeMinting.Amount), "after balance should be greater than before balance")

	})

	t.Run("Tokens Already Minted!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		msg := &uexecutortypes.MsgMintPC{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.ErrorContains(t, err, "evm tx verification failed: tokens already minted for txHash 0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2 on chain eip155:11155111")

	})
	t.Run("Invalid Signer!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x59d9c4b86fb9cf62bc857bc9c0463f3bfd11ca6ec00b7e7021db1f660908bdbf"

		msg := &uexecutortypes.MsgMintPC{
			Signer:             "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.ErrorContains(t, err, "contract call failed")

	})

	t.Run("fail: Invalid TxHash", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}
		validTxHash := "0xabc123"
		msg := &uexecutortypes.MsgMintPC{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err := ms.MintPC(ctx, msg)
		require.ErrorContains(t, err, "rpc call failed after retries")
	})

}
