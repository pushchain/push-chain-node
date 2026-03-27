package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) MigrateUEA(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, migrationPayload *types.MigrationPayload, signature string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get Caip2Identifier for the universal account
	caip2Identifier := universalAccountId.GetCAIP2()

	k.Logger().Info("migrate UEA",
		"from", evmFrom.Hex(),
		"chain", caip2Identifier,
		"owner", universalAccountId.Owner,
	)

	// Step 1: Parse and validate payload and signature
	_, err := types.NewAbiMigrationPayload(migrationPayload)
	if err != nil {
		return errors.Wrapf(err, "invalid migration payload")
	}

	// add signature verification
	signatureVal, err := utils.HexToBytes(signature)
	if err != nil {
		return errors.Wrapf(err, "invalid signature format")
	}

	chainConfig, err := k.uregistryKeeper.GetChainConfig(sdkCtx, caip2Identifier)
	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain %s", caip2Identifier)
	}

	// TODO: Decide later if migration should be disabled if inbound is disabled
	if !chainConfig.Enabled.IsInboundEnabled {
		k.Logger().Warn("migrate UEA rejected: chain not enabled", "chain", caip2Identifier)
		return fmt.Errorf("chain %s is not enabled", caip2Identifier)
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// Step 2: Compute smart account address
	// Calling factory contract to compute the UEA address
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, evmFrom, factoryAddress, universalAccountId)
	if err != nil {
		return err
	}

	if !isDeployed {
		k.Logger().Warn("migrate UEA rejected: UEA not deployed", "chain", caip2Identifier, "owner", universalAccountId.Owner)
		return fmt.Errorf("UEA is not deployed")
	}

	k.Logger().Debug("migrating UEA",
		"uea", ueaAddr.Hex(),
		"chain", caip2Identifier,
		"from", evmFrom.Hex(),
	)

	// Step 3: Migrate UEA through UEA
	receipt, err := k.CallUEAMigrateUEA(sdkCtx, evmFrom, ueaAddr, migrationPayload, signatureVal)
	if err != nil {
		return err
	}

	k.Logger().Info("UEA migrated",
		"chain", caip2Identifier,
		"uea", ueaAddr.Hex(),
		"tx_hash", receipt.Hash,
		"gas_used", receipt.GasUsed,
	)

	return nil
}
