package auditfixes

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

const UpgradeName = "audit-fixes"

// Hacken audit remediation plus the evm-side fixes. No store or module version
// changes; the handler seeds the one new param so the ante's zero-fallback is
// never the operative value.
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
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("starting audit-fixes upgrade")

		if err := setMaxGaslessTxGas(ctx, ak, logger); err != nil {
			return nil, err
		}

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}

		logger.Info("audit-fixes upgrade complete")
		return versionMap, nil
	}
}

// setMaxGaslessTxGas writes the cap a gasless tx may declare. Params stored before
// this release decode the new field as 0, which the ante reads as "unset" and
// substitutes the default for; setting it here makes the live value explicit and
// governance-adjustable instead.
func setMaxGaslessTxGas(ctx context.Context, ak *upgrades.AppKeepers, logger interface{ Info(string, ...any) }) error {
	if ak == nil || ak.UExecutorKeeper == nil {
		return fmt.Errorf("uexecutor keeper unavailable")
	}

	params, err := ak.UExecutorKeeper.GetParams(ctx)
	if err != nil {
		return fmt.Errorf("read uexecutor params: %w", err)
	}

	if params.MaxGaslessTxGas != 0 {
		logger.Info("max gasless tx gas already set, leaving it alone",
			"value", params.MaxGaslessTxGas)
		return nil
	}

	params.MaxGaslessTxGas = uexecutortypes.DefaultMaxGaslessTxGas
	if err := ak.UExecutorKeeper.UpdateParams(ctx, params); err != nil {
		return fmt.Errorf("set max gasless tx gas: %w", err)
	}

	logger.Info("max gasless tx gas set", "value", uexecutortypes.DefaultMaxGaslessTxGas)
	return nil
}
