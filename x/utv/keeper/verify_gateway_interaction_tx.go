package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/ue/types"
)

// VerifyGatewayInteractionTx only verifies if the user has interacted with the gateway on the source chain.
func (k Keeper) VerifyGatewayInteractionTx(ctx context.Context, ownerKey, txHash, chain string) error {
	if exists, err := k.IsTxHashVerified(ctx, chain, txHash); err != nil {
		return err
	} else if exists {
		return nil
	}

	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chain)
	if err != nil {
		return err
	}

	if !chainConfig.Enabled {
		return fmt.Errorf("chain %s is not enabled", chain)
	}

	switch chainConfig.VmType {
	case types.VM_TYPE_EVM:
		if err := k.verifyEVMInteraction(ctx, ownerKey, txHash, chainConfig); err != nil {
			return fmt.Errorf("evm tx verification failed: %w", err)
		}
	case types.VM_TYPE_SVM:
		if err := k.verifySVMInteraction(ctx, ownerKey, txHash, chainConfig); err != nil {
			return fmt.Errorf("svm tx verification failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chain)
	}
	return nil
}
