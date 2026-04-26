package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) DeployUEAV2(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId) (*evmtypes.MsgEthereumTxResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	k.Logger().Debug("deploying UEA",
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

	k.Logger().Info("UEA deployed",
		"chain_namespace", universalAccountId.ChainNamespace,
		"chain_id", universalAccountId.ChainId,
		"owner", universalAccountId.Owner,
		"tx_hash", receipt.Hash,
		"gas_used", receipt.GasUsed,
	)

	return receipt, nil
}
