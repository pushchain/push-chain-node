package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

func (k Keeper) BuildOutboundsFromReceipt(
	ctx context.Context,
	utxId string,
	receipt *evmtypes.MsgEthereumTxResponse,
) ([]*types.OutboundTx, error) {

	outbounds := []*types.OutboundTx{}
	universalGatewayPC := strings.ToLower(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_GATEWAY_PC"].Address)

	for _, lg := range receipt.Logs {
		if lg.Removed {
			continue
		}

		if strings.ToLower(lg.Address) != universalGatewayPC {
			continue
		}

		if len(lg.Topics) == 0 {
			continue
		}

		if strings.ToLower(lg.Topics[0]) != strings.ToLower(types.UniversalTxOutboundEventSig) {
			continue
		}

		event, err := types.DecodeUniversalTxOutboundFromLog(lg)
		if err != nil {
			return nil, fmt.Errorf("failed to decode UniversalTxWithdraw: %w", err)
		}

		// Check if outbound is enabled for the destination chain
		outboundEnabled, err := k.uregistryKeeper.IsChainOutboundEnabled(ctx, event.ChainId)
		if err != nil {
			return nil, fmt.Errorf("failed to check outbound enabled for chain %s: %w", event.ChainId, err)
		}
		if !outboundEnabled {
			return nil, fmt.Errorf("outbound is disabled for chain %s", event.ChainId)
		}

		// Get the external asset addr
		tokenCfg, err := k.uregistryKeeper.GetTokenConfigByPRC20(
			ctx,
			event.ChainId,
			event.Token, // PRC20 address
		)
		if err != nil {
			return nil, err
		}

		outbound := &types.OutboundTx{
			DestinationChain:  event.ChainId,
			Recipient:         event.Target,
			Amount:            event.Amount.String(),
			ExternalAssetAddr: tokenCfg.Address,
			Prc20AssetAddr:    event.Token,
			Sender:            event.Sender,
			Payload:           event.Payload,
			GasFee:            event.GasFee.String(),
			GasLimit:          event.GasLimit.String(),
			GasPrice:          event.GasPrice.String(),
			GasToken:          event.GasToken,
			TxType:            event.TxType,
			PcTx: &types.OriginatingPcTx{
				TxHash:   receipt.Hash,
				LogIndex: fmt.Sprintf("%d", lg.Index),
			},
			RevertInstructions: &types.RevertInstructions{
				FundRecipient: event.RevertRecipient,
			},
			OutboundStatus: types.Status_PENDING,
			Id:             strings.TrimPrefix(event.TxID, "0x"),
		}

		outbounds = append(outbounds, outbound)
	}

	return outbounds, nil
}

func (k Keeper) CreateUniversalTxFromPCTx(
	ctx context.Context,
	pcTx types.PCTx,
) (*types.UniversalTx, error) {

	universalTxKey, err := k.BuildPcUniversalTxKey(ctx, pcTx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create UniversalTx key")
	}

	found, err := k.HasUniversalTx(ctx, universalTxKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check UniversalTx")
	}
	if found {
		return nil, fmt.Errorf("universal tx already exists for pc tx %s", pcTx.TxHash)
	}

	utx := types.UniversalTx{
		Id:              universalTxKey,
		InboundTx:       nil,                  // no inbound
		PcTx:            []*types.PCTx{&pcTx}, // origin is PC
		OutboundTx:      nil,
	}

	if err := k.CreateUniversalTx(ctx, universalTxKey, utx); err != nil {
		return nil, err
	}

	return &utx, nil
}

// AttachOutboundsToExistingUniversalTx
// Used when UniversalTx already exists (e.g. inbound execution)
// It attaches outbounds extracted from receipt to the existing utx.
func (k Keeper) AttachOutboundsToExistingUniversalTx(
	ctx sdk.Context,
	receipt *evmtypes.MsgEthereumTxResponse,
	utx types.UniversalTx,
) error {
	outbounds, err := k.BuildOutboundsFromReceipt(ctx, utx.Id, receipt)
	if err != nil {
		return err
	}

	return k.attachOutboundsToUtx(ctx, utx.Id, outbounds, "")
}

// CreateUniversalTxFromReceiptIfOutbound
// Creates a UniversalTx ONLY if outbound events exist in the receipt.
// Safe to call from ExecutePayload, EVM hooks
func (k Keeper) CreateUniversalTxFromReceiptIfOutbound(
	ctx sdk.Context,
	receipt *evmtypes.MsgEthereumTxResponse,
	pcTx types.PCTx,
) error {
	universalTxKey, err := k.BuildPcUniversalTxKey(ctx, pcTx)
	if err != nil {
		return errors.Wrap(err, "failed to create UniversalTx key")
	}

	outbounds, err := k.BuildOutboundsFromReceipt(ctx, universalTxKey, receipt)
	if err != nil {
		return err
	}

	if len(outbounds) == 0 {
		return nil
	}

	utx, err := k.CreateUniversalTxFromPCTx(ctx, pcTx)
	if err != nil {
		return err
	}

	return k.attachOutboundsToUtx(ctx, utx.Id, outbounds, "")
}

// AttachRescueOutboundFromReceipt scans the receipt for RescueFundsOnSourceChain events
// emitted by UniversalGatewayPC and, for each one found, attaches a RESCUE_FUNDS outbound
// to the original UTX referenced by the event's universalTxId.
//
// Unlike normal outbounds (which create a new UTX), rescue outbounds are appended to the
// already-existing UTX whose funds are stuck on the source chain.
func (k Keeper) AttachRescueOutboundFromReceipt(
	ctx sdk.Context,
	receipt *evmtypes.MsgEthereumTxResponse,
	pcTx types.PCTx,
) error {
	universalGatewayPC := strings.ToLower(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_GATEWAY_PC"].Address)

	evmChainID, err := utils.ExtractEvmChainID(ctx.ChainID())
	if err != nil {
		return fmt.Errorf("rescue: failed to extract EVM chain ID: %w", err)
	}
	pushChainCaip := fmt.Sprintf("eip155:%s", evmChainID)

	for _, lg := range receipt.Logs {
		if lg.Removed {
			continue
		}
		if strings.ToLower(lg.Address) != universalGatewayPC {
			continue
		}
		if len(lg.Topics) == 0 {
			continue
		}
		if strings.ToLower(lg.Topics[0]) != strings.ToLower(types.RescueFundsOnSourceChainEventSig) {
			continue
		}

		event, err := types.DecodeRescueFundsOnSourceChainFromLog(lg)
		if err != nil {
			return fmt.Errorf("failed to decode RescueFundsOnSourceChain: %w", err)
		}

		// The universalTxId in the event is a 0x-prefixed bytes32 matching our UTX key.
		originalUtxId := strings.TrimPrefix(event.UniversalTxId, "0x")

		originalUtx, found, err := k.GetUniversalTx(ctx, originalUtxId)
		if err != nil {
			return fmt.Errorf("rescue: failed to fetch UTX %s: %w", originalUtxId, err)
		}
		if !found {
			return fmt.Errorf("rescue: original UTX %s not found", originalUtxId)
		}
		if originalUtx.InboundTx == nil {
			return fmt.Errorf("rescue: UTX %s has no inbound tx", originalUtxId)
		}

		// Rescue eligibility differs by inbound type:
		//
		//  CEA inbounds: the deposit (first PCTx) must have failed, meaning the funds
		//  never arrived on Push Chain and are still locked on the source chain.
		//
		//  Non-CEA inbounds: the auto-generated INBOUND_REVERT outbound must exist and
		//  have reached REVERTED status, meaning TSS could not return the funds to the
		//  source chain and they are stuck (held by the gateway contract or in escrow).
		if originalUtx.InboundTx.IsCEA {
			if len(originalUtx.PcTx) == 0 || originalUtx.PcTx[0] == nil || originalUtx.PcTx[0].Status != "FAILED" {
				return fmt.Errorf("rescue: UTX %s CEA deposit did not fail", originalUtxId)
			}
		} else {
			hasRevertedAutoRevert := false
			for _, ob := range originalUtx.OutboundTx {
				if ob != nil && ob.TxType == types.TxType_INBOUND_REVERT && ob.OutboundStatus == types.Status_REVERTED {
					hasRevertedAutoRevert = true
					break
				}
			}
			if !hasRevertedAutoRevert {
				return fmt.Errorf("rescue: UTX %s has no reverted inbound-revert outbound", originalUtxId)
			}
		}

		// Guard against duplicate rescue outbounds: reject if an active rescue
		// (PENDING or OBSERVED) already exists. A REVERTED rescue may be retried.
		for _, ob := range originalUtx.OutboundTx {
			if ob == nil || ob.TxType != types.TxType_RESCUE_FUNDS {
				continue
			}
			if ob.OutboundStatus == types.Status_PENDING || ob.OutboundStatus == types.Status_OBSERVED {
				return fmt.Errorf("rescue: UTX %s already has an active rescue outbound (%s)", originalUtxId, ob.Id)
			}
		}

		// Resolve external asset address from PRC20 → token config for the source chain.
		tokenCfg, err := k.uregistryKeeper.GetTokenConfigByPRC20(
			ctx,
			originalUtx.InboundTx.SourceChain,
			event.PRC20,
		)
		if err != nil {
			return fmt.Errorf("rescue: token config not found for PRC20 %s on %s: %w",
				event.PRC20, originalUtx.InboundTx.SourceChain, err)
		}

		// Rescued funds go to the original revert recipient (or the sender as fallback).
		recipient := originalUtx.InboundTx.Sender
		if originalUtx.InboundTx.RevertInstructions != nil &&
			originalUtx.InboundTx.RevertInstructions.FundRecipient != "" {
			recipient = originalUtx.InboundTx.RevertInstructions.FundRecipient
		}

		logIndex := fmt.Sprintf("%d", lg.Index)
		outbound := &types.OutboundTx{
			Id:                types.GetRescueFundsOutboundId(pushChainCaip, receipt.Hash, logIndex),
			DestinationChain:  originalUtx.InboundTx.SourceChain,
			Recipient:         recipient,
			Amount:            originalUtx.InboundTx.Amount,
			ExternalAssetAddr: tokenCfg.Address,
			Prc20AssetAddr:    event.PRC20,
			Sender:            event.Sender,
			GasFee:            event.GasFee.String(),
			GasPrice:          event.GasPrice.String(),
			GasLimit:          event.GasLimit.String(),
			TxType:            types.TxType_RESCUE_FUNDS,
			OutboundStatus:    types.Status_PENDING,
			PcTx: &types.OriginatingPcTx{
				TxHash:   receipt.Hash,
				LogIndex: logIndex,
			},
		}

		// Record the rescue call as a PCTx on the original UTX so the full
		// PC-side history is visible (deposit FAILED → rescue call → outbound).
		if err := k.UpdateUniversalTx(ctx, originalUtxId, func(utx *types.UniversalTx) error {
			utx.PcTx = append(utx.PcTx, &pcTx)
			return nil
		}); err != nil {
			return fmt.Errorf("rescue: failed to record PCTx on UTX %s: %w", originalUtxId, err)
		}

		if err := k.attachOutboundsToUtx(ctx, originalUtxId, []*types.OutboundTx{outbound}, ""); err != nil {
			return fmt.Errorf("rescue: failed to attach outbound to UTX %s: %w", originalUtxId, err)
		}
	}

	return nil
}

func (k Keeper) attachOutboundsToUtx(
	ctx sdk.Context,
	utxId string,
	outbounds []*types.OutboundTx,
	revertMsg string, // revert msg if the outbound is for a inbound revert
) error {

	if len(outbounds) == 0 {
		return nil
	}
	return k.UpdateUniversalTx(ctx, utxId, func(utx *types.UniversalTx) error {

		for _, outbound := range outbounds {

			utx.OutboundTx = append(utx.OutboundTx, outbound)

			var pcTxHash string
			var logIndex string

			if outbound.PcTx != nil {
				pcTxHash = outbound.PcTx.TxHash
				logIndex = outbound.PcTx.LogIndex
			}

			evt, err := types.NewOutboundCreatedEvent(types.OutboundCreatedEvent{
				UniversalTxId:    utxId,
				TxID:             outbound.Id,
				DestinationChain: outbound.DestinationChain,
				Recipient:        outbound.Recipient,
				Amount:           outbound.Amount,
				AssetAddr:        outbound.ExternalAssetAddr,
				Sender:           outbound.Sender,
				Payload:          outbound.Payload,
				GasFee:           outbound.GasFee,
				GasLimit:         outbound.GasLimit,
				GasPrice:         outbound.GasPrice,
				GasToken:         outbound.GasToken,
				TxType:           outbound.TxType.String(),
				PcTxHash:         pcTxHash,
				LogIndex:         logIndex,
				RevertMsg:        revertMsg,
			})
			if err == nil {
				ctx.EventManager().EmitEvent(evt)
			}
		}

		return nil
	})
}
