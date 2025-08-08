package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
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
	err := k.utxverifierKeeper.VerifyGatewayInteractionTx(ctx, universalAccountId.Owner, txHash, universalAccountId.GetCAIP2())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to verify gateway interaction transaction")
	}

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
