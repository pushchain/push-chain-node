package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) DeployUEA(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId) ([]byte, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	k.Logger().Info("deploy UEA via msg",
		"chain_namespace", universalAccountId.ChainNamespace,
		"chain_id", universalAccountId.ChainId,
		"owner", universalAccountId.Owner,
		"from", evmFrom.Hex(),
	)

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

	k.Logger().Info("UEA deployed via msg",
		"chain_namespace", universalAccountId.ChainNamespace,
		"chain_id", universalAccountId.ChainId,
		"owner", universalAccountId.Owner,
		"tx_hash", receipt.Hash,
		"gas_used", receipt.GasUsed,
	)

	return receipt.Ret, nil
}
