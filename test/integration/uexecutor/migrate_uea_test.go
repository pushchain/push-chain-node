package integrationtest

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/common"
	utils "github.com/pushchain/push-chain-node/test/utils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestMigrateUEA(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	chainConfigTest := uregistrytypes.ChainConfig{
		Chain:          "eip155:11155111",
		VmType:         uregistrytypes.VmType_EVM,
		PublicRpcUrl:   "https://sepolia.drpc.org",
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

	params := app.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = math.LegacyNewDec(1000000000)
	app.FeeMarketKeeper.SetParams(ctx, params)

	ms := uexecutorkeeper.NewMsgServerImpl(app.UexecutorKeeper)

	t.Run("Success!", func(t *testing.T) {
		migratedAddress, newEVMImplAddr := utils.DeployMigrationContract(t, app, ctx)

		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		validMP := &uexecutortypes.MigrationPayload{
			Migration: migratedAddress.Hex(),
			Nonce:     "0",
			Deadline:  "0",
		}

		msgDeploy := &uexecutortypes.MsgDeployUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		deployUEAResponse, err := ms.DeployUEA(ctx, msgDeploy)
		require.NoError(t, err)

		msgMint := &uexecutortypes.MsgMintPC{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			TxHash:             validTxHash,
		}

		_, err = ms.MintPC(ctx, msgMint)
		require.NoError(t, err)

		msg := &uexecutortypes.MsgMigrateUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			MigrationPayload:   validMP,
			Signature:          "0xd1d343e944b77d71542ff15b545244464d0a9bb5606a69ca97d123abc52ec84a54cd46e50cf771fcdf40df0cdb047c50c1dcc17f6482d5def3895ad94e0b1cad1c",
		}

		_, err = ms.MigrateUEA(ctx, msg)
		require.NoError(t, err)

		logicAfterMigration := app.EVMKeeper.GetState(ctx, common.BytesToAddress(deployUEAResponse.UEA[12:]), common.HexToHash("0x868a771a75a4aa6c2be13e9a9617cb8ea240ed84a3a90c8469537393ec3e115d"))
		require.Equal(t, newEVMImplAddr, common.BytesToAddress(logicAfterMigration.Bytes()))

	})
	t.Run("Invalid Migration Payload!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		// validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		validMP := &uexecutortypes.MigrationPayload{
			Migration: "",
			Nonce:     "1",
			Deadline:  "9999999999",
		}

		msg := &uexecutortypes.MsgMigrateUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			MigrationPayload:   validMP,
		}

		_, err := ms.MigrateUEA(ctx, msg)
		require.ErrorContains(t, err, "invalid migration payload")

	})

	t.Run("Invalid Signature Data", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		// validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		validMP := &uexecutortypes.MigrationPayload{
			Migration: "0x527F3692F5C53CfA83F7689885995606F93b6164",
			Nonce:     "1",
			Deadline:  "9999999999",
		}

		msg := &uexecutortypes.MsgMigrateUEA{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			MigrationPayload:   validMP,
			Signature:          "0xZZZZ",
		}

		_, err := ms.MigrateUEA(ctx, msg)
		require.ErrorContains(t, err, "invalid signature format")

	})
}
