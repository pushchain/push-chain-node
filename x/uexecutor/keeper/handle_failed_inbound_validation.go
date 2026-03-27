package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// handleFailedInboundValidation records a failed PCTx on the UTX and, for non-isCEA
// inbounds, schedules an INBOUND_REVERT outbound so the user's funds can be returned
// on the source chain. This is called when ValidateForExecution fails after the ballot
// has already been finalized and the UTX created.
func (k Keeper) handleFailedInboundValidation(sdkCtx sdk.Context, utx types.UniversalTx, validationErr error) error {
	inbound := utx.InboundTx
	_, ueModuleAddressStr := k.GetUeModuleAddress(sdkCtx)
	universalTxKey := utx.Id

	k.Logger().Warn("inbound validation failed",
		"utx_key", universalTxKey,
		"source_chain", inbound.SourceChain,
		"is_cea", inbound.IsCEA,
		"error", validationErr.Error(),
	)

	// Record the failed PCTx
	failedPcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED",
		ErrorMsg:    validationErr.Error(),
	}

	if err := k.UpdateUniversalTx(sdkCtx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &failedPcTx)
		return nil
	}); err != nil {
		return err
	}

	// For non-isCEA inbounds, schedule a revert outbound to return funds on source chain.
	// isCEA failures never create an INBOUND_REVERT outbound (consistent with execute_inbound_funds_and_payload.go).
	if !inbound.IsCEA {
		k.Logger().Info("scheduling inbound revert outbound",
			"utx_key", universalTxKey,
			"source_chain", inbound.SourceChain,
			"amount", inbound.Amount,
		)
		revertRecipient := inbound.Sender
		if inbound.RevertInstructions != nil && inbound.RevertInstructions.FundRecipient != "" {
			revertRecipient = inbound.RevertInstructions.FundRecipient
		}

		revertOutbound := &types.OutboundTx{
			DestinationChain:  inbound.SourceChain,
			Recipient:         revertRecipient,
			Amount:            inbound.Amount,
			ExternalAssetAddr: inbound.AssetAddr,
			Sender:            inbound.Sender,
			TxType:            types.TxType_INBOUND_REVERT,
			OutboundStatus:    types.Status_PENDING,
			Id:                types.GetOutboundRevertId(inbound.SourceChain, inbound.TxHash),
		}

		if attachErr := k.attachOutboundsToUtx(
			sdkCtx,
			universalTxKey,
			[]*types.OutboundTx{revertOutbound},
			validationErr.Error(),
		); attachErr != nil {
			// Store the revert failure reason on the UTX so it's queryable on-chain.
			// The FAILED PCTx is already recorded above — this adds why the revert wasn't attached.
			if storeErr := k.UpdateUniversalTx(sdkCtx, universalTxKey, func(utx *types.UniversalTx) error {
				utx.RevertError = attachErr.Error()
				return nil
			}); storeErr != nil {
				// UpdateUniversalTx only fails on infra issues — return to roll back and retry
				return storeErr
			}
		}
	}

	return nil
}
