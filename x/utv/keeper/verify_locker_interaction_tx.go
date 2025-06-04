package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/ue/types"
)

// VerifyLockerInteractionTx only verifies if the user has interacted with the locker on the source chain.
func (k Keeper) VerifyLockerInteractionTx(ctx context.Context, ownerKey, txHash, chainId string) error {
	// 1. Fetch Chain config
	// 2. Check if already verified -> return
	// 3. Redirect to specific VM verification fn
	// 4. Return error if verification fails

	if exists, err := k.IsTxHashVerified(ctx, chainId, txHash); err != nil {
		return err
	} else if exists {
		return nil
	}

	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chainId)
	if err != nil {
		return err
	}

	switch chainConfig.VmType {
	case types.VM_TYPE_EVM:
		if err := k.verifyEVMInteraction(ctx, ownerKey, txHash, chainConfig); err != nil {
			return fmt.Errorf("evm tx verification failed: %w", err)
		}
	case types.VM_TYPE_SVM:
		if err := k.verifySVMInteraction(ctx, ownerKey, txHash, chainId); err != nil {
			return fmt.Errorf("svm tx verification failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chainId)
	}
	return nil
}
