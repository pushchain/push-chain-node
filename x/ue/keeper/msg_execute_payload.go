package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/utils"
	"github.com/rollchains/pchain/x/ue/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) executePayload(ctx context.Context, evmFrom common.Address, accountId *types.AccountId, crosschainPayload *types.CrossChainPayload, signature string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Step 1: Get params and validate addresses
	adminParams, err := k.AdminParams.Get(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to get admin params")
	}

	chainConfig, err := k.GetChainConfig(sdkCtx, accountId.ChainId)
	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain %s", accountId.ChainId)
	}

	if !chainConfig.Enabled {
		return fmt.Errorf("chain %s is not enabled", accountId.ChainId)
	}

	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)

	// Step 2: Compute smart account address
	receipt, err := k.CallFactoryToComputeAddress(sdkCtx, evmFrom, factoryAddress, accountId)
	if err != nil {
		return err
	}

	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	addressBytes := returnedBytesHex[24:] // last 20 bytes
	nmscComputedAddress := "0x" + addressBytes
	nmscAddr := common.HexToAddress(nmscComputedAddress)

	// Step 3: Parse and validate payload and signature
	payload, err := types.NewAbiCrossChainPayload(crosschainPayload)
	if err != nil {
		return errors.Wrapf(err, "invalid cross-chain payload")
	}

	signatureVal, err := utils.HexToBytes(signature)
	if err != nil {
		return errors.Wrapf(err, "invalid signature format")
	}

	// Step 4: Execute payload through NMSC
	receipt, err = k.CallNMSCExecutePayload(sdkCtx, evmFrom, nmscAddr, payload, signatureVal)
	if err != nil {
		return err
	}

	// Step 5: Handle fee calculation and deduction
	nmscAccAddr := sdk.AccAddress(nmscAddr.Bytes())

	baseFee := k.feemarketKeeper.GetBaseFee(sdkCtx)
	if baseFee.IsNil() {
		return errors.Wrapf(sdkErrors.ErrLogic, "base fee not found")
	}

	gasCost, err := k.CalculateGasCost(baseFee, payload.MaxFeePerGas, payload.MaxPriorityFeePerGas, receipt.GasUsed)
	if err != nil {
		return errors.Wrapf(err, "failed to calculate gas cost")
	}

	if gasCost.Cmp(payload.GasLimit) > 0 {
		return errors.Wrapf(sdkErrors.ErrOutOfGas, "gas cost (%d) exceeds limit (%d)", gasCost, payload.GasLimit)
	}

	if err = k.DeductAndBurnFees(ctx, nmscAccAddr, gasCost); err != nil {
		return errors.Wrapf(err, "failed to deduct fees from %s", nmscAccAddr)
	}

	return nil
}
