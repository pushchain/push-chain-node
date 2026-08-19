package app

import (
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/pushchain/push-chain-node/app/upgrades"
	aiauditfixes "github.com/pushchain/push-chain-node/app/upgrades/ai-audit-fixes"
	aiauditfixes2 "github.com/pushchain/push-chain-node/app/upgrades/ai-audit-fixes-2"
	ceagasandpayload "github.com/pushchain/push-chain-node/app/upgrades/cea-gas-and-payload"
	ceapayloadverificationfix "github.com/pushchain/push-chain-node/app/upgrades/cea-payload-verification-fix"
	chainmeta "github.com/pushchain/push-chain-node/app/upgrades/chain-meta"
	chainmetavotegasless "github.com/pushchain/push-chain-node/app/upgrades/chain-meta-vote-gasless"
	contractauditchanges "github.com/pushchain/push-chain-node/app/upgrades/contract-audit-changes"
	ethhashfix "github.com/pushchain/push-chain-node/app/upgrades/eth-hash-fix"
	evmblockscoutfix "github.com/pushchain/push-chain-node/app/upgrades/evm-blockscout-fix"
	evmchainidffix "github.com/pushchain/push-chain-node/app/upgrades/evm-chainid-fix"
	evmparamsmigration "github.com/pushchain/push-chain-node/app/upgrades/evm-params-migration"
	evmpreinstalls "github.com/pushchain/push-chain-node/app/upgrades/evm-preinstalls"
	evmrpcfix "github.com/pushchain/push-chain-node/app/upgrades/evm-rpc-fix"
	evmv040 "github.com/pushchain/push-chain-node/app/upgrades/evm-v0-4-0"
	evmv050 "github.com/pushchain/push-chain-node/app/upgrades/evm-v0-5-0"
	evmderivedgasprice "github.com/pushchain/push-chain-node/app/upgrades/evm-derived-gas-price"
	evmv060 "github.com/pushchain/push-chain-node/app/upgrades/evm-v0-6-0"
	feeabs "github.com/pushchain/push-chain-node/app/upgrades/fee-abs"
	gasoracle "github.com/pushchain/push-chain-node/app/upgrades/gas-oracle"
	"github.com/pushchain/push-chain-node/app/upgrades/noop"
	outbound "github.com/pushchain/push-chain-node/app/upgrades/outbound"
	pcmintcap "github.com/pushchain/push-chain-node/app/upgrades/pc-mint-cap"
	pc20 "github.com/pushchain/push-chain-node/app/upgrades/pc20"
	proxybytecodefix "github.com/pushchain/push-chain-node/app/upgrades/proxy-bytecode-fix"
	purgeexpiredoutbounds "github.com/pushchain/push-chain-node/app/upgrades/purge-expired-outbounds"
	removefeeabsv1 "github.com/pushchain/push-chain-node/app/upgrades/remove-fee-abs-v1"
	removeutxverifier "github.com/pushchain/push-chain-node/app/upgrades/remove-utxverifier"
	sdkv053 "github.com/pushchain/push-chain-node/app/upgrades/sdk-v0-53"
	securityauditfixes "github.com/pushchain/push-chain-node/app/upgrades/security-audit-fixes"
	solanafix "github.com/pushchain/push-chain-node/app/upgrades/solana-fix"
	supplyburn "github.com/pushchain/push-chain-node/app/upgrades/supply-burn"
	supplyslash "github.com/pushchain/push-chain-node/app/upgrades/supply-slash"
	tsscore "github.com/pushchain/push-chain-node/app/upgrades/tss-core"
	tsscoreevmparamsfix "github.com/pushchain/push-chain-node/app/upgrades/tss-core-evm-params-fix"
	tsscorefix "github.com/pushchain/push-chain-node/app/upgrades/tss-core-fix"
	tssfundmigrationfixes "github.com/pushchain/push-chain-node/app/upgrades/tss-fund-migration-fixes"
	tssmigration "github.com/pushchain/push-chain-node/app/upgrades/tss-migration"
	tssvotegasless "github.com/pushchain/push-chain-node/app/upgrades/tss-vote-gasless"
	ueamigration "github.com/pushchain/push-chain-node/app/upgrades/uea-migration"
	universaltxv1 "github.com/pushchain/push-chain-node/app/upgrades/universal-tx-v1"
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
	evmblockscoutfix.NewUpgrade(),
	tssvotegasless.NewUpgrade(),
	removefeeabsv1.NewUpgrade(),
	outbound.NewUpgrade(),
	universaltxv1.NewUpgrade(),
	proxybytecodefix.NewUpgrade(),
	supplyslash.NewUpgrade(),
	supplyburn.NewUpgrade(),
	chainmeta.NewUpgrade(),
	chainmetavotegasless.NewUpgrade(),
	ceagasandpayload.NewUpgrade(),
	ceapayloadverificationfix.NewUpgrade(),
	aiauditfixes.NewUpgrade(),
	aiauditfixes2.NewUpgrade(),
	ueamigration.NewUpgrade(),
	tssmigration.NewUpgrade(),
	purgeexpiredoutbounds.NewUpgrade(),
	removeutxverifier.NewUpgrade(),
	tssfundmigrationfixes.NewUpgrade(),
	contractauditchanges.NewUpgrade(),
	evmv040.NewUpgrade(),
	evmparamsmigration.NewUpgrade(),
	evmchainidffix.NewUpgrade(),
	evmpreinstalls.NewUpgrade(),
	evmv050.NewUpgrade(),
	securityauditfixes.NewUpgrade(),
	// cosmos/evm v0.6.0 — runs the standard transfer module v5->v6 migration
	evmv060.NewUpgrade(),
	// pc20 — no-op binary swap; VAULT_PC/VAULT_PC20 deployed separately from EVM side
	pc20.NewUpgrade(),
	// sdk-v0.53 — Cosmos SDK v0.50.10 -> v0.53.7; no-op (no module ConsensusVersion changes)
	sdkv053.NewUpgrade(),
	// evm-derived-gas-price — cosmos/evm bump for the derived-tx gas price fix;
	// no-op (JSON-RPC reporting only, no module ConsensusVersion changes)
	evmderivedgasprice.NewUpgrade(),
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
		Erc20Keeper:           &app.Erc20Keeper,
		BankKeeper:            app.BankKeeper,

		// Module keepers
		UExecutorKeeper:  &app.UexecutorKeeper,
		URegistryKeeper:  &app.UregistryKeeper,
		UValidatorKeeper: &app.UvalidatorKeeper,
		UTssKeeper:       &app.UtssKeeper,
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
