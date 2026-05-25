package evmv040

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	erc20types "github.com/cosmos/evm/x/erc20/types"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

// Upgrade for the pushchain/evm dependency bump from v0.3.x to v0.4.0.
//
// Key changes shipped in cosmos/evm v0.4.0:
//   - Post-audit security fixes (batches 1–5) applied to EVM state machine and precompiles
//   - Enforce single EVM transaction per Cosmos transaction (#294)
//   - Evidence precompile removed (#305) — push-chain did not register it; no cleanup needed
//   - ERC20 precompile storage format changed: DynamicPrecompiles and NativePrecompiles moved
//     from concatenated hex strings under single keys to per-address prefix-store entries.
//   - Various bug fixes: revert reason format, address codec, estimate gas, blockHash RPCs, etc.
const UpgradeName = "evm-v0-4-0"

func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{},
			Deleted: []string{},
		},
	}
}

func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	keepers *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")
		logger.Info("pushchain/evm v0.3.x → v0.4.0: security audit patches, single-EVM-tx enforcement, ERC20 precompile storage migration")

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		if err := migrateERC20Precompiles(sdkCtx, keepers); err != nil {
			return nil, fmt.Errorf("migrateERC20Precompiles: %w", err)
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}

// migrateERC20Precompiles migrates DynamicPrecompiles and NativePrecompiles from the
// legacy storage format (concatenated 42-char hex strings under a single key) to the
// new per-address prefix-store format introduced in cosmos/evm v0.4.0.
func migrateERC20Precompiles(ctx sdk.Context, keepers *upgrades.AppKeepers) error {
	store := ctx.KVStore(keepers.GetStoreKey(erc20types.StoreKey))
	logger := ctx.Logger().With("migration", "erc20-precompiles")

	const addressLength = 42

	migrations := []struct {
		oldKey      string
		setter      func(sdk.Context, common.Address)
		description string
	}{
		{
			oldKey:      erc20types.CtxKeyDynamicPrecompiles,
			setter:      keepers.Erc20Keeper.SetDynamicPrecompile,
			description: "dynamic precompiles",
		},
		{
			oldKey:      erc20types.CtxKeyNativePrecompiles,
			setter:      keepers.Erc20Keeper.SetNativePrecompile,
			description: "native precompiles",
		},
	}

	for _, m := range migrations {
		oldData := store.Get([]byte(m.oldKey))
		if len(oldData) == 0 {
			logger.Info("No legacy data found, skipping", "type", m.description)
			continue
		}

		count := 0
		for i := 0; i+addressLength <= len(oldData); i += addressLength {
			addr := common.HexToAddress(string(oldData[i : i+addressLength]))
			if addr == (common.Address{}) {
				logger.Warn("Skipping zero address", "type", m.description, "position", i)
				continue
			}
			m.setter(ctx, addr)
			count++
		}

		store.Delete([]byte(m.oldKey))
		logger.Info("Migration complete", "type", m.description, "count", count)
	}

	return nil
}
