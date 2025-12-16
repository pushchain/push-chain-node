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

	receipt, err := k.depositPRC20(
		sdkCtx,
		inbound.SourceChain,
		inbound.AssetAddr,
		common.HexToAddress(inbound.Recipient), // recipient is inbound recipient
		inbound.Amount,
	)

	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*inbound)
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
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
		} else {
			pcTx.TxHash = receipt.Hash
			pcTx.GasUsed = receipt.GasUsed
			pcTx.Status = "SUCCESS"
			pcTx.ErrorMsg = ""
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		}

		utx.PcTx = append(utx.PcTx, &pcTx)
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	if err != nil {
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
			Id:                types.GetOutboundRevertId(),
		}
		_ = k.attachOutboundsToUtx(sdkCtx, utx.Id, []*types.OutboundTx{&revertOutbound}, err.Error())
	}

	return nil
}
