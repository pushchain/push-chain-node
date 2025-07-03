package keeper

import (
	"context"
	"fmt"
	"math/big"

	uetypes "github.com/rollchains/pchain/x/ue/types"
	utvtypes "github.com/rollchains/pchain/x/utv/types"
)

// VerifyAndGetLockedFunds verifies if the user has interacted with the gateway on the source chain and send the locked funds amount.
func (k Keeper) VerifyAndGetLockedFunds(ctx context.Context, ownerKey, txHash, chain string) (big.Int, uint32, error) {
	// Step 1: Check if already verified
	if exists, err := k.IsTxHashVerified(ctx, chain, txHash); err != nil {
		return *big.NewInt(0), 0, err
	} else if exists {
		return *big.NewInt(0), 0, fmt.Errorf("tx is already verified once")
	}

	// Step 2: Load chain config
	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chain)
	if err != nil {
		return *big.NewInt(0), 0, err
	}

	if !chainConfig.Enabled {
		return *big.NewInt(0), 0, fmt.Errorf("chain %s is not enabled", chain)
	}

	// Step 3: Normalize tx hash
	txHashNormalized, err := utvtypes.NormalizeTxHash(txHash, chainConfig.VmType)
	if err != nil {
		return *big.NewInt(0), 0, fmt.Errorf("failed to normalize tx hash: %w", err)
	}

	switch chainConfig.VmType {
	case uetypes.VM_TYPE_EVM:
		amount, decimals, err := k.verifyEVMAndGetFunds(ctx, ownerKey, txHashNormalized, chainConfig)
		if err != nil {
			return amount, decimals, fmt.Errorf("evm tx verification failed: %w", err)
		}

		// tx is verified, now store it
		if err := k.storeVerifiedTx(ctx, chain, txHashNormalized); err != nil {
			return amount, decimals, fmt.Errorf("failed to store verified tx: %w", err)
		}
		return amount, decimals, nil
	case uetypes.VM_TYPE_SVM:
		amount, decimals, err := k.verifySVMAndGetFunds(ctx, ownerKey, txHashNormalized, chainConfig)
		if err != nil {
			return amount, decimals, fmt.Errorf("svm tx verification failed: %w", err)
		}

		// tx is verified, now store it
		if err := k.storeVerifiedTx(ctx, chain, txHashNormalized); err != nil {
			return amount, decimals, fmt.Errorf("failed to store verified tx: %w", err)
		}
		return amount, decimals, nil
	default:
		return *big.NewInt(0), 0, fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chain)
	}
}
