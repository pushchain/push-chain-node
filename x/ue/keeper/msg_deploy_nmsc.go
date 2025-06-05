package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/x/ue/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) deployNMSC(ctx context.Context, evmFrom common.Address, accountId *types.AccountId, txHash string) ([]byte, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Retrieve the current Params
	adminParams, err := k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	// EVM Call arguments
	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)

	// RPC call verification to verify the locker interaction tx on source chain
	err = k.utvKeeper.VerifyLockerInteractionTx(ctx, accountId.OwnerKey, txHash, accountId.ChainId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to verify locker interaction transaction")
	}

	// Use your keeper CallEVM directly
	receipt, err := k.CallFactoryToDeployNMSC(
		sdkCtx,
		evmFrom,
		factoryAddress,
		accountId,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("DeployNMSC receipt:", receipt)
	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	fmt.Println("Returned Bytes Hex:", returnedBytesHex)

	return receipt.Ret, nil
}
