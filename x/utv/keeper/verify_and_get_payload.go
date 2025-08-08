package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	uetypes "github.com/pushchain/push-chain-node/x/ue/types"
)

// VerifyAndGetLockedFunds verifies if the user has interacted with the gateway on the source chain and send the locked funds amount.
func (k Keeper) VerifyAndGetPayloadHash(ctx sdk.Context, ownerKey, txHash, chain string) (string, error) {
	// Step 1: Load chain config
	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chain)
	if err != nil {
		return "", err
	}

	if !chainConfig.Enabled {
		return "", fmt.Errorf("chain %s is not enabled", chain)
	}

	switch chainConfig.VmType {
	case uetypes.VM_TYPE_EVM:
		payloadHash, err := k.verifyEVMAndGetPayload(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return "", fmt.Errorf("evm tx verification failed: %w", err)
		}
		return payloadHash, nil
	case uetypes.VM_TYPE_SVM:
		payloadHash, err := k.verifySVMAndGetPayload(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return "", fmt.Errorf("svm tx verification failed: %w", err)
		}
		return payloadHash, nil
	default:
		return "", fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chain)
	}
}
