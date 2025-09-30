package v2

import (
	"fmt"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/x/utxverifier/keeper"
	utxverifiertypes "github.com/pushchain/push-chain-node/x/utxverifier/types"
)

// OldVerifiedTxMetadata matches the old on-disk protobuf shape (single PayloadHash)
type OldVerifiedTxMetadata struct {
	Minted      bool                       `protobuf:"varint,1,opt,name=minted,proto3" json:"minted,omitempty"`
	PayloadHash string                     `protobuf:"bytes,2,opt,name=payload_hash,proto3" json:"payload_hash,omitempty"`
	UsdValue    *utxverifiertypes.USDValue `protobuf:"bytes,3,opt,name=usd_value,proto3" json:"usd_value,omitempty"`
	Sender      string                     `protobuf:"bytes,4,opt,name=sender,proto3" json:"sender,omitempty"`
}

func (*OldVerifiedTxMetadata) ProtoMessage()    {}
func (m *OldVerifiedTxMetadata) Reset()         { *m = OldVerifiedTxMetadata{} }
func (m *OldVerifiedTxMetadata) String() string { return fmt.Sprintf("%+v", *m) }

// MigrateVerifiedInboundTxs converts old VerifiedTxMetadata (single PayloadHash) to the new shape
// (PayloadHashes []string) using the keeper's collections API.
func MigrateVerifiedInboundTxs(ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec) error {
	// Build a schema builder – use the keeper's SchemaBuilder so it points to the same store service.
	sb := k.SchemaBuilder()

	// Create a temporary map that decodes values using the OLD struct type.
	oldMap := collections.NewMap(
		sb,
		utxverifiertypes.VerifiedInboundTxsKeyPrefix,
		utxverifiertypes.VerifiedInboundTxsName,
		collections.StringKey,
		codec.CollValue[OldVerifiedTxMetadata](cdc),
	)

	// Iterate old map
	iter, err := oldMap.Iterate(ctx, nil) // nil => iterate all keys
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}

		oldMeta, err := iter.Value()
		if err != nil {
			return err
		}

		// Construct the new metadata type
		newMeta := utxverifiertypes.VerifiedTxMetadata{
			Minted:   oldMeta.Minted,
			UsdValue: oldMeta.UsdValue,
			Sender:   oldMeta.Sender,
		}

		// Wrap the old single payload hash into a slice (if present)
		if oldMeta.PayloadHash != "" {
			newMeta.PayloadHashes = []string{oldMeta.PayloadHash}
		} else {
			newMeta.PayloadHashes = []string{}
		}

		// Persist using the keeper's new collection
		if err := k.VerifiedInboundTxs.Set(ctx, key, newMeta); err != nil {
			return err
		}
	}

	return nil
}
