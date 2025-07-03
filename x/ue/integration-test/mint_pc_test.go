package integrationtest

import (
	// "math/big"
	"testing"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
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
	app, ctx := SetAppWithValidators(t)

	// create addr
	acc := simtestutil.CreateIncrementalAccounts(3)
	validSigner := acc[0]

	validUA := &uetypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	validTxHash := "0xabc123"

	msg := &uetypes.MsgMintPC{
		Signer:             validSigner.String(),
		UniversalAccountId: validUA,
		TxHash:             validTxHash,
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
		PublicRpcUrl:      "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
		GatewayAddress:    "0x1234567890abcdef1234567890abcdef12345678",
		BlockConfirmation: 12,
		GatewayMethods:    []*uetypes.MethodConfig{},
		Enabled:           true,
	}

	app.UeKeeper.AddChainConfig(ctx, &chainConfigTest)

	_, evmFromAddress, err := utils.GetAddressPair(msg.Signer)
	require.NoError(t, err)

	app.UeKeeper.MintPC(ctx, evmFromAddress, msg.UniversalAccountId, validTxHash)

}
