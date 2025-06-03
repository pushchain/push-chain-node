package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/x/ue/types"
)

func (k Keeper) VerifyTx(ctx context.Context, ownerKey, txHash, chainId string) error {
	// 1. Fetch Chain config
	// 2. Check if already verified -> return err if yes
	// 3. Redirect to specific VM verification fn
	// 4. Return error if verification fails
	// 5. Return the locked funds usd value

	chainConfig, err := k.ueKeeper.GetChainConfig(ctx, chainId)
	if err != nil {
		return err
	}

	// MsgDeployNMSC - Don't revert if already verified
	// MsgMintPush - Revert if already verified
	// Check if the transaction is already verified
	// isVerified, err := k.IsTxHashVerified(ctx, chainId, txHash)
	// if err != nil {
	// 	return err
	// }
	// if isVerified {
	// 	return fmt.Errorf("transaction %s on chain %s is already verified", txHash, chainId)
	// }

	switch chainConfig.VmType {
	case types.VM_TYPE_EVM:
		return k.VerifyEVMTransaction(ctx, ownerKey, txHash, chainId)
	case types.VM_TYPE_SVM:
		return k.VerifySVMTransaction(ctx, ownerKey, txHash, chainId)
	default:
		return fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType, chainId)
	}
}
