package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) DeployUEA(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId) ([]byte, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// EVM Call arguments
	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// Use your keeper CallEVM directly
	receipt, err := k.CallFactoryToDeployUEA(
		sdkCtx,
		evmFrom,
		factoryAddress,
		universalAccountId,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("DeployUEA receipt:", receipt)
	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	fmt.Println("Returned Bytes Hex:", returnedBytesHex)

	return receipt.Ret, nil
}
