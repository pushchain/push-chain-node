package keeper

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	// sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	// state management
	Params                collections.Item[types.Params]
	CoreToUniversal       collections.Map[string, string] // Mapping: Core Validator Address → Universal Validator Address
	UniversalValidatorSet collections.KeySet[string]      // Set of all registered Universal Validator addresses

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
		CoreToUniversal: collections.NewMap(
			sb,
			types.CoreToUniversalKey,
			types.CoreToUniversalName,
			collections.StringKey,
			collections.StringValue,
		),

		UniversalValidatorSet: collections.NewKeySet(
			sb,
			types.CoreValidatorSetKey,
			types.CoreValidatorSetName,
			collections.StringKey,
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

func (k Keeper) SetCoreToUniversal(ctx context.Context, coreAddr, uvAddr string) error {
	return k.CoreToUniversal.Set(ctx, coreAddr, uvAddr)
}

func (k Keeper) HasCoreToUniversal(ctx context.Context, coreAddr string) (bool, error) {
	return k.CoreToUniversal.Has(ctx, coreAddr)
}

func (k Keeper) GetCoreToUniversal(ctx context.Context, coreAddr string) (string, bool, error) {
	value, err := k.CoreToUniversal.Get(ctx, coreAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (k Keeper) RemoveCoreToUniversalMappingByUniversalAddr(ctx context.Context, universalValidatorAddr string) error {
	var found bool

	err := k.CoreToUniversal.Walk(ctx, nil, func(coreAddr, mappedUniversal string) (bool, error) {
		if mappedUniversal == universalValidatorAddr {
			if err := k.CoreToUniversal.Remove(ctx, coreAddr); err != nil {
				return false, fmt.Errorf("failed to remove mapping for core addr %s: %w", coreAddr, err)
			}
			found = true
			return true, nil // Stop walking
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("error while walking CoreToUniversal map: %w", err)
	}
	if !found {
		return fmt.Errorf("no core validator maps to universal validator %s", universalValidatorAddr)
	}

	return nil
}

func (k Keeper) AddUniversalValidatorToSet(ctx context.Context, uvAddr string) error {
	return k.UniversalValidatorSet.Set(ctx, uvAddr)
}

func (k Keeper) HasUniversalValidatorInSet(ctx context.Context, uvAddr string) (bool, error) {
	return k.UniversalValidatorSet.Has(ctx, uvAddr)
}

func (k Keeper) RemoveUniversalValidatorFromSet(ctx context.Context, addr string) error {
	return k.UniversalValidatorSet.Remove(ctx, addr)
}

func (k Keeper) GetBlockHeight(ctx context.Context) (int64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.BlockHeight(), nil
}

// GetUniversalToCore finds the core validator address corresponding to a universal validator.
// Returns (coreAddr, true, nil) if found, ("", false, nil) if not found, or error if something goes wrong.
func (k Keeper) GetUniversalToCore(ctx context.Context, uvAddr string) (string, bool, error) {
	var coreAddr string
	var found bool

	err := k.CoreToUniversal.Walk(ctx, nil, func(cAddr, mappedUV string) (bool, error) {
		if mappedUV == uvAddr {
			coreAddr = cAddr
			found = true
			return true, nil // stop walking early
		}
		return false, nil
	})
	if err != nil {
		return "", false, fmt.Errorf("error walking CoreToUniversal map: %w", err)
	}

	if !found {
		return "", false, nil
	}
	return coreAddr, true, nil
}

// Returns the universal validator set
func (k Keeper) GetUniversalValidatorSet(ctx context.Context) ([]string, error) {
	var validators []string

	err := k.UniversalValidatorSet.Walk(ctx, nil, func(key string) (stop bool, err error) {
		validators = append(validators, key)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return validators, nil
}
