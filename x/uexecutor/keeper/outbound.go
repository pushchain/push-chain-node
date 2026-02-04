package keeper

import (
	"context"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) UpdateOutbound(ctx context.Context, utxId string, outbound types.OutboundTx) error {
	return k.UpdateUniversalTx(ctx, utxId, func(utx *types.UniversalTx) error {
		if utx.OutboundTx == nil {
			return fmt.Errorf("outbound tx list is not initialized for utx %s", utxId)
		}

		updated := false
		for i, ob := range utx.OutboundTx {
			if ob.Id == outbound.Id {
				utx.OutboundTx[i] = &outbound
				updated = true
				break
			}
		}

		if !updated {
			return fmt.Errorf(
				"outbound with id %s not found in utx %s",
				outbound.Id,
				utxId,
			)
		}

		return nil
	})
}

func (k Keeper) FinalizeOutbound(ctx context.Context, utxId string, outbound types.OutboundTx) error {
	// If not observed yet, do nothing
	if outbound.OutboundStatus != types.Status_OBSERVED {
		return nil
	}

	obs := outbound.ObservedTx
	if obs == nil || obs.Success {
		return nil
	}

	// Only refund for funds-related tx types
	if outbound.TxType != types.TxType_FUNDS && outbound.TxType != types.TxType_GAS_AND_PAYLOAD &&
		outbound.TxType != types.TxType_FUNDS_AND_PAYLOAD {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Decide refund recipient safely
	recipient := outbound.Sender
	if outbound.RevertInstructions != nil &&
		outbound.RevertInstructions.FundRecipient != "" {
		recipient = outbound.RevertInstructions.FundRecipient
	}

	// Mint tokens back
	amount := new(big.Int)
	amount, ok := amount.SetString(outbound.Amount, 10)
	if !ok {
		return fmt.Errorf("invalid amount: %s", outbound.Amount)
	}
	receipt, err := k.CallPRC20Deposit(sdkCtx, common.HexToAddress(outbound.Prc20AssetAddr), common.HexToAddress(recipient), amount)

	// Update outbound status
	outbound.OutboundStatus = types.Status_REVERTED

	pcTx := types.PCTx{
		TxHash:      "", // no hash if depositPRC20 failed
		Sender:      outbound.Sender,
		GasUsed:     0,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
	}

	if err != nil {
		pcTx.Status = "FAILED"
		pcTx.ErrorMsg = err.Error()
	} else {
		pcTx.TxHash = receipt.Hash
		pcTx.GasUsed = receipt.GasUsed
		pcTx.Status = "SUCCESS"
		pcTx.ErrorMsg = ""
	}

	outbound.PcRevertExecution = &pcTx

	// Store Reverted tx in Outbound
	return k.UpdateOutbound(ctx, utxId, outbound)
}
