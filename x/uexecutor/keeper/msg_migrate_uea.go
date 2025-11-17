package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) MigrateUEA(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, migrationPayload *types.MigrationPayload, signature string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get Caip2Identifier for the universal account
	caip2Identifier := universalAccountId.GetCAIP2()

	// Step 1: Parse and validate payload and signature
	_, err := types.NewAbiMigrationPayload(migrationPayload)
	if err != nil {
		return errors.Wrapf(err, "invalid migration payload")
	}

	// add signature verification
	signatureVal, err := utils.HexToBytes(signature)
	if err != nil {
		return errors.Wrapf(err, "invalid signature format")
	}

	chainConfig, err := k.uregistryKeeper.GetChainConfig(sdkCtx, caip2Identifier)
	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain %s", caip2Identifier)
	}

	if !chainConfig.Enabled.IsInboundEnabled {
		return fmt.Errorf("chain %s is not enabled", caip2Identifier)
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// Step 2: Compute smart account address
	// Calling factory contract to compute the UEA address
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, evmFrom, factoryAddress, universalAccountId)
	if err != nil {
		return err
	}

	if !isDeployed {
		return fmt.Errorf("UEA is not deployed")
	}

	// Step 3: Migrate UEA through UEA
	receipt, err := k.CallUEAMigrateUEA(sdkCtx, evmFrom, ueaAddr, migrationPayload, signatureVal)
	if err != nil {
		return err
	}
	fmt.Println(receipt)

	// gasUnitsUsed := receipt.GasUsed
	// gasUnitsUsedBig := new(big.Int).SetUint64(gasUnitsUsed)

	// // Step 4: Handle fee calculation and deduction
	// ueaAccAddr := sdk.AccAddress(ueaAddr.Bytes())

	// baseFee := k.feemarketKeeper.GetBaseFee(sdkCtx)
	// if baseFee.IsNil() {
	// 	return errors.Wrapf(sdkErrors.ErrLogic, "base fee not found")
	// }

	// gasCost, err := k.CalculateGasCost(baseFee, payload.MaxFeePerGas, payload.MaxPriorityFeePerGas, gasUnitsUsed)
	// if err != nil {
	// 	return errors.Wrapf(err, "failed to calculate gas cost")
	// }

	// if gasUnitsUsedBig.Cmp(payload.GasLimit) > 0 {
	// 	return errors.Wrapf(sdkErrors.ErrOutOfGas, "gas cost (%d) exceeds limit (%d)", gasCost, payload.GasLimit)
	// }

	// if err = k.DeductAndBurnFees(ctx, ueaAccAddr, gasCost); err != nil {
	// 	return errors.Wrapf(err, "failed to deduct fees from %s", ueaAccAddr)
	// }

	return nil
}
