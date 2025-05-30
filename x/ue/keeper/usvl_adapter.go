package keeper

import (
	"context"

	"github.com/push-protocol/push-chain/x/ue/types"
	usvlkeeper "github.com/push-protocol/push-chain/x/usvl/keeper"
)

// UsvlKeeperAdapter adapts the usvl.Keeper to match the ue.USVLKeeper interface
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
// and converting the response type to match the ue module's expected type
func (a UsvlKeeperAdapter) VerifyExternalTransaction(ctx context.Context, txHash string, caipAddress string) (*types.UsvlVerificationResult, error) {
	// Call the actual USVL keeper implementation
	result, err := a.keeper.VerifyExternalTransaction(ctx, txHash, caipAddress)
	if err != nil {
		return nil, err
	}

	// Convert the result type to match ue's expected interface
	return &types.UsvlVerificationResult{
		Verified: result.Verified,
		TxInfo:   result.TxInfo,
	}, nil
}

// VerifyExternalTransactionToLocker verifies that a transaction is directed to the locker contract
// by delegating to the actual USVL keeper and converting the response type
func (a UsvlKeeperAdapter) VerifyExternalTransactionToLocker(ctx context.Context, txHash string, caipAddress string) (*types.UsvlVerificationResult, error) {
	// Call the actual USVL keeper implementation
	result, err := a.keeper.VerifyExternalTransactionToLocker(ctx, txHash, caipAddress)
	if err != nil {
		return nil, err
	}

	// Convert the result type to match ue's expected interface
	return &types.UsvlVerificationResult{
		Verified: result.Verified,
		TxInfo:   result.TxInfo,
	}, nil
}

// GetFundsAddedEventTopic returns the event topic signature for the FundsAdded event for a given chain identifier
func (a UsvlKeeperAdapter) GetFundsAddedEventTopic(ctx context.Context, chainIdentifier string) (string, error) {
	// First try to get all chain configs
	configs, err := a.keeper.GetAllChainConfigs(ctx)
	if err != nil {
		return "", err
	}

	// Look for the chain with matching CAIP prefix
	for _, config := range configs {
		if config.CaipPrefix == chainIdentifier {
			// Return the FundsAddedEventTopic from the matching config
			if config.FundsAddedEventTopic != "" {
				return config.FundsAddedEventTopic, nil
			}
			// If not set in config, return the default value
			return "0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0", nil
		}
	}

	// If no matching chain is found, return the default value
	return "0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0", nil
}
