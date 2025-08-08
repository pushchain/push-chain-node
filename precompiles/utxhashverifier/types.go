package utxhashverifier

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UtxverifierKeeper defines the expected interface for the UTV keeper
type UtxverifierKeeper interface {
	VerifyAndGetPayloadHash(ctx sdk.Context, ownerKey, txHash, chain string) (string, error)
}
