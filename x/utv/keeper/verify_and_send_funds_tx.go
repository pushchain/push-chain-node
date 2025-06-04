package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/ue/types"
)

// VerifyLockerInteractionTx verifies if the user has interacted with the locker on the source chain and send the locked funds amount.
func (k Keeper) VerifyAndGetLockedFunds(ctx context.Context, ownerKey, txHash, chainId string) (string, error) {
	// 1. Fetch Chain config
	// 2. Check if already verified -> return err
	// 3. Redirect to specific VM verification fn
	// 4. Return error if verification fails

	if exists, err := k.IsTxHashVerified(ctx, chainId, txHash); err != nil {
		return "", err
	} else if exists {
		return "", fmt.Errorf("tx is already verified once")
	}

	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chainId)
	if err != nil {
		return "", err
	}

	switch chainConfig.VmType {
	case types.VM_TYPE_EVM:
		if amount, err := k.verifyEVMAndGetFunds(ctx, ownerKey, txHash, chainConfig); err != nil {
			err := k.storeVerifiedTx(ctx, chainId, txHash)
			if err != nil {
				return amount, fmt.Errorf("failed to store verified tx: %w", err)
			}
			return amount, fmt.Errorf("evm tx verification failed: %w", err)
		}
	case types.VM_TYPE_SVM:
		if amount, err := k.verifySVMAndGetFunds(ctx, ownerKey, txHash, chainId); err != nil {
			err := k.storeVerifiedTx(ctx, chainId, txHash)
			if err != nil {
				return amount, fmt.Errorf("failed to store verified tx: %w", err)
			}
			return amount, fmt.Errorf("svm tx verification failed: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chainId)
	}
	return "", nil
}
