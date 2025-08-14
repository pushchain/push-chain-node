package integrationtest

import (
	"testing"

	"cosmossdk.io/math"
	utils "github.com/pushchain/push-chain-node/testutils"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestExecutePayload(t *testing.T) {
	app, ctx, _ := utils.SetAppWithValidators(t)

	chainConfigTest := uexecutortypes.ChainConfig{
		Chain:             "eip155:11155111",
		VmType:            uexecutortypes.VM_TYPE_EVM,
		PublicRpcUrl:      "https://1rpc.io/sepolia",
		GatewayAddress:    "0x28E0F09bE2321c1420Dc60Ee146aACbD68B335Fe",
		BlockConfirmation: 12,
		GatewayMethods: []*uexecutortypes.MethodConfig{&uexecutortypes.MethodConfig{
			Name:            "addFunds",
			Identifier:      "",
			EventIdentifier: "0xb28f49668e7e76dc96d7aabe5b7f63fecfbd1c3574774c05e8204e749fd96fbd",
		}},
		Enabled: true,
	}

	app.UexecutorKeeper.AddChainConfig(ctx, &chainConfigTest)

	params := app.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = math.LegacyNewDec(1000000000)
	app.FeeMarketKeeper.SetParams(ctx, params)

	ms := uexecutorkeeper.NewMsgServerImpl(app.UexecutorKeeper)

	t.Run("Success!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		// validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		validUP := &uexecutortypes.UniversalPayload{
			To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
			Value:                "0",
			Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(0),
		}

		msg := &uexecutortypes.MsgExecutePayload{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "0x075bcd15",
		}

		_, err := ms.ExecutePayload(ctx, msg)
		require.NoError(t, err)

	})
	t.Run("Invalid Universal Payload!", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		// validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		validUP := &uexecutortypes.UniversalPayload{
			To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
			Value:                "0",
			Data:                 "0xZZZZ",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
		}

		msg := &uexecutortypes.MsgExecutePayload{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
		}

		_, err := ms.ExecutePayload(ctx, msg)
		require.ErrorContains(t, err, "invalid universal payload")

	})

	t.Run("Invalid Verification Data", func(t *testing.T) {
		validUA := &uexecutortypes.UniversalAccountId{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		}

		// validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

		validUP := &uexecutortypes.UniversalPayload{
			To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
			Value:                "0",
			Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
			GasLimit:             "21000000",
			MaxFeePerGas:         "1000000000",
			MaxPriorityFeePerGas: "200000000",
			Nonce:                "1",
			Deadline:             "9999999999",
			VType:                uexecutortypes.VerificationType(0),
		}

		msg := &uexecutortypes.MsgExecutePayload{
			Signer:             "cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9",
			UniversalAccountId: validUA,
			UniversalPayload:   validUP,
			VerificationData:   "0xZZZZ",
		}

		_, err := ms.ExecutePayload(ctx, msg)
		require.ErrorContains(t, err, "invalid verificationData format")

	})

}
