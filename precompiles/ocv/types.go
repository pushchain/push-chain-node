package ocv

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UtvKeeper defines the expected interface for the UTV keeper
type UtvKeeper interface {
	VerifyAndGetPayloadHash(ctx sdk.Context, ownerKey, txHash, chain string) (string, error)
}
