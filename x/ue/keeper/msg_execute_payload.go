package keeper

import (
	"context"
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/utils"
	"github.com/rollchains/pchain/x/ue/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) ExecutePayload(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, universalPayload *types.UniversalPayload, verificationData string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get Caip2Identifier for the universal account
	caip2Identifier := universalAccountId.GetCAIP2()

	chainConfig, err := k.uregistryKeeper.GetChainConfig(sdkCtx, caip2Identifier)
	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain %s", caip2Identifier)
	}

	if !chainConfig.Enabled.IsInboundEnabled {
		return fmt.Errorf("chain %s is not enabled", caip2Identifier)
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// Step 1: Compute smart account address
	receipt, err := k.CallFactoryToComputeUEAAddress(sdkCtx, evmFrom, factoryAddress, universalAccountId)
	if err != nil {
		return err
	}

	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	addressBytes := returnedBytesHex[24:] // last 20 bytes
	ueaComputedAddress := "0x" + addressBytes
	ueaAddr := common.HexToAddress(ueaComputedAddress)

	// // Step 2: Parse and validate payload and verificationData
	payload, err := types.NewAbiUniversalPayload(universalPayload)
	if err != nil {
		return errors.Wrapf(err, "invalid universal payload")
	}

	verificationDataVal, err := utils.HexToBytes(verificationData)
	if err != nil {
		return errors.Wrapf(err, "invalid verificationData format")
	}

	// Step 3: Execute payload through UEA
	receipt, err = k.CallUEAExecutePayload(sdkCtx, evmFrom, ueaAddr, universalPayload, verificationDataVal)
	if err != nil {
		return err
	}

	gasUnitsUsed := receipt.GasUsed
	gasUnitsUsedBig := new(big.Int).SetUint64(gasUnitsUsed)

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

	return nil
}
