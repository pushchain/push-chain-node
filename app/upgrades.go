package app

import (
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/pushchain/push-chain-node/app/upgrades"
	dummytest "github.com/pushchain/push-chain-node/app/upgrades/dummy-test"
	ethhashfix "github.com/pushchain/push-chain-node/app/upgrades/eth-hash-fix"
	evmrpcfix "github.com/pushchain/push-chain-node/app/upgrades/evm-rpc-fix"
	feeabs "github.com/pushchain/push-chain-node/app/upgrades/fee-abs"
	gasoracle "github.com/pushchain/push-chain-node/app/upgrades/gas-oracle"
	"github.com/pushchain/push-chain-node/app/upgrades/noop"
	pcmintcap "github.com/pushchain/push-chain-node/app/upgrades/pc-mint-cap"
	solanafix "github.com/pushchain/push-chain-node/app/upgrades/solana-fix"
	tsscore "github.com/pushchain/push-chain-node/app/upgrades/tss-core"
	tsscoreevmparamsfix "github.com/pushchain/push-chain-node/app/upgrades/tss-core-evm-params-fix"
	tsscorefix "github.com/pushchain/push-chain-node/app/upgrades/tss-core-fix"
)

// Upgrades list of chain upgrades
var Upgrades = []upgrades.Upgrade{
	feeabs.NewUpgrade(),
	solanafix.NewUpgrade(),
	ethhashfix.NewUpgrade(),
	gasoracle.NewUpgrade(),
	pcmintcap.NewUpgrade(),
	tsscore.NewUpgrade(),
	tsscorefix.NewUpgrade(),
	tsscoreevmparamsfix.NewUpgrade(),
	evmrpcfix.NewUpgrade(),
	dummytest.NewUpgrade(),
}

// RegisterUpgradeHandlers registers the chain upgrade handlers
func (app *ChainApp) RegisterUpgradeHandlers() {
	// setupLegacyKeyTables(&app.ParamsKeeper)
	if len(Upgrades) == 0 {
		// always have a unique upgrade registered for the current version to test in system tests
		Upgrades = append(Upgrades, noop.NewUpgrade(app.Version()))
	}

	keepers := upgrades.AppKeepers{
		AccountKeeper:         &app.AccountKeeper,
		ParamsKeeper:          &app.ParamsKeeper,
		ConsensusParamsKeeper: &app.ConsensusParamsKeeper,
		IBCKeeper:             app.IBCKeeper,
		Codec:                 app.appCodec,
		GetStoreKey:           app.GetKey,
		EVMKeeper:             app.EVMKeeper,

		// Module keepers
		UExecutorKeeper:   &app.UexecutorKeeper,
		UTxVerifierKeeper: &app.UtxverifierKeeper,
		URegistryKeeper:   &app.UregistryKeeper,
		UValidatorKeeper:  &app.UvalidatorKeeper,
	}

	// register all upgrade handlers
	for _, upgrade := range Upgrades {
		app.UpgradeKeeper.SetUpgradeHandler(
			upgrade.UpgradeName,
			upgrade.CreateUpgradeHandler(
				app.ModuleManager,
				app.configurator,
				&keepers,
			),
		)
	}

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Sprintf("failed to read upgrade info from disk %s", err))
	}

	if app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return
	}

	// register store loader for current upgrade
	for _, upgrade := range Upgrades {
		if upgradeInfo.Name == upgrade.UpgradeName {
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &upgrade.StoreUpgrades)) // nolint:gosec
			break
		}
	}
}
