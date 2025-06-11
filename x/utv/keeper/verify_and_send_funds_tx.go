package keeper

import (
	"context"
	"fmt"
	"math/big"

	"github.com/rollchains/pchain/x/ue/types"
)

// VerifyLockerInteractionTx verifies if the user has interacted with the locker on the source chain and send the locked funds amount.
func (k Keeper) VerifyAndGetLockedFunds(ctx context.Context, ownerKey, txHash, chainId string) (big.Int, uint32, error) {
	if exists, err := k.IsTxHashVerified(ctx, chainId, txHash); err != nil {
		return *big.NewInt(0), 0, err
	} else if exists {
		return *big.NewInt(0), 0, fmt.Errorf("tx is already verified once")
	}

	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chainId)
	if err != nil {
		return *big.NewInt(0), 0, err
	}

	if !chainConfig.Enabled {
		return *big.NewInt(0), 0, fmt.Errorf("chain %s is not enabled", chainId)
	}

	switch chainConfig.VmType {
	case types.VM_TYPE_EVM:
		amount, decimals, err := k.verifyEVMAndGetFunds(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return amount, decimals, fmt.Errorf("svm tx verification failed: %w", err)
		}

		// tx is verified, now store it
		if err := k.storeVerifiedTx(ctx, chainId, txHash); err != nil {
			return amount, decimals, fmt.Errorf("failed to store verified tx: %w", err)
		}
		return amount, decimals, nil
	case types.VM_TYPE_SVM:
		amount, decimals, err := k.verifySVMAndGetFunds(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return amount, decimals, fmt.Errorf("svm tx verification failed: %w", err)
		}

		// tx is verified, now store it
		if err := k.storeVerifiedTx(ctx, chainId, txHash); err != nil {
			return amount, decimals, fmt.Errorf("failed to store verified tx: %w", err)
		}
		return amount, decimals, nil
	default:
		return *big.NewInt(0), 0, fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chainId)
	}
}
