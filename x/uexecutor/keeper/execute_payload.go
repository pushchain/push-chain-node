package keeper

import (
	"context"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	vmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ExecutePayloadV2 executes a universal payload through a UEA.
// The caller is responsible for resolving and validating ueaAddr before calling this function.
func (k Keeper) ExecutePayloadV2(ctx context.Context, evmFrom common.Address, ueaAddr common.Address, universalPayload *types.UniversalPayload, verificationData string) (*vmtypes.MsgEthereumTxResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	k.Logger().Debug("execute payload v2",
		"uea", ueaAddr.Hex(),
		"from", evmFrom.Hex(),
	)

	// Step 1: Parse and validate payload and verificationData
	payload, err := types.NewAbiUniversalPayload(universalPayload)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid universal payload")
	}

	verificationDataVal, err := utils.HexToBytes(verificationData)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid verificationData format")
	}

	// Step 2: Execute payload through UEA
	receipt, err := k.CallUEAExecutePayload(sdkCtx, evmFrom, ueaAddr, universalPayload, verificationDataVal)
	if err != nil {
		return nil, err
	}

	gasUnitsUsed := receipt.GasUsed
	gasUnitsUsedBig := new(big.Int).SetUint64(gasUnitsUsed)

	k.Logger().Debug("payload executed via UEA",
		"uea", ueaAddr.Hex(),
		"tx_hash", receipt.Hash,
		"gas_used", gasUnitsUsed,
	)

	// Step 3: Handle fee calculation and deduction
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

	k.Logger().Debug("fees deducted for payload execution",
		"uea", ueaAddr.Hex(),
		"gas_cost", gasCost.String(),
	)

	return receipt, nil
}
