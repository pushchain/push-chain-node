package keeper

import (
	"context"
	"errors"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/ethereum/go-ethereum/common"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	// state management
	Schema collections.Schema
	Params collections.Item[types.Params]

	// UniversalReads is the canonical record for every read request, keyed by
	// requestId. Indexes over it are added alongside the lookups they serve, and
	// are always derived — never a source of truth.
	UniversalReads collections.Map[string, types.UniversalRead]

	// PendingByExpiry holds (expiryHeight, requestId) for reads that have not
	// settled — the module's in-flight set. Ordered composite key so the sweeper
	// can range-scan by height.
	PendingByExpiry collections.KeySet[collections.Pair[uint64, string]]

	// ReadsByTxHash holds (pushTxHash, requestId) so every read emitted by one
	// Push transaction can be listed together.
	ReadsByTxHash collections.KeySet[collections.Pair[string, string]]

	// ModuleAccountNonce is the EVM nonce of this module's account. x/ucallback
	// owns it because it owns the account — UniversalCallback admits only this
	// module's address, so nothing else ever sends from it.
	ModuleAccountNonce collections.Item[uint64]

	uvalidatorKeeper types.UValidatorKeeper
	evmKeeper        types.EVMKeeper
	accountKeeper    types.AccountKeeper

	authority string
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	uvalidatorKeeper types.UValidatorKeeper,
	evmKeeper types.EVMKeeper,
	accountKeeper types.AccountKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	if authority == "" {
		authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	}

	k := Keeper{
		cdc:    cdc,
		logger: logger,

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		UniversalReads: collections.NewMap(
			sb, types.UniversalReadsKey, "universal_reads",
			collections.StringKey, codec.CollValue[types.UniversalRead](cdc),
		),
		PendingByExpiry: collections.NewKeySet(
			sb, types.PendingByExpiryKey, "pending_by_expiry",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
		),
		ReadsByTxHash: collections.NewKeySet(
			sb, types.ReadsByTxHashKey, "reads_by_tx_hash",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),

		ModuleAccountNonce: collections.NewItem(
			sb, types.ModuleAccountNonceKey, types.ModuleAccountNonceName,
			collections.Uint64Value,
		),

		uvalidatorKeeper: uvalidatorKeeper,
		evmKeeper:        evmKeeper,
		accountKeeper:    accountKeeper,
		authority:        authority,
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	k.Schema = schema

	return k
}

func (k Keeper) Logger() log.Logger {
	return k.logger
}

// GetModuleAddress returns the x/ucallback module account's EVM address.
//
// This is the address UniversalCallback's access control admits; the contract
// rejects a call from anything else. Derived from the module name, so it is fixed
// for the life of the chain: 0x07a0258D367A4A4cd9d6E4b7eEE8E7eF491CC519.
func (k Keeper) GetModuleAddress(ctx context.Context) (common.Address, string) {
	acc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	var evmAddr common.Address
	copy(evmAddr[:], acc.GetAddress().Bytes())
	return evmAddr, evmAddr.Hex()
}

// GetModuleAccountNonce returns the module account's current EVM nonce, defaulting
// to 0 before the first call is made.
func (k Keeper) GetModuleAccountNonce(ctx context.Context) (uint64, error) {
	nonce, err := k.ModuleAccountNonce.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return nonce, nil
}

// IncrementModuleAccountNonce advances the nonce and returns the new value.
func (k Keeper) IncrementModuleAccountNonce(ctx context.Context) (uint64, error) {
	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return 0, err
	}
	next := nonce + 1
	if err := k.ModuleAccountNonce.Set(ctx, next); err != nil {
		return 0, err
	}
	return next, nil
}
