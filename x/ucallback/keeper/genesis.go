package keeper

import (
	"context"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

// InitGenesis initializes the module's state from a genesis state.
//
// Only UniversalReads is imported. The PendingByExpiry and ReadsByTxHash indexes
// are rebuilt here from the records themselves, via SetUniversalRead — importing
// them separately would allow a genesis file to carry indexes that disagree with
// the records they point at.
func (k *Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if err := data.Params.Validate(); err != nil {
		return err
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return err
	}

	for _, entry := range data.UniversalReads {
		if err := k.SetUniversalRead(ctx, entry.Value); err != nil {
			return err
		}
	}

	// Only written when non-zero so a fresh genesis leaves the item unset and
	// GetModuleAccountNonce's default applies.
	if data.ModuleAccountNonce > 0 {
		if err := k.ModuleAccountNonce.Set(ctx, data.ModuleAccountNonce); err != nil {
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

	reads := []types.UniversalReadEntry{}
	if err := k.UniversalReads.Walk(ctx, nil, func(key string, value types.UniversalRead) (bool, error) {
		reads = append(reads, types.UniversalReadEntry{Key: key, Value: value})
		return false, nil
	}); err != nil {
		panic(err)
	}

	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:             params,
		UniversalReads:     reads,
		ModuleAccountNonce: nonce,
	}
}
