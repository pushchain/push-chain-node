package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundFunds(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	receipt, err := k.depositPRC20(
		sdkCtx,
		utx.InboundTx.SourceChain,
		utx.InboundTx.AssetAddr,
		common.HexToAddress(utx.InboundTx.Recipient), // recipient is inbound recipient
		utx.InboundTx.Amount,
	)

	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*utx.InboundTx)
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

	return nil
}
