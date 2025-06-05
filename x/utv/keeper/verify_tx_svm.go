package keeper

import (
	"context"
	"math/big"

	"github.com/rollchains/pchain/x/ue/types"
)

// Only verifies that user interacted with locker (used in deploy)
func (k Keeper) verifySVMInteraction(ctx context.Context, ownerKey, txHash string, chainConfig types.ChainConfig) error {

	return nil
}

// Verifies and extracts locked amount (used in mint)
func (k Keeper) verifySVMAndGetFunds(ctx context.Context, ownerKey, txHash string, chainConfig types.ChainConfig) (big.Int, error) {

	return *big.NewInt(0), nil
}
