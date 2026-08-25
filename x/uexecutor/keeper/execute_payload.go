package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	vmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// ExecutePayloadV2 executes a universal payload through a UEA and attaches the
// gateway outbounds it emitted to utx, atomically with the execution itself.
// The caller is responsible for resolving and validating ueaAddr before calling this function.
func (k Keeper) ExecutePayloadV2(ctx context.Context, evmFrom common.Address, ueaAddr common.Address, universalPayload *types.UniversalPayload, verificationData string, utx types.UniversalTx) (*vmtypes.MsgEthereumTxResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	k.Logger().Debug("execute payload v2",
		"uea", ueaAddr.Hex(),
		"from", evmFrom.Hex(),
	)

	// Step 1: Validate payload and verificationData early (fast-fail before EVM work)
	if _, err := types.NewAbiUniversalPayload(universalPayload); err != nil {
		return nil, errors.Wrapf(err, "invalid universal payload")
	}

	verificationDataVal, err := utils.HexToBytes(verificationData)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid verificationData format")
	}

	// Step 2: Wrap EVM execution + fee deduction + outbound attach in a
	// CacheContext so they commit/revert together. If fee deduction fails, the
	// EVM state changes from CallUEAExecutePayload are discarded — closes the
	// free-execution gap when the UEA has no native UPC to cover gas.
	cacheCtx, writeCache := sdkCtx.CacheContext()
	receipt, execErr := k.CallUEAExecutePayload(cacheCtx, evmFrom, ueaAddr, universalPayload, verificationDataVal)

	// Step 3: Try fee deduction in the same cache. DeductGasFeesFromReceipt
	// is a no-op if the receipt is nil or GasUsed == 0 (EVM call produced
	// nothing to bill).
	if feeErr := k.DeductGasFeesFromReceipt(cacheCtx, cacheCtx, ueaAddr, receipt, universalPayload); feeErr != nil {
		// Cache discarded — EVM state and any partial fee work both roll back.
		return receipt, fmt.Errorf("gas fee deduction failed: %w", feeErr)
	}

	if execErr != nil {
		// EVM execution failed — cache discarded by not calling writeCache.
		return receipt, execErr
	}

	// Step 4: Attach the outbounds the payload emitted, still inside the cache.
	// A payload that calls UniversalGatewayPC has already burned the PRC20 by
	// the time we get here, and BuildOutboundsFromReceipt is all-or-nothing:
	// one invalid leg of a multicall (unregistered PRC20, disabled chain)
	// discards every valid outbound alongside it. Attaching here means that
	// failure also discards the burn, instead of leaving burned supply with no
	// OutboundTx and no PendingOutbounds row to deliver against.
	if receipt != nil {
		if attachErr := k.AttachOutboundsToExistingUniversalTx(cacheCtx, receipt, utx); attachErr != nil {
			return receipt, fmt.Errorf("outbound attach failed: %w", attachErr)
		}
	}

	// All succeeded — commit EVM state, fee deduction and outbounds together.
	writeCache()

	k.Logger().Debug("payload executed via UEA",
		"uea", ueaAddr.Hex(),
		"tx_hash", receipt.Hash,
		"gas_used", receipt.GasUsed,
	)

	return receipt, nil
}
