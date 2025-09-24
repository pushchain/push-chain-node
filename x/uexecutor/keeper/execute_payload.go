package keeper

import (
	"context"
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	vmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) ExecutePayloadV2(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, universalPayload *types.UniversalPayload, verificationData string) (*vmtypes.MsgEthereumTxResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get Caip2Identifier for the universal account
	caip2Identifier := universalAccountId.GetCAIP2()

	chainConfig, err := k.uregistryKeeper.GetChainConfig(sdkCtx, caip2Identifier)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get chain config for chain %s", caip2Identifier)
	}

	if !chainConfig.Enabled.IsInboundEnabled {
		return nil, fmt.Errorf("chain %s is not enabled", caip2Identifier)
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// Step 1: Compute smart account address
	// Calling factory contract to compute the UEA address
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, evmFrom, factoryAddress, universalAccountId)
	if err != nil {
		return nil, err
	}

	if !isDeployed {
		return nil, fmt.Errorf("UEA is not deployed")
	}

	// // Step 2: Parse and validate payload and verificationData
	payload, err := types.NewAbiUniversalPayload(universalPayload)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid universal payload")
	}

	verificationDataVal, err := utils.HexToBytes(verificationData)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid verificationData format")
	}

	// Step 3: Execute payload through UEA
	receipt, err := k.CallUEAExecutePayload(sdkCtx, evmFrom, ueaAddr, universalPayload, verificationDataVal)
	if err != nil {
		return nil, err
	}

	gasUnitsUsed := receipt.GasUsed
	gasUnitsUsedBig := new(big.Int).SetUint64(gasUnitsUsed)

	// Step 4: Handle fee calculation and deduction
	ueaAccAddr := sdk.AccAddress(ueaAddr.Bytes())

	baseFee := k.feemarketKeeper.GetBaseFee(sdkCtx)
	if baseFee.IsNil() {
		return nil, errors.Wrapf(sdkErrors.ErrLogic, "base fee not found")
	}

	gasCost, err := k.CalculateGasCost(baseFee, payload.MaxFeePerGas, payload.MaxPriorityFeePerGas, gasUnitsUsed)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to calculate gas cost")
	}

	if gasUnitsUsedBig.Cmp(payload.GasLimit) > 0 {
		return nil, errors.Wrapf(sdkErrors.ErrOutOfGas, "gas cost (%d) exceeds limit (%d)", gasCost, payload.GasLimit)
	}

	if err = k.DeductAndBurnFees(ctx, ueaAccAddr, gasCost); err != nil {
		return nil, errors.Wrapf(err, "failed to deduct fees from %s", ueaAccAddr)
	}

	return receipt, nil
}
