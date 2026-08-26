package keeper

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger        log.Logger
	schemaBuilder *collections.SchemaBuilder

	// state management
	storeService      storetypes.KVStoreService
	Params            collections.Item[types.Params]
	authority         string
	evmKeeper         types.EVMKeeper
	feemarketKeeper   types.FeeMarketKeeper
	bankKeeper        types.BankKeeper
	accountKeeper     types.AccountKeeper
	uregistryKeeper  types.UregistryKeeper
	uvalidatorKeeper types.UValidatorKeeper

	// PendingInbounds tracks in-flight inbounds with full per-variant
	// audit trail (which validators voted what payload, terminal status
	// per variant). Created on first vote (RecordInboundVote), removed
	// when all variants reach a terminal state (BallotHooks impl).
	// See plan-pending-inbound-cleanup.md.
	PendingInbounds collections.Map[string, types.PendingInboundEntry]

	// ExpiredInbounds preserves the per-variant audit trail of inbounds
	// whose ballots all reached EXPIRED/REJECTED without producing a UTX.
	// Consumed by the future escape-hatch refund flow.
	ExpiredInbounds collections.Map[string, types.ExpiredInboundEntry]

	// UniversalTx collection
	UniversalTx collections.Map[string, types.UniversalTx]

	// Module account manual nonce
	ModuleAccountNonce collections.Item[uint64]

	// GasPrices — deprecated, replaced by ChainMetas. Kept for genesis backward compat.
	GasPrices collections.Map[string, types.GasPrice]

	// ChainMetas collection stores aggregated chain metadata (gas price + block height) for each chain
	ChainMetas collections.Map[string, types.ChainMeta]

	// PendingOutbounds is a secondary index of outbounds with PENDING status.
	// Key: outbound ID -> Value: PendingOutboundEntry
	PendingOutbounds collections.Map[string, types.PendingOutboundEntry]
}

// NewKeeper creates a new Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	evmKeeper types.EVMKeeper,
	feemarketKeeper types.FeeMarketKeeper,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	uregistryKeeper types.UregistryKeeper,
	uvalidatorKeeper types.UValidatorKeeper,
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
		storeService:  storeService,
		Params:        collections.NewItem(sb, types.ParamsKey, types.ParamsName, codec.CollValue[types.Params](cdc)),

		authority:         authority,
		evmKeeper:         evmKeeper,
		feemarketKeeper:   feemarketKeeper,
		bankKeeper:        bankKeeper,
		accountKeeper:     accountKeeper,
		uregistryKeeper:  uregistryKeeper,
		uvalidatorKeeper: uvalidatorKeeper,

		PendingInbounds: collections.NewMap(
			sb,
			types.PendingInboundsKey,
			types.PendingInboundsName,
			collections.StringKey,
			codec.CollValue[types.PendingInboundEntry](cdc),
		),

		ExpiredInbounds: collections.NewMap(
			sb,
			types.ExpiredInboundsKey,
			types.ExpiredInboundsName,
			collections.StringKey,
			codec.CollValue[types.ExpiredInboundEntry](cdc),
		),

		UniversalTx: collections.NewMap(
			sb,
			types.UniversalTxKey,
			types.UniversalTxName,
			collections.StringKey,
			codec.CollValue[types.UniversalTx](cdc),
		),

		ModuleAccountNonce: collections.NewItem(
			sb,
			types.ModuleAccountNonceKey,
			types.ModuleAccountNonceName,
			collections.Uint64Value,
		),

		GasPrices: collections.NewMap(
			sb,
			types.GasPricesKey,
			types.GasPricesName,
			collections.StringKey,
			codec.CollValue[types.GasPrice](cdc),
		),

		ChainMetas: collections.NewMap(
			sb,
			types.ChainMetaKey,
			types.ChainMetasName,
			collections.StringKey,
			codec.CollValue[types.ChainMeta](cdc),
		),

		PendingOutbounds: collections.NewMap(
			sb,
			types.PendingOutboundsKey,
			types.PendingOutboundsName,
			collections.StringKey,
			codec.CollValue[types.PendingOutboundEntry](cdc),
		),
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

	// Only deploy factory contracts on fresh genesis, not on import from export.
	// Re-deploying on import would overwrite existing EVM state or cause nonce collisions.
	if !data.Exported {
		deployFactoryEA(ctx, k.evmKeeper)
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return err
	}

	// Restore PendingInbounds (variant-aware Map at the new prefix).
	for _, entry := range data.PendingInbounds {
		if err := k.PendingInbounds.Set(ctx, entry.UtxKey, entry); err != nil {
			return err
		}
	}

	// Restore ExpiredInbounds.
	for _, entry := range data.ExpiredInbounds {
		if err := k.ExpiredInbounds.Set(ctx, entry.UtxKey, entry); err != nil {
			return err
		}
	}

	// Restore UniversalTx
	for _, entry := range data.UniversalTxs {
		if err := k.UniversalTx.Set(ctx, entry.Key, entry.Value); err != nil {
			return err
		}
	}

	// Restore ModuleAccountNonce
	if data.ModuleAccountNonce > 0 {
		if err := k.ModuleAccountNonce.Set(ctx, data.ModuleAccountNonce); err != nil {
			return err
		}
	}

	// Restore GasPrices
	for _, entry := range data.GasPrices {
		if err := k.GasPrices.Set(ctx, entry.Key, entry.Value); err != nil {
			return err
		}
	}

	// Restore ChainMetas
	for _, entry := range data.ChainMetas {
		if err := k.ChainMetas.Set(ctx, entry.Key, entry.Value); err != nil {
			return err
		}
	}

	// Restore PendingOutbounds
	for _, entry := range data.PendingOutbounds {
		if err := k.PendingOutbounds.Set(ctx, entry.OutboundId, entry); err != nil {
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

	// Export PendingInbounds (variant-aware Map).
	var pendingInbounds []types.PendingInboundEntry
	err = k.PendingInbounds.Walk(ctx, nil, func(_ string, value types.PendingInboundEntry) (bool, error) {
		pendingInbounds = append(pendingInbounds, value)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export ExpiredInbounds.
	var expiredInbounds []types.ExpiredInboundEntry
	err = k.ExpiredInbounds.Walk(ctx, nil, func(_ string, value types.ExpiredInboundEntry) (bool, error) {
		expiredInbounds = append(expiredInbounds, value)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export UniversalTx
	var universalTxs []types.UniversalTxEntry
	err = k.UniversalTx.Walk(ctx, nil, func(key string, value types.UniversalTx) (bool, error) {
		universalTxs = append(universalTxs, types.UniversalTxEntry{Key: key, Value: value})
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export ModuleAccountNonce
	moduleAccountNonce, err := k.ModuleAccountNonce.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			panic(err)
		}
		moduleAccountNonce = 0
	}

	// Export GasPrices
	var gasPrices []types.GasPriceEntry
	err = k.GasPrices.Walk(ctx, nil, func(key string, value types.GasPrice) (bool, error) {
		gasPrices = append(gasPrices, types.GasPriceEntry{Key: key, Value: value})
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export ChainMetas
	var chainMetas []types.ChainMetaEntry
	err = k.ChainMetas.Walk(ctx, nil, func(key string, value types.ChainMeta) (bool, error) {
		chainMetas = append(chainMetas, types.ChainMetaEntry{Key: key, Value: value})
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	// Export PendingOutbounds
	var pendingOutbounds []types.PendingOutboundEntry
	err = k.PendingOutbounds.Walk(ctx, nil, func(key string, value types.PendingOutboundEntry) (bool, error) {
		pendingOutbounds = append(pendingOutbounds, value)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:             params,
		PendingInbounds:    pendingInbounds,
		ExpiredInbounds:    expiredInbounds,
		UniversalTxs:       universalTxs,
		ModuleAccountNonce: moduleAccountNonce,
		GasPrices:          gasPrices,
		ChainMetas:         chainMetas,
		PendingOutbounds:   pendingOutbounds,
		Exported:           true,
	}
}

func (k *Keeper) GetUeModuleAddress(ctx context.Context) (common.Address, string) {
	ueModuleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName) // "ue"
	ueModuleAddr := ueModuleAcc.GetAddress()
	var ethSenderUEAddr common.Address
	copy(ethSenderUEAddr[:], ueModuleAddr.Bytes())

	return ethSenderUEAddr, ethSenderUEAddr.Hex()
}

func (k Keeper) SchemaBuilder() *collections.SchemaBuilder {
	return k.schemaBuilder
}

// GetModuleAccountNonce returns the current module account nonce.
// If not set yet, it safely defaults to 0.
func (k Keeper) GetModuleAccountNonce(ctx sdk.Context) (uint64, error) {
	nonce, err := k.ModuleAccountNonce.Get(ctx)
	if err != nil {
		// If the key is missing, return 0 instead of error
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return nonce, nil
}

// SetModuleAccountNonce allows explicitly setting the nonce (optional, for migration or testing).
// It keeps the module account's EVM nonce in step, so the two can never diverge —
// see nextModuleSenderNonce for why that matters.
func (k Keeper) SetModuleAccountNonce(ctx sdk.Context, nonce uint64) error {
	if err := k.ModuleAccountNonce.Set(ctx, nonce); err != nil {
		return err
	}

	acc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	if acc == nil {
		return fmt.Errorf("module account %s not found", types.ModuleName)
	}
	if acc.GetSequence() == nonce {
		return nil
	}
	if err := acc.SetSequence(nonce); err != nil {
		return err
	}
	k.accountKeeper.SetAccount(ctx, acc)

	return nil
}

// nextModuleSenderNonce picks the nonce for the module's next DerivedEVMCall.
//
// F-2026-18189. The module account's EVM nonce is the source of truth, but x/vm
// will not maintain it: ApplyMessageWithConfig advances a sender's nonce only in
// its contractCreation branch, and every call the module makes is a plain CALL.
// So the module maintains it itself (see burnModuleSenderNonce), and reads it
// back here so that a nonce the EVM *did* advance — a CREATE from the module, or
// a chain upgraded from a build that left the account nonce behind — is picked up
// instead of being re-issued.
func (k Keeper) nextModuleSenderNonce(ctx sdk.Context, moduleAddr common.Address) (uint64, error) {
	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return 0, err
	}
	if evmNonce := k.evmKeeper.GetNonce(ctx, moduleAddr); evmNonce > nonce {
		nonce = evmNonce
	}
	return nonce, nil
}

// burnModuleSenderNonce consumes the nonce handed out by nextModuleSenderNonce.
//
// The advance is unconditional: it happens whether the call committed, reverted,
// or never reached the EVM at all. That is deliberate. The derived tx hash is
// ethtypes.NewTx(&DynamicFeeTx{Nonce, GasFeeCap, GasTipCap, Gas, To, Value,
// Data}).Hash(), so the nonce is the only thing separating two byte-identical
// module calls; a failed attempt that gave its nonce back would let the retry
// reproduce a hash already emitted in this block. Because the counter and the
// account nonce move together, advancing on failure can no longer desync them —
// which is what F-2026-18189 reported.
func (k Keeper) burnModuleSenderNonce(ctx sdk.Context, moduleAddr common.Address, nonce uint64) error {
	next := nonce + 1
	if evmNonce := k.evmKeeper.GetNonce(ctx, moduleAddr); evmNonce > next {
		next = evmNonce
	}
	return k.SetModuleAccountNonce(ctx, next)
}
