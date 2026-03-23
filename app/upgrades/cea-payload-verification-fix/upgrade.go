package ceapayloadverificationfix

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "cea-payload-verification-fix"

// NewUpgrade constructs the upgrade definition
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
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)
		logger.Info("Starting upgrade handler")

		// ── Fix 1: CEA payload sender ───────────────────────────────────────────
		// In the CEA (Chain Enabled Abstraction) route, the verified payload hash
		// was stored with the inbound tx sender (CEA executor) as the sender field.
		// The UEA contract's executePayload calls the verifyTxHash precompile with
		// id.owner (UEA owner), causing a sender mismatch and verification failure.
		// Fix: store the UEA owner as the sender when isCEA=true and recipient is a UEA.
		logger.Info("Fix: CEA inbound payload hash now stores UEA owner as sender instead of inbound tx sender")

		// ── Fix 2: CEA payload chain ────────────────────────────────────────────
		// The verified payload hash was stored under the inbound source chain
		// (e.g., eip155:97), but the UEA contract calls verifyTxHash with its own
		// origin chain (e.g., eip155:11155111). This chain mismatch caused the
		// precompile lookup to fail.
		// Fix: store the payload hash under the UEA's origin chain for CEA inbounds.
		logger.Info("Fix: CEA inbound payload hash now stored under UEA origin chain instead of inbound source chain")

		// ── State migration ──────────────────────────────────────────────────────
		// No state migration required – fixes apply to new inbound execution only.
		// Previously failed CEA inbounds will succeed on retry with the new logic.
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			logger.Error("RunMigrations failed", "error", err)
			return nil, err
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
