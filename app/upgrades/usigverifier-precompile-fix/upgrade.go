package usigverifierprecompilefix

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
	usigverifierprecompile "github.com/pushchain/push-chain-node/precompiles/usigverifier"
)

const UpgradeName = "usigverifier-precompile-fix"

// LegacyUSigVerifierAddress is the address the Ed25519 signature verifier precompile
// used to live at. The node no longer instantiates anything at this address — the
// verifier now lives at usigverifierprecompile.USigVerifierPrecompileAddress
// (0xEC..01) — yet the address is still listed in EVM ActiveStaticPrecompiles on
// chains started from an older genesis.
//
// A declared-but-unimplemented address is worse than an unlisted one:
// Keeper.GetStaticPrecompileInstance panics with "precompiled contract not stored
// in memory" for any address that is active in params but absent from the in-memory
// precompile map, so every call to it aborts the transaction.
const LegacyUSigVerifierAddress = "0x00000000000000000000000000000000000000ca"

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

		// 1. Run module migrations
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		// 2. Point EVM ActiveStaticPrecompiles at the address the verifier is
		//    actually registered at.
		if err := syncUSigVerifierPrecompile(sdkCtx, ak, logger); err != nil {
			return nil, fmt.Errorf("syncUSigVerifierPrecompile: %w", err)
		}

		logger.Info("Upgrade complete")
		return versionMap, nil
	}
}

// syncUSigVerifierPrecompile drops the legacy Ed25519 verifier address from EVM
// ActiveStaticPrecompiles and makes sure the address the verifier is registered at
// today is present. It is a no-op when params are already in sync.
func syncUSigVerifierPrecompile(sdkCtx sdk.Context, ak *upgrades.AppKeepers, logger log.Logger) error {
	evmParams := ak.EVMKeeper.GetParams(sdkCtx)

	active, removed, added := syncActiveStaticPrecompiles(evmParams.ActiveStaticPrecompiles)
	if !removed && !added {
		logger.Info("EVM ActiveStaticPrecompiles already in sync, skipping",
			"legacy", LegacyUSigVerifierAddress,
			"current", usigverifierprecompile.USigVerifierPrecompileAddress,
		)
		return nil
	}

	evmParams.ActiveStaticPrecompiles = active

	if err := ak.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
		return fmt.Errorf("failed to set EVM params after syncing usigverifier precompile: %w", err)
	}

	logger.Info("Synced usigverifier precompile in EVM params",
		"removed_legacy", removed,
		"added_current", added,
		"legacy", LegacyUSigVerifierAddress,
		"current", usigverifierprecompile.USigVerifierPrecompileAddress,
	)
	return nil
}

// syncActiveStaticPrecompiles returns active with the legacy Ed25519 verifier
// address removed and the current one appended when missing, reporting whether
// either happened. Every other entry is left untouched.
//
// The result is kept sorted because x/vm's ValidatePrecompiles rejects an unsorted
// list; Keeper.SetParams sorts too, but exported genesis is validated as-is.
func syncActiveStaticPrecompiles(active []string) (out []string, removed, added bool) {
	out = make([]string, 0, len(active)+1)
	hasCurrent := false

	for _, addr := range active {
		if strings.EqualFold(addr, LegacyUSigVerifierAddress) {
			removed = true
			continue
		}
		if strings.EqualFold(addr, usigverifierprecompile.USigVerifierPrecompileAddress) {
			hasCurrent = true
		}
		out = append(out, addr)
	}

	if !hasCurrent {
		out = append(out, usigverifierprecompile.USigVerifierPrecompileAddress)
		added = true
	}

	slices.Sort(out)
	return out, removed, added
}
