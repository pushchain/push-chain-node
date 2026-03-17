package chainmeta

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/pushchain/push-chain-node/app/upgrades"
)

const UpgradeName = "chain-meta"

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

		// ── Feature 1 ───────────────────────────────────────────────────────────
		// funds_and_payload / payload routes no longer revert the inbound when the
		// UEA is not yet deployed.  The call simply skips the payload execution and
		// marks the tx as PC_EXECUTED_FAILED instead of reverting funds.
		// No state migration required – purely behavioural.
		logger.Info("Feature: funds_and_payload / payload txs no longer revert when UEA is not deployed")

		// ── Feature 2 ───────────────────────────────────────────────────────────
		// Chain inbound / outbound can now be toggled via ChainConfig.Enabled
		// (IsInboundEnabled / IsOutboundEnabled).  VoteInbound and
		// BuildOutboundsFromReceipt both gate on these flags.
		// The Enabled field is already part of ChainConfig in uregistry – no
		// additional state migration is required.
		logger.Info("Feature: per-chain inbound/outbound enable/disable checks are now enforced")

		// ── Feature 3 ───────────────────────────────────────────────────────────
		// New MsgVoteChainMeta message + ChainMeta store.
		// uexecutor module consensus version bumped 4 → 5.
		// RunMigrations below triggers MigrateGasPricesToChainMeta which seeds
		// ChainMetas from existing GasPrices entries (preserving signers, prices,
		// block heights and median index).
		logger.Info("Feature: MsgVoteChainMeta added; migrating GasPrices → ChainMetas store (v4 → v5)")

		// ── Feature 4 ───────────────────────────────────────────────────────────
		// GAS and GAS_AND_PAYLOAD inbound routes now call the Uniswap V3 QuoterV2
		// contract to obtain an on-chain swap quote and pass minPCOut (quote × 95%)
		// to CallPRC20DepositAutoSwap, replacing the previous 0-slippage call.
		// No state migration required.
		logger.Info("Feature: Uniswap V3 QuoterV2 used for minPCOut (5% slippage) on GAS / GAS_AND_PAYLOAD routes")

		// ── Feature 5 ───────────────────────────────────────────────────────────
		// OutboundTx proto gains gas_price (field 16) and gas_fee (field 17).
		// These are populated from the decoded UniversalTxOutbound EVM event.
		// OutboundCreatedEvent and the on-chain event attributes also include
		// gas_price and gas_fee.  Existing stored OutboundTx records default
		// the new fields to "" which is safe – no migration required.
		logger.Info("Feature: gas_price and gas_fee fields added to OutboundTx proto and outbound events")

		// ── Feature 6 ───────────────────────────────────────────────────────────
		// On a successful outbound observation, if gas_fee_used < gas_fee the
		// excess is refunded to the sender (or fund_recipient) via
		// UniversalCore.refundUnusedGas.  A swap (gasToken → PC native) is
		// attempted first; on failure the raw PRC20 is deposited directly.
		// The result is persisted in OutboundTx.pc_refund_execution.
		// No state migration required.
		logger.Info("Feature: excess gas fee refund executed on successful outbound vote finalisation")

		// ── Feature 7 ───────────────────────────────────────────────────────────
		// OutboundTx proto gains gas_token (field 20) which records the PRC20
		// address of the token used to pay the relayer fee.  Populated from the
		// decoded UniversalTxOutbound EVM event.  Existing records default to "".
		// No state migration required.
		logger.Info("Feature: gas_token field added to OutboundTx proto")

		// ── State migration ──────────────────────────────────────────────────────
		// RunMigrations triggers uexecutor v4 → v5 which calls
		// MigrateGasPricesToChainMeta via the registered migration handler.
		versionMap, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			logger.Error("RunMigrations failed", "error", err)
			return nil, err
		}

		logger.Info("Upgrade complete", "upgrade", UpgradeName)
		return versionMap, nil
	}
}
