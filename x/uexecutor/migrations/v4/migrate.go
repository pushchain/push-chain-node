package v4

import (
	"fmt"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	olduexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/oldtypes"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func MigrateUniversalTx(ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec) error {
	sb := k.SchemaBuilder()

	// Use the generated old types for reading the previous state
	oldMap := collections.NewMap(
		sb,
		uexecutortypes.UniversalTxKey,
		uexecutortypes.UniversalTxName,
		collections.StringKey,
		codec.CollValue[olduexecutortypes.UniversalTx](cdc),
	)

	iter, err := oldMap.Iterate(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to iterate old universal txs: %w", err)
	}
	defer iter.Close()

	var migratedCount uint64

	for ; iter.Valid(); iter.Next() {
		oldKey, err := iter.Key()
		if err != nil {
			return fmt.Errorf("failed to read key: %w", err)
		}

		oldTx, err := iter.Value()
		if err != nil {
			return fmt.Errorf("failed to read value for key %s: %w", oldKey, err)
		}

		// Build new UniversalTx using old key as new .Id
		newTx := uexecutortypes.UniversalTx{
			Id:              oldKey,
			InboundTx:       mapInbound(oldTx.InboundTx),
			PcTx:            oldTx.PcTx,
			OutboundTx:      mapOutbound(oldTx.OutboundTx),
			UniversalStatus: oldTx.UniversalStatus,
		}

		if err := k.UniversalTx.Set(ctx, newTx.Id, newTx); err != nil {
			return fmt.Errorf("failed to store migrated universal tx %s: %w", newTx.Id, err)
		}

		migratedCount++
	}

	ctx.Logger().With(
		"module", "uexecutor",
		"migrated_utxs", migratedCount,
	).Info("v4 → universal tx migration completed")

	return nil
}

func mapInbound(old *olduexecutortypes.Inbound) *uexecutortypes.Inbound {
	if old == nil {
		return nil
	}

	newInbound := &uexecutortypes.Inbound{
		SourceChain:      old.SourceChain,
		TxHash:           old.TxHash,
		Sender:           old.Sender,
		Recipient:        old.Recipient,
		Amount:           old.Amount,
		AssetAddr:        old.AssetAddr,
		LogIndex:         old.LogIndex,
		UniversalPayload: old.UniversalPayload,
		VerificationData: old.VerificationData,
		RevertInstructions: &uexecutortypes.RevertInstructions{
			FundRecipient: old.Sender,
		},
	}

	// Map old enum values to new enum values
	// Using the generated old enum constants
	switch old.TxType {
	case olduexecutortypes.InboundTxType_GAS:
		newInbound.TxType = uexecutortypes.TxType_GAS
	case olduexecutortypes.InboundTxType_FUNDS:
		newInbound.TxType = uexecutortypes.TxType_FUNDS
	case olduexecutortypes.InboundTxType_FUNDS_AND_PAYLOAD:
		newInbound.TxType = uexecutortypes.TxType_FUNDS_AND_PAYLOAD
	case olduexecutortypes.InboundTxType_GAS_AND_PAYLOAD:
		newInbound.TxType = uexecutortypes.TxType_GAS_AND_PAYLOAD
	default:
		newInbound.TxType = uexecutortypes.TxType_UNSPECIFIED_TX
	}

	return newInbound
}

func mapOutbound(old *olduexecutortypes.OutboundTx) []*uexecutortypes.OutboundTx {
	if old == nil || old.DestinationChain == "" {
		return nil
	}

	newOutbound := &uexecutortypes.OutboundTx{
		DestinationChain:  old.DestinationChain,
		Recipient:         old.Recipient,
		Amount:            old.Amount,
		ExternalAssetAddr: old.AssetAddr,
	}

	// If old tx had tx_hash -> assume it was observed successfully
	if old.TxHash != "" {
		newOutbound.ObservedTx = &uexecutortypes.OutboundObservation{
			Success: true,
			TxHash:  old.TxHash,
		}
	}

	return []*uexecutortypes.OutboundTx{newOutbound}
}
