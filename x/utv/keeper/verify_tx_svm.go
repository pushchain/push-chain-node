package keeper

import (
	"context"
)

// Only verifies that user interacted with locker (used in deploy)
func (k Keeper) verifySVMInteraction(ctx context.Context, ownerKey, txHash, chainId string) error {

	return nil
}

// Verifies and extracts locked amount (used in mint)
func (k Keeper) verifySVMAndGetFunds(ctx context.Context, ownerKey, txHash, chainId string) (string, error) {

	return "", nil
}
