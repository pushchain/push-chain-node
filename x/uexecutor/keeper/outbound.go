package keeper

import (
	"context"
	"fmt"

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
	if obs == nil {
		return nil
	}

	// If outbound succeeded -> nothing to do
	if obs.Success {
		return nil
	}

	// Only refund for funds-related tx types
	if outbound.TxType != types.TxType_FUNDS &&
		outbound.TxType != types.TxType_FUNDS_AND_PAYLOAD {
		return nil
	}

	// Parse amount and mint as per revert Instruction
	// TODO
	// Store Reverted tx in Outbound
	return nil
}
