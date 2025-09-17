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

	logger log.Logger

	// state management
	Params                collections.Item[types.Params]
	UniversalValidatorSet collections.KeySet[sdk.ValAddress] // Set of all registered Universal Validator addresses

	// Ballots management
	Ballots            collections.Map[string, types.Ballot] // stores the actual ballot object, keyed by ballot ID
	ActiveBallotIDs    collections.KeySet[string]            // set of ballot IDs currently collecting votes
	ExpiredBallotIDs   collections.KeySet[string]            // set of ballot IDs that have expired (not yet pruned)
	FinalizedBallotIDs collections.KeySet[string]            // set of ballot IDs that are PASSED or REJECTED

	stakingKeeper  types.StakingKeeper
	slashingKeeper types.SlashingKeeper

	authority string
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	stakingKeeper types.StakingKeeper,
	slashingKeeper types.SlashingKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	k := Keeper{
		cdc:    cdc,
		logger: logger,

		Params: collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),

		UniversalValidatorSet: collections.NewKeySet(
			sb,
			types.CoreValidatorSetKey,
			types.CoreValidatorSetName,
			sdk.ValAddressKey,
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

		authority:      authority,
		stakingKeeper:  stakingKeeper,
		slashingKeeper: slashingKeeper,
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

	return k.Params.Set(ctx, data.Params)
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params: params,
	}
}

func (k Keeper) AddUniversalValidatorToSet(ctx context.Context, uvAddr sdk.ValAddress) error {
	return k.UniversalValidatorSet.Set(ctx, uvAddr)
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

// Returns the universal validator set
func (k Keeper) GetUniversalValidatorSet(ctx context.Context) ([]sdk.ValAddress, error) {
	var validators []sdk.ValAddress

	err := k.UniversalValidatorSet.Walk(ctx, nil, func(key sdk.ValAddress) (stop bool, err error) {
		validators = append(validators, key)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return validators, nil
}
