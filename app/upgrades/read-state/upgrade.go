package readstate

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

const UpgradeName = "read-state"

// NewUpgrade constructs the upgrade definition.
//
// x/ucallback is a new module, so its store has to be added here — RunMigrations
// alone will register the module's consensus version but cannot create a store that
// the multistore was never told about, and the node fails to load at the upgrade
// height without it.
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{ucallbacktypes.StoreKey},
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
		sdkCtx.Logger().Info("🔧 Running upgrade:", "name", UpgradeName)

		// Reserve every system-contract address that is still empty.
		//
		// SYSTEM_CONTRACTS is not just the named contracts: constants.go's init()
		// fills the 0xA0-0xAF, 0xB0-0xBF and 0xC0-0xCF ranges with a full
		// proxy+admin+impl triple per slot, 47 entries in total. The genesis loop
		// runs only at InitGenesis, so every slot added after a chain launched is
		// unreserved on it — and an unreserved slot can be squatted by an ordinary
		// account, which is the squatting defence those ranges exist for.
		//
		// On donut that is 41 of the 47, all three addresses bare, including
		// UNIVERSAL_CALLBACK (0x…C2). x/ucallback only accepts ReadRequested logs
		// from that exact address, so until it holds the proxy the module is inert.
		// The remaining 6 are already deployed and the already-deployed guard skips
		// them, leaving their code untouched.
		if err := ak.URegistryKeeper.DeployMissingSystemContracts(sdkCtx); err != nil {
			return nil, err
		}
		sdkCtx.Logger().Info("Reserved any missing system contract addresses")

		// RunMigrations registers x/ucallback at its current consensus version and
		// runs its InitGenesis, which seeds Params. There is no state to migrate:
		// the module is new, so it starts empty.
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
