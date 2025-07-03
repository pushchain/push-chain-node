package ocv

import (
	"context"

	uetypes "github.com/rollchains/pchain/x/ue/types"
)

// UtvKeeper defines the expected interface for the UTV keeper
type UtvKeeper interface {
	VerifyTxHashWithPayload(ctx context.Context, universalAccountId uetypes.UniversalAccountId, payloadHash, txHash string) (bool, error)
}
