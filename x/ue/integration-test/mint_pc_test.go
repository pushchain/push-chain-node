package integrationtest

import (
	// "math/big"
	"testing"

	// sdk "github.com/cosmos/cosmos-sdk/types"
	// "github.com/ethereum/go-ethereum/common"
	// evmtypes "github.com/evmos/os/x/evm/types"
	// pchaintypes "github.com/rollchains/pchain/types"
	// "github.com/rollchains/pchain/utils"
	// uekeeper "github.com/rollchains/pchain/x/ue/keeper"
	"github.com/rollchains/pchain/utils"
	uetypes "github.com/rollchains/pchain/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestMintPC(t *testing.T) {
	app, ctx, _ := SetAppWithValidators(t)

	validUA := &uetypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
	}

	validTxHash := "0x770f8df204a925dbfc3d73c7d532c832bd5fe78ed813835b365320e65b105ec2"

	msg := &uetypes.MsgMintPC{
		Signer:             "0x778d3206374f8ac265728e18e3fe2ae6b93e4ce4",
		UniversalAccountId: validUA,
		TxHash:             validTxHash, // make a MintPC transaction on Sepolia and add it here
	}

	//addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// padded := common.LeftPadBytes(addr.Bytes(), 32)
	// receipt := &evmtypes.MsgEthereumTxResponse{
	// 	Ret: padded,
	// }

	// usdAmount := new(big.Int)
	// usdAmount.SetString("1000000000000000000", 10) // 10 USD, 18 decimals
	// decimals := uint32(18)
	// amountToMint := uekeeper.ConvertUsdToPCTokens(usdAmount, decimals)
	// expectedCoins := sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint))

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

	_, evmFromAddress, err := utils.GetAddressPair("cosmos1xpurwdecvsenyvpkxvmnge3cv93nyd34xuersef38pjnxen9xfsk2dnz8yek2drrv56qmn2ak9")
	require.NoError(t, err)

	app.UeKeeper.MintPC(ctx, evmFromAddress, msg.UniversalAccountId, validTxHash)

}
