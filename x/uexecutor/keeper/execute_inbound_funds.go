package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundFunds(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.Logger().Info("ExecuteInboundFunds: start")

	inbound := utx.InboundTx
	sdkCtx.Logger().Info(
		"ExecuteInboundFunds: inbound received",
		"utx_id", utx.Id,
		"source_chain", inbound.SourceChain,
		"asset_addr", inbound.AssetAddr,
		"recipient", inbound.Recipient,
		"amount", inbound.Amount,
		"sender", inbound.Sender,
		"tx_hash", inbound.TxHash,
	)

	sdkCtx.Logger().Info("ExecuteInboundFunds: calling depositPRC20")
	receipt, err := k.depositPRC20(
		sdkCtx,
		inbound.SourceChain,
		inbound.AssetAddr,
		common.HexToAddress(inbound.Recipient), // recipient is inbound recipient
		inbound.Amount,
	)
	if err != nil {
		sdkCtx.Logger().Error("ExecuteInboundFunds: depositPRC20 failed", "error", err.Error())
	} else {
		sdkCtx.Logger().Info(
			"ExecuteInboundFunds: depositPRC20 succeeded",
			"tx_hash", receipt.Hash,
			"gas_used", receipt.GasUsed,
		)
	}

	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*inbound)
	sdkCtx.Logger().Info(
		"ExecuteInboundFunds: prepared universal tx update",
		"universal_tx_key", universalTxKey,
		"ue_module_sender", ueModuleAddressStr,
	)
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		sdkCtx.Logger().Info("ExecuteInboundFunds: appending deposit PCTx")
		pcTx := types.PCTx{
			TxHash:      "", // no hash if depositPRC20 failed
			Sender:      ueModuleAddressStr,
			GasUsed:     0,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
		}

		if err != nil {
			pcTx.Status = "FAILED" // or "PENDING_REVERT"
			pcTx.ErrorMsg = err.Error()
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
			sdkCtx.Logger().Error("ExecuteInboundFunds: marking universal tx failed", "error", err.Error())
		} else {
			pcTx.TxHash = receipt.Hash
			pcTx.GasUsed = receipt.GasUsed
			pcTx.Status = "SUCCESS"
			pcTx.ErrorMsg = ""
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
			sdkCtx.Logger().Info(
				"ExecuteInboundFunds: marking universal tx success",
				"tx_hash", pcTx.TxHash,
				"gas_used", pcTx.GasUsed,
			)
		}

		utx.PcTx = append(utx.PcTx, &pcTx)
		sdkCtx.Logger().Info("ExecuteInboundFunds: deposit PCTx appended", "pc_tx_status", pcTx.Status)
		return nil
	})
	if updateErr != nil {
		sdkCtx.Logger().Error("ExecuteInboundFunds: failed to update universal tx", "error", updateErr.Error())
		return updateErr
	}
	sdkCtx.Logger().Info("ExecuteInboundFunds: universal tx updated")

	if err != nil {
		sdkCtx.Logger().Info("ExecuteInboundFunds: creating revert outbound")
		revertOutbound := types.OutboundTx{
			DestinationChain: inbound.SourceChain,
			Recipient: func() string {
				if inbound.RevertInstructions != nil {
					return inbound.RevertInstructions.FundRecipient
				}
				return inbound.Sender
			}(),
			Amount:            inbound.Amount,
			ExternalAssetAddr: inbound.AssetAddr,
			Sender:            inbound.Sender,
			TxType:            types.TxType_INBOUND_REVERT,
			OutboundStatus:    types.Status_PENDING,
			Id:                types.GetOutboundRevertId(inbound.TxHash),
		}
		sdkCtx.Logger().Info(
			"ExecuteInboundFunds: attaching revert outbound",
			"outbound_id", revertOutbound.Id,
			"destination_chain", revertOutbound.DestinationChain,
			"recipient", revertOutbound.Recipient,
		)
		_ = k.attachOutboundsToUtx(sdkCtx, utx.Id, []*types.OutboundTx{&revertOutbound}, err.Error())
		sdkCtx.Logger().Info("ExecuteInboundFunds: revert outbound attached")
	}

	sdkCtx.Logger().Info("ExecuteInboundFunds: done")
	return nil
}
