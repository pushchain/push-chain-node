package keeper

import (
	"context"
	"fmt"
	"math/big"

	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// VerifyAndGetLockedFunds verifies if the user has interacted with the gateway on the source chain and send the locked funds amount.
func (k Keeper) VerifyAndGetLockedFunds(ctx context.Context, ownerKey, txHash, chain string) (big.Int, uint32, error) {
	// Step 1: Load chain config
	chainConfig, err := k.uregistryKeeper.GetChainConfig(ctx, chain)
	if err != nil {
		return *big.NewInt(0), 0, err
	}

	if !chainConfig.Enabled.IsInboundEnabled {
		return *big.NewInt(0), 0, fmt.Errorf("chain %s is not enabled", chain)
	}

	switch chainConfig.VmType {
	case uregistrytypes.VmType_EVM:
		usdValue, err := k.verifyEVMAndGetFunds(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return *big.NewInt(0), 0, fmt.Errorf("evm tx verification failed: %w", err)
		}
		usdAmount := new(big.Int)
		usdAmount, ok := usdAmount.SetString(usdValue.Amount, 10) // base 10
		if !ok {
			return *big.NewInt(0), 0, fmt.Errorf("invalid amount string: %s", usdValue.Amount)
		}
		return *usdAmount, usdValue.Decimals, nil
	case uregistrytypes.VmType_SVM:
		usdValue, err := k.verifySVMAndGetFunds(ctx, ownerKey, txHash, chainConfig)
		if err != nil {
			return *big.NewInt(0), 0, fmt.Errorf("evm tx verification failed: %w", err)
		}
		usdAmount := new(big.Int)
		usdAmount, ok := usdAmount.SetString(usdValue.Amount, 10) // base 10
		if !ok {
			return *big.NewInt(0), 0, fmt.Errorf("invalid amount string: %s", usdValue.Amount)
		}
		return *usdAmount, usdValue.Decimals, nil
	default:
		return *big.NewInt(0), 0, fmt.Errorf("unsupported VM type %s for chain %s", chainConfig.VmType.String(), chain)
	}
}
