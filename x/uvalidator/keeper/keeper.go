package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger        log.Logger
	schemaBuilder *collections.SchemaBuilder

	// state management
	Params collections.Item[types.Params]
	// TODO: write the migration from sdk.ValAddress to current structure
	UniversalValidatorSet collections.Map[sdk.ValAddress, types.UniversalValidator] // Set of all registered Universal Validator addresses

	// Ballots management
	Ballots            collections.Map[string, types.Ballot] // stores the actual ballot object, keyed by ballot ID
	ActiveBallotIDs    collections.KeySet[string]            // set of ballot IDs currently collecting votes
	ExpiredBallotIDs   collections.KeySet[string]            // set of ballot IDs that have expired (not yet pruned)
	FinalizedBallotIDs collections.KeySet[string]            // set of ballot IDs that are PASSED or REJECTED

	StakingKeeper      types.StakingKeeper
	SlashingKeeper     types.SlashingKeeper
	UtssKeeper         types.UtssKeeper
	BankKeeper         types.BankKeeper
	AuthKeeper         types.AccountKeeper
	DistributionKeeper types.DistributionKeeper

	authority   string
	hooks       types.UValidatorHooks
	ballotHooks types.BallotHooks
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	bankKeeper types.BankKeeper,
	authKeeper types.AccountKeeper,
	distributionKeeper types.DistributionKeeper,
	stakingKeeper types.StakingKeeper,
	slashingKeeper types.SlashingKeeper,
	utssKeeper types.UtssKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	k := Keeper{
		cdc:           cdc,
		logger:        logger,
		schemaBuilder: sb,

		Params: collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),

		UniversalValidatorSet: collections.NewMap(
			sb,
			types.CoreValidatorSetKey,
			types.CoreValidatorSetName,
			sdk.ValAddressKey,
			codec.CollValue[types.UniversalValidator](cdc),
		),

		// Ballot collections
		Ballots: collections.NewMap(
			sb, types.BallotsKey, types.BallotsName,
			collections.StringKey, codec.CollValue[types.Ballot](cdc),
		),
		ActiveBallotIDs: collections.NewKeySet(
			sb, types.ActiveBallotIDsKey, types.ActiveBallotIDsName,
			collections.StringKey,
		),
		ExpiredBallotIDs: collections.NewKeySet(
			sb, types.ExpiredBallotIDsKey, types.ExpiredBallotIDsName,
			collections.StringKey,
		),
		FinalizedBallotIDs: collections.NewKeySet(
			sb, types.FinalizedBallotIDsKey, types.FinalizedBallotIDsName,
			collections.StringKey,
		),

		authority:          authority,
		StakingKeeper:      stakingKeeper,
		SlashingKeeper:     slashingKeeper,
		UtssKeeper:         utssKeeper,
		BankKeeper:         bankKeeper,
		AuthKeeper:         authKeeper,
		DistributionKeeper: distributionKeeper,
	}

	return k
}

func (k Keeper) Logger() log.Logger {
	return k.logger
}

// InitGenesis initializes the module's state from a genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {

	if err := data.Params.ValidateBasic(); err != nil {
		return err
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return err
	}

	// Restore UniversalValidatorSet
	for _, entry := range data.UniversalValidators {
		valAddr, err := sdk.ValAddressFromBech32(entry.Key)
		if err != nil {
			return err
		}
		if err := k.UniversalValidatorSet.Set(ctx, valAddr, entry.Value); err != nil {
			return err
		}
	}

	// Restore Ballots
	for _, ballot := range data.Ballots {
		if err := k.Ballots.Set(ctx, ballot.Id, ballot); err != nil {
			return err
		}
	}

	// Restore ActiveBallotIDs
	for _, id := range data.ActiveBallotIds {
		if err := k.ActiveBallotIDs.Set(ctx, id); err != nil {
			return err
		}
	}

	// Restore ExpiredBallotIDs
	for _, id := range data.ExpiredBallotIds {
		if err := k.ExpiredBallotIDs.Set(ctx, id); err != nil {
			return err
		}
	}

	// Restore FinalizedBallotIDs
	for _, id := range data.FinalizedBallotIds {
		if err := k.FinalizedBallotIDs.Set(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	// Export UniversalValidatorSet
	var universalValidators []types.UniversalValidatorEntry
	err = k.UniversalValidatorSet.Walk(ctx, nil, func(key sdk.ValAddress, value types.UniversalValidator) (bool, error) {
		universalValidators = append(universalValidators, types.UniversalValidatorEntry{
			Key:   key.String(),
			Value: value,
		})
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export Ballots
	var ballots []types.Ballot
	err = k.Ballots.Walk(ctx, nil, func(key string, value types.Ballot) (bool, error) {
		ballots = append(ballots, value)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export ActiveBallotIDs
	var activeBallotIDs []string
	err = k.ActiveBallotIDs.Walk(ctx, nil, func(key string) (bool, error) {
		activeBallotIDs = append(activeBallotIDs, key)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export ExpiredBallotIDs
	var expiredBallotIDs []string
	err = k.ExpiredBallotIDs.Walk(ctx, nil, func(key string) (bool, error) {
		expiredBallotIDs = append(expiredBallotIDs, key)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export FinalizedBallotIDs
	var finalizedBallotIDs []string
	err = k.FinalizedBallotIDs.Walk(ctx, nil, func(key string) (bool, error) {
		finalizedBallotIDs = append(finalizedBallotIDs, key)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:              params,
		UniversalValidators: universalValidators,
		Ballots:             ballots,
		ActiveBallotIds:     activeBallotIDs,
		ExpiredBallotIds:    expiredBallotIDs,
		FinalizedBallotIds:  finalizedBallotIDs,
	}
}

func (k Keeper) HasUniversalValidatorInSet(ctx context.Context, uvAddr sdk.ValAddress) (bool, error) {
	return k.UniversalValidatorSet.Has(ctx, uvAddr)
}

func (k Keeper) RemoveUniversalValidatorFromSet(ctx context.Context, addr sdk.ValAddress) error {
	return k.UniversalValidatorSet.Remove(ctx, addr)
}

func (k Keeper) GetBlockHeight(ctx context.Context) (int64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.BlockHeight(), nil
}

// Hooks bundles every external-module callback surface that x/uvalidator
// exposes. Each field is independently optional — nil means "don't
// register that surface."
//
//   - Validator: validator-lifecycle callbacks (AfterValidatorAdded,
//     AfterValidatorRemoved, AfterValidatorStatusChanged). Today
//     consumed by x/utss + x/uexecutor (typically wrapped in a
//     MultiUValidatorHooks for fan-out).
//
//   - Ballot: ballot-lifecycle terminal callbacks (AfterBallotTerminal).
//     Today consumed by x/uexecutor only — for the F-2026-16642
//     per-variant audit-trail cleanup. If a future module needs to
//     react to ballot terminals, introduce a MultiBallotHooks wrapper
//     following the MultiUValidatorHooks pattern.
type Hooks struct {
	Validator types.UValidatorHooks
	Ballot    types.BallotHooks
}

// SetHooks registers the external-module hook implementations on this
// keeper. Each Hooks field is independently optional; nil means the
// corresponding surface is not registered. Calling SetHooks twice
// panics — all hook wiring must happen in a single registration call
// (typically inside app.go after every keeper has been constructed).
func (k *Keeper) SetHooks(h Hooks) *Keeper {
	if k.hooks != nil || k.ballotHooks != nil {
		panic("cannot set uvalidator hooks twice")
	}
	k.hooks = h.Validator
	k.ballotHooks = h.Ballot
	return k
}

func (k Keeper) SchemaBuilder() *collections.SchemaBuilder {
	return k.schemaBuilder
}
