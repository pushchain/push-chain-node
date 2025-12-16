package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
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

		if strings.ToLower(lg.Topics[0]) != strings.ToLower(types.UniversalTxWithdrawEventSig) {
			continue
		}

		event, err := types.DecodeUniversalTxWithdrawFromLog(lg)
		if err != nil {
			return nil, fmt.Errorf("failed to decode UniversalTxWithdraw: %w", err)
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
			GasLimit:          event.GasLimit.String(),
			TxType:            event.TxType,
			PcTx: &types.OriginatingPcTx{
				TxHash:   receipt.Hash,
				LogIndex: fmt.Sprintf("%d", lg.Index),
			},
			RevertInstructions: &types.RevertInstructions{
				FundRecipient: event.RevertRecipient,
			},
			OutboundStatus: types.Status_PENDING,
			Id:             types.GetOutboundId(utxId, receipt.Hash, lg.Index),
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
		UniversalStatus: types.UniversalTxStatus_PC_EXECUTED_SUCCESS,
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

			// ABI-encode (utx_id, outbound_id)
			txIDHex, err := types.EncodeOutboundTxIDHex(utxId, outbound.Id)
			if err != nil {
				return fmt.Errorf("failed to encode outbound txID: %w", err)
			}

			var pcTxHash string
			var logIndex string

			if outbound.PcTx != nil {
				pcTxHash = outbound.PcTx.TxHash
				logIndex = outbound.PcTx.LogIndex
			}

			evt, err := types.NewOutboundCreatedEvent(types.OutboundCreatedEvent{
				UniversalTxId:    utxId,
				OutboundId:       outbound.Id,
				TxID:             txIDHex,
				DestinationChain: outbound.DestinationChain,
				Recipient:        outbound.Recipient,
				Amount:           outbound.Amount,
				AssetAddr:        outbound.ExternalAssetAddr,
				Sender:           outbound.Sender,
				Payload:          outbound.Payload,
				GasLimit:         outbound.GasLimit,
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
