package evmpreinstalls

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "evm-preinstalls"

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

		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, fmt.Errorf("RunMigrations: %w", err)
		}

		if err := deployCreate2Factory(sdkCtx, keepers); err != nil {
			return nil, fmt.Errorf("deployCreate2Factory: %w", err)
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}

func deployCreate2Factory(ctx sdk.Context, keepers *upgrades.AppKeepers) error {
	logger := ctx.Logger().With("migration", "evm-preinstalls")

	address := common.HexToAddress("0x4e59b44847b379578588920ca78fbf26c0b4956c")
	code := common.FromHex("0x7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf3")
	codeHash := crypto.Keccak256Hash(code).Bytes()

	if evmtypes.IsEmptyCodeHash(codeHash) {
		return fmt.Errorf("create2 factory has empty code hash")
	}

	if err := keepers.EVMKeeper.SetAccount(ctx, address, statedb.Account{
		Nonce:    0,
		Balance:  new(uint256.Int),
		CodeHash: codeHash,
	}); err != nil {
		return fmt.Errorf("SetAccount: %w", err)
	}

	keepers.EVMKeeper.SetCodeHash(ctx, address.Bytes(), codeHash)
	keepers.EVMKeeper.SetCode(ctx, codeHash, code)

	logger.Info("Create2 factory deployed", "address", address.Hex())
	return nil
}
