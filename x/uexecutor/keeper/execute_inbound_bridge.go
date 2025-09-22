package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundBridge(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	receipt, err := k.depositPRC20(
		sdkCtx,
		utx.InboundTx.SourceChain,
		utx.InboundTx.AssetAddr,
		common.HexToAddress(utx.InboundTx.Recipient), // recipient is inbound recipient
		utx.InboundTx.Amount,
	)
	if err != nil {
		// TODO: update status to PendingRevert and add revert mechanism here
		return err
	}

	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)

	universalTxKey := types.GetInboundKey(*utx.InboundTx)
	err = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		pcTx := types.PCTx{
			TxHash:      receipt.Hash, // TODO: handler revert params
			Sender:      ueModuleAddressStr,
			GasUsed:     receipt.GasUsed,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "SUCCESS", // TODO: handler revert params
			ErrorMsg:    "",        // TODO: handler revert params
		}

		utx.PcTx = append(utx.PcTx, &pcTx)
		utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
