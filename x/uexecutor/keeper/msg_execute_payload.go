package keeper

import (
	"context"
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) ExecutePayload(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, universalPayload *types.UniversalPayload, verificationData string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := k.Logger()

	logger.Info("ExecutePayload started", "evmFrom", evmFrom.Hex(), "universalAccountId", universalAccountId)

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)

	// Get Caip2Identifier for the universal account
	caip2Identifier := universalAccountId.GetCAIP2()

	// Step 1: Parse and validate payload and verificationData
	logger.Info("Step 1: Parsing and validating payload")
	payload, err := types.NewAbiUniversalPayload(universalPayload)
	if err != nil {
		return errors.Wrapf(err, "invalid universal payload")
	}

	verificationDataVal, err := utils.HexToBytes(verificationData)
	if err != nil {
		return errors.Wrapf(err, "invalid verificationData format")
	}

	chainConfig, err := k.uregistryKeeper.GetChainConfig(sdkCtx, caip2Identifier)
	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain %s", caip2Identifier)
	}

	if !chainConfig.Enabled.IsInboundEnabled {
		return fmt.Errorf("chain %s is not enabled", caip2Identifier)
	}

	logger.Info("Step 1 completed: Payload validation successful", "gasUsed", sdkCtx.GasMeter().GasConsumed())

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// Step 2: Compute smart account address
	logger.Info("Step 2: Computing smart account address")
	// Calling factory contract to compute the UEA address
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, evmFrom, factoryAddress, universalAccountId)
	if err != nil {
		return err
	}
	abiUniversalPayload, err := types.NewAbiUniversalPayload(universalPayload)
	if err != nil {
		return err
	}

	// Compute txHash for storing
	txHash := crypto.Keccak256Hash(append(payload.Data, []byte(verificationData)...)).Hex()

	payloadHashErr := k.StoreVerifiedPayloadHashForExecutePayload(sdkCtx, abiUniversalPayload, ueaAddr, ueModuleAddr, evmFrom.Hex(), caip2Identifier, txHash)
	if payloadHashErr != nil {
		return payloadHashErr
	}

	logger.Info("Step 2 completed: Smart account address computed", "ueaAddr", ueaAddr.Hex(), "isDeployed", isDeployed, "gasUsed", sdkCtx.GasMeter().GasConsumed())

	if !isDeployed {
		return fmt.Errorf("UEA is not deployed")
	}

	// Step 3: Execute payload through UEA
	logger.Info("Step 3: Executing payload through UEA")
	receipt, err := k.CallUEAExecutePayload(sdkCtx, evmFrom, ueaAddr, universalPayload, verificationDataVal)
	if err != nil {
		return err
	}

	gasUnitsUsed := receipt.GasUsed
	gasUnitsUsedBig := new(big.Int).SetUint64(gasUnitsUsed)

	logger.Info("Step 3 completed: Payload executed", "gasUsedInEVM", gasUnitsUsed, "gasConsumedTotal", sdkCtx.GasMeter().GasConsumed())

	// Step 4: Handle fee calculation and deduction
	ueaAccAddr := sdk.AccAddress(ueaAddr.Bytes())

	baseFee := k.feemarketKeeper.GetBaseFee(sdkCtx)
	if baseFee.IsNil() {
		return errors.Wrapf(sdkErrors.ErrLogic, "base fee not found")
	}

	gasCost, err := k.CalculateGasCost(baseFee, payload.MaxFeePerGas, payload.MaxPriorityFeePerGas, gasUnitsUsed)
	if err != nil {
		return errors.Wrapf(err, "failed to calculate gas cost")
	}

	if gasUnitsUsedBig.Cmp(payload.GasLimit) > 0 {
		return errors.Wrapf(sdkErrors.ErrOutOfGas, "gas cost (%d) exceeds limit (%d)", gasCost, payload.GasLimit)
	}

	if err = k.DeductAndBurnFees(ctx, ueaAccAddr, gasCost); err != nil {
		return errors.Wrapf(err, "failed to deduct fees from %s", ueaAccAddr)
	}

	logger.Info("Step 4 completed: Fees deducted successfully", "gasCost", gasCost.String(), "totalGasConsumed", sdkCtx.GasMeter().GasConsumed())
	logger.Info("ExecutePayload completed successfully", "ueaAddr", ueaAddr.Hex(), "totalGasConsumed", sdkCtx.GasMeter().GasConsumed())

	return nil
}
