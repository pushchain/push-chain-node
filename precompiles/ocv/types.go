package ocv

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	uetypes "github.com/rollchains/pchain/x/ue/types"
)

// UtvKeeper defines the expected interface for the UTV keeper
type UtvKeeper interface {
	VerifyTxHashWithPayload(ctx sdk.Context, universalAccountId uetypes.UniversalAccountId, payloadHash, txHash string) (bool, error)
}
