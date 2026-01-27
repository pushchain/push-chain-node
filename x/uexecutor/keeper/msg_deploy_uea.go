package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) DeployUEA(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, txHash string) ([]byte, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// EVM Call arguments
	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// RPC call verification to verify the gateway interaction tx on source chain
	// err := k.utxverifierKeeper.VerifyGatewayInteractionTx(ctx, universalAccountId.Owner, txHash, universalAccountId.GetCAIP2())
	// if err != nil {
	// 	return nil, errors.Wrapf(err, "failed to verify gateway interaction transaction")
	// }

	uacc := types.UniversalAccountId{
		ChainNamespace: universalAccountId.ChainNamespace,
		ChainId:        universalAccountId.ChainId,
		Owner:          "0xa96CaA79eb2312DbEb0B8E93c1Ce84C98b67bF12",
	}

	// Use your keeper CallEVM directly
	receipt1, err := k.CallFactoryToDeployUEA(
		sdkCtx,
		evmFrom,
		factoryAddress,
		universalAccountId,
	)
	if err != nil {
		return nil, err
	}

	receipt2, err := k.CallFactoryToDeployUEA(
		sdkCtx,
		evmFrom,
		factoryAddress,
		&uacc,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println(receipt2)

	fmt.Println("DeployUEA receipt:", receipt1)
	returnedBytesHex := common.Bytes2Hex(receipt1.Ret)
	fmt.Println("Returned Bytes Hex:", returnedBytesHex)

	return receipt1.Ret, nil
}
