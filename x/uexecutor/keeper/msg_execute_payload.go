package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) ExecutePayload(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, universalPayload *types.UniversalPayload, verificationData string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get Caip2Identifier for the universal account
	caip2Identifier := universalAccountId.GetCAIP2()

	k.Logger().Info("execute payload",
		"from", evmFrom.Hex(),
		"chain", caip2Identifier,
		"owner", universalAccountId.Owner,
	)

	// Step 1: Validate payload and verificationData early (fast-fail before EVM work)
	if _, err := types.NewAbiUniversalPayload(universalPayload); err != nil {
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
		k.Logger().Warn("execute payload rejected: chain inbound disabled", "chain", caip2Identifier)
		return fmt.Errorf("inbound is disabled for chain %s", caip2Identifier)
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// Step 2: Compute smart account address
	// Calling factory contract to compute the UEA address
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, evmFrom, factoryAddress, universalAccountId)
	if err != nil {
		return err
	}

	if !isDeployed {
		// only deploy if the UEA address has funds and not deployed yet
		ueaAccAddr := sdk.AccAddress(ueaAddr.Bytes())
		balance := k.bankKeeper.GetBalance(sdkCtx, ueaAccAddr, pchaintypes.BaseDenom)
		if balance.Amount.Sign() == 0 {
			k.Logger().Warn("execute payload rejected: UEA not deployed and has no balance",
				"chain", caip2Identifier,
				"owner", universalAccountId.Owner,
			)
			return fmt.Errorf("UEA is not deployed")
		}

		k.Logger().Info("auto-deploying UEA before execute (pre-funded address)",
			"uea", ueaAddr.Hex(),
			"balance", balance.Amount.String(),
			"chain", caip2Identifier,
			"owner", universalAccountId.Owner,
		)
		if _, err := k.DeployUEAV2(ctx, evmFrom, universalAccountId); err != nil {
			return errors.Wrapf(err, "failed to auto-deploy pre-funded UEA")
		}
	}

	k.Logger().Debug("executing payload via UEA",
		"uea", ueaAddr.Hex(),
		"chain", caip2Identifier,
		"from", evmFrom.Hex(),
	)

	// Step 3: Execute payload through UEA
	receipt, execErr := k.CallUEAExecutePayload(sdkCtx, evmFrom, ueaAddr, universalPayload, verificationDataVal)

	// Step 4: Deduct gas fees regardless of success/failure.
	// If deduction fails, return error so the entire Cosmos tx rolls back (including EVM state).
	if feeErr := k.DeductGasFeesFromReceipt(ctx, sdkCtx, ueaAddr, receipt, universalPayload); feeErr != nil {
		return fmt.Errorf("gas fee deduction failed: %w", feeErr)
	}

	if execErr != nil {
		return execErr
	}

	k.Logger().Info("payload executed via direct msg",
		"chain", caip2Identifier,
		"uea", ueaAddr.Hex(),
		"tx_hash", receipt.Hash,
		"gas_used", receipt.GasUsed,
	)

	// Step 5
	pcTx := types.PCTx{
		Sender:      evmFrom.Hex(),
		TxHash:      receipt.Hash,
		GasUsed:     receipt.GasUsed,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "SUCCESS",
	}

	// Step 6: create outbound + UTX only if needed
	if err := k.CreateUniversalTxFromReceiptIfOutbound(sdkCtx, receipt, pcTx); err != nil {
		return err
	}
	if err := k.AttachRescueOutboundFromReceipt(sdkCtx, receipt, pcTx); err != nil {
		return err
	}

	return nil
}
