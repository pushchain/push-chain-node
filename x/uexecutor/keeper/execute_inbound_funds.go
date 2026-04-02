package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundFunds(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	inbound := utx.InboundTx

	k.Logger().Info("execute inbound funds: depositing PRC20",
		"utx_key", utx.Id,
		"source_chain", inbound.SourceChain,
		"recipient", inbound.Recipient,
		"amount", inbound.Amount,
		"is_cea", inbound.IsCEA,
	)

	receipt, err := k.depositPRC20(
		sdkCtx,
		inbound.SourceChain,
		inbound.AssetAddr,
		common.HexToAddress(inbound.Recipient), // recipient is inbound recipient
		inbound.Amount,
	)

	if err != nil {
		k.Logger().Warn("execute inbound funds: deposit failed",
			"utx_key", utx.Id,
			"source_chain", inbound.SourceChain,
			"error", err.Error(),
		)
	} else {
		k.Logger().Info("execute inbound funds: deposit succeeded",
			"utx_key", utx.Id,
			"tx_hash", receipt.Hash,
			"gas_used", receipt.GasUsed,
		)
	}

	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*inbound)
	if updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		pcTx := types.PCTx{
			Sender:      ueModuleAddressStr,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
		}

		// Capture tx hash from receipt even on EVM revert -- the reverted tx
		// still has a valid hash for debugging via eth_getTransactionByHash.
		if receipt != nil {
			pcTx.TxHash = receipt.Hash
			pcTx.GasUsed = receipt.GasUsed
		}

		if err != nil {
			pcTx.Status = "FAILED"
			pcTx.ErrorMsg = err.Error()
		} else {
			pcTx.Status = "SUCCESS"
		}

		utx.PcTx = append(utx.PcTx, &pcTx)
		return nil
	}); updateErr != nil {
		return updateErr
	}

	// isCEA failures never create an INBOUND_REVERT outbound
	// (consistent with execute_inbound_funds_and_payload.go and execute_inbound_gas_and_payload.go)
	if err != nil && !inbound.IsCEA {
		revertOutbound := k.buildRevertOutbound(sdkCtx, inbound)
		if attachErr := k.attachOutboundsToUtx(sdkCtx, utx.Id, []*types.OutboundTx{revertOutbound}, err.Error()); attachErr != nil {
			if storeErr := k.UpdateUniversalTx(sdkCtx, utx.Id, func(u *types.UniversalTx) error {
				u.RevertError = attachErr.Error()
				return nil
			}); storeErr != nil {
				return storeErr
			}
		}
	}

	return nil
}
