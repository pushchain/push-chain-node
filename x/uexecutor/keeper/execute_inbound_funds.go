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
			TxHash:      "", // no hash if depositPRC20 failed
			Sender:      ueModuleAddressStr,
			GasUsed:     0,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
		}

		if err != nil {
			pcTx.Status = "FAILED"
			pcTx.ErrorMsg = err.Error()
		} else if receipt != nil {
			pcTx.TxHash = receipt.Hash
			pcTx.GasUsed = receipt.GasUsed
			pcTx.Status = "SUCCESS"
			pcTx.ErrorMsg = ""
		}

		utx.PcTx = append(utx.PcTx, &pcTx)
		return nil
	}); updateErr != nil {
		return updateErr
	}

	// isCEA failures never create an INBOUND_REVERT outbound
	// (consistent with execute_inbound_funds_and_payload.go and execute_inbound_gas_and_payload.go)
	if err != nil && !inbound.IsCEA {
		revertOutbound := types.OutboundTx{
			DestinationChain: inbound.SourceChain,
			Recipient: func() string {
				if inbound.RevertInstructions != nil && inbound.RevertInstructions.FundRecipient != "" {
					return inbound.RevertInstructions.FundRecipient
				}
				return inbound.Sender
			}(),
			Amount:            inbound.Amount,
			ExternalAssetAddr: inbound.AssetAddr,
			Sender:            inbound.Sender,
			TxType:            types.TxType_INBOUND_REVERT,
			OutboundStatus:    types.Status_PENDING,
			Id:                types.GetOutboundRevertId(inbound.SourceChain, inbound.TxHash),
		}
		if attachErr := k.attachOutboundsToUtx(sdkCtx, utx.Id, []*types.OutboundTx{&revertOutbound}, err.Error()); attachErr != nil {
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
