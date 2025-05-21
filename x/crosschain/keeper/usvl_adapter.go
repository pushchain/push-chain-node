package keeper

import (
	"context"

	"github.com/push-protocol/push-chain/x/crosschain/types"
	usvlkeeper "github.com/push-protocol/push-chain/x/usvl/keeper"
)

// UsvlKeeperAdapter adapts the usvl.Keeper to match the crosschain.USVLKeeper interface
type UsvlKeeperAdapter struct {
	keeper usvlkeeper.Keeper
}

// NewUsvlKeeperAdapter creates a new adapter for the USVL keeper
func NewUsvlKeeperAdapter(keeper usvlkeeper.Keeper) UsvlKeeperAdapter {
	return UsvlKeeperAdapter{
		keeper: keeper,
	}
}

// VerifyExternalTransaction verifies transactions by delegating to the actual USVL keeper
// and converting the response type to match the crosschain module's expected type
func (a UsvlKeeperAdapter) VerifyExternalTransaction(ctx context.Context, txHash string, caipAddress string) (*types.UsvlVerificationResult, error) {
	// Call the actual USVL keeper implementation
	result, err := a.keeper.VerifyExternalTransaction(ctx, txHash, caipAddress)
	if err != nil {
		return nil, err
	}

	// Convert the result type to match crosschain's expected interface
	return &types.UsvlVerificationResult{
		Verified: result.Verified,
		TxInfo:   result.TxInfo,
	}, nil
}
