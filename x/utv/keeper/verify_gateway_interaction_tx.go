package keeper

import (
	"context"
	"fmt"

	uetypes "github.com/rollchains/pchain/x/ue/types"
	utvtypes "github.com/rollchains/pchain/x/utv/types"
)

// VerifyGatewayInteractionTx only verifies if the user has interacted with the gateway on the source chain.
func (k Keeper) VerifyGatewayInteractionTx(ctx context.Context, ownerKey, txHash, chain string) error {
	// Step 1: Load chain config
	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chain)
	if err != nil {
		return err
	}

	if !chainConfig.Enabled {
		return fmt.Errorf("chain %s is not enabled", chain)
	}

	// Step 3: Normalize tx hash
	txHashNormalized, err := utvtypes.NormalizeTxHash(txHash, chainConfig.VmType)
	if err != nil {
		return fmt.Errorf("failed to normalize tx hash: %w", err)
	}

	switch chainConfig.VmType {
	case uetypes.VM_TYPE_EVM:
		if err := k.verifyEVMInteraction(ctx, ownerKey, txHashNormalized, chainConfig); err != nil {
			return fmt.Errorf("evm tx verification failed: %w", err)
		}
	case uetypes.VM_TYPE_SVM:
		if err := k.verifySVMInteraction(ctx, ownerKey, txHashNormalized, chainConfig); err != nil {
			return fmt.Errorf("svm tx verification failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chain)
	}
	return nil
}
