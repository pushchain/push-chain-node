package integrationtest

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/rollchains/pchain/utils"
	uetypes "github.com/rollchains/pchain/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestExecutePayload(t *testing.T) {
	app, ctx, _ := SetAppWithValidators(t)
	validUA := &uetypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
	}

	validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

	msg := &uetypes.MsgDeployUEA{
		Signer:             "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		UniversalAccountId: validUA,
		TxHash:             validTxHash, // make a Deploy UEA transaction on Sepolia and add it here
	}

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

	app.UeKeeper.AddChainConfig(ctx, &chainConfigTest)

	validUP := &uetypes.UniversalPayload{
		To:                   "0x527F3692F5C53CfA83F7689885995606F93b6164",
		Value:                "0",
		Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
		GasLimit:             "21000000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "1",
		Deadline:             "9999999999",
		VType:                uetypes.VerificationType(0),
	}

	params := app.FeeMarketKeeper.GetParams(ctx)
	params.BaseFee = math.LegacyNewDec(1000000000)
	app.FeeMarketKeeper.SetParams(ctx, params)

	_, evmFromAddress, err := utils.GetAddressPair("cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9")
	require.NoError(t, err)
	app.UeKeeper.ExecutePayload(ctx, evmFromAddress, msg.UniversalAccountId, validUP, "0x075bcd15")

}
