package inbound

import (
	"context"
	"math/big"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	sdkmath "cosmossdk.io/math"
	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "pc-mint-cap"

// NewUpgrade constructs the upgrade definition
func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          UpgradeName,
		CreateUpgradeHandler: CreateUpgradeHandler,
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

		// --- 1. Mint 50T tokens to one address ---
		recipientAddr, err := sdk.AccAddressFromBech32("push1p9wxp8uczwdmt0d5f4nzayqezhha5lrxv0heqg")
		if err != nil {
			panic(err) // valid address required
		}

		amount := sdkmath.NewIntFromBigInt(
			new(big.Int).Mul(big.NewInt(50_000_000_000_000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)),
		)

		err = ak.UExecutorKeeper.MintPCTokensDirectly(sdkCtx, recipientAddr, amount)
		if err != nil {
			panic(err)
		}

		sdkCtx.Logger().Info("✅ Minted 50T PC tokens to upgrade recipient", "address", recipientAddr.String())

		// Run module migrations
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
