package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// VerifyAndGetLockedFunds verifies if the user has interacted with the gateway on the source chain and send the locked funds amount.
func (k Keeper) VerifyAndGetPayloadHash(ctx sdk.Context, ownerKey, txHash, chain string) ([]string, error) {
	// Step 1: Load chain config
	chainConfig, err := k.uregistryKeeper.GetChainConfig(ctx, chain)
	if err != nil {
		return nil, err
	}

	if !chainConfig.Enabled.IsInboundEnabled {
		return nil, fmt.Errorf("chain %s is not enabled", chain)
	}

	switch chainConfig.VmType {
	case uregistrytypes.VmType_EVM:
		payloadHashes, err := k.verifyEVMAndGetPayload(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return nil, fmt.Errorf("evm tx verification failed: %w", err)
		}
		return payloadHashes, nil
	case uregistrytypes.VmType_SVM:
		payloadHashes, err := k.verifySVMAndGetPayload(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return nil, fmt.Errorf("svm tx verification failed: %w", err)
		}
		return payloadHashes, nil
	default:
		return nil, fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chain)
	}
}
