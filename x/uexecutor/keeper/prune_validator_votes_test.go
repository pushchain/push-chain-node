package keeper_test

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func setupPruneFixture(t *testing.T) *testFixture {
	t.Helper()
	f := SetupTest(t)

	f.mockEVMKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetCode(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	f.mockEVMKeeper.EXPECT().SetState(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	f.k.InitGenesis(f.ctx, &types.GenesisState{Params: types.DefaultParams()})
	return f
}

func TestPruneValidatorVotes_ChainMeta_RemovesSigner(t *testing.T) {
	f := setupPruneFixture(t)
	require := require.New(t)

	entry := types.ChainMeta{
		ObservedChainId: "eip155:1",
		Signers:         []string{"valA", "valB"},
		Prices:          []uint64{100, 200},
		ChainHeights:    []uint64{1000, 2000},
		StoredAts:       []uint64{10, 20},
		MedianIndex:     0,
	}
	require.NoError(f.k.ChainMetas.Set(f.ctx, "eip155:1", entry))

	f.k.PruneValidatorVotes(f.ctx, "valA")

	got, err := f.k.ChainMetas.Get(f.ctx, "eip155:1")
	require.NoError(err)
	require.Len(got.Signers, 1)
	require.Equal("valB", got.Signers[0])
	require.Equal(uint64(200), got.Prices[0])
	require.Equal(uint64(2000), got.ChainHeights[0])
}

func TestPruneValidatorVotes_ChainMeta_LastSigner_RemovesEntry(t *testing.T) {
	f := setupPruneFixture(t)
	require := require.New(t)

	entry := types.ChainMeta{
		ObservedChainId: "eip155:1",
		Signers:         []string{"valA"},
		Prices:          []uint64{100},
		ChainHeights:    []uint64{1000},
		StoredAts:       []uint64{10},
		MedianIndex:     0,
	}
	require.NoError(f.k.ChainMetas.Set(f.ctx, "eip155:1", entry))

	f.k.PruneValidatorVotes(f.ctx, "valA")

	has, err := f.k.ChainMetas.Has(f.ctx, "eip155:1")
	require.NoError(err)
	require.False(has)
}

func TestPruneValidatorVotes_NonExistentValidator_NoOp(t *testing.T) {
	f := setupPruneFixture(t)
	require := require.New(t)

	entry := types.ChainMeta{
		ObservedChainId: "eip155:1",
		Signers:         []string{"valA", "valB"},
		Prices:          []uint64{100, 200},
		ChainHeights:    []uint64{1000, 2000},
		StoredAts:       []uint64{10, 20},
		MedianIndex:     0,
	}
	require.NoError(f.k.ChainMetas.Set(f.ctx, "eip155:1", entry))

	f.k.PruneValidatorVotes(f.ctx, "valX")

	got, err := f.k.ChainMetas.Get(f.ctx, "eip155:1")
	require.NoError(err)
	require.Len(got.Signers, 2)
}

func TestPruneValidatorVotes_MultipleChains(t *testing.T) {
	f := setupPruneFixture(t)
	require := require.New(t)

	require.NoError(f.k.ChainMetas.Set(f.ctx, "eip155:1", types.ChainMeta{
		ObservedChainId: "eip155:1",
		Signers:         []string{"valA", "valB"},
		Prices:          []uint64{100, 200},
		ChainHeights:    []uint64{1000, 2000},
		StoredAts:       []uint64{10, 20},
	}))
	require.NoError(f.k.ChainMetas.Set(f.ctx, "eip155:137", types.ChainMeta{
		ObservedChainId: "eip155:137",
		Signers:         []string{"valB", "valC"},
		Prices:          []uint64{50, 75},
		ChainHeights:    []uint64{500, 700},
		StoredAts:       []uint64{10, 20},
	}))

	f.k.PruneValidatorVotes(f.ctx, "valB")

	got1, err := f.k.ChainMetas.Get(f.ctx, "eip155:1")
	require.NoError(err)
	require.Len(got1.Signers, 1)
	require.Equal("valA", got1.Signers[0])

	got137, err := f.k.ChainMetas.Get(f.ctx, "eip155:137")
	require.NoError(err)
	require.Len(got137.Signers, 1)
	require.Equal("valC", got137.Signers[0])
}

func TestPruneValidatorVotes_MedianRecomputed(t *testing.T) {
	f := setupPruneFixture(t)
	require := require.New(t)

	entry := types.ChainMeta{
		ObservedChainId: "eip155:1",
		Signers:         []string{"valA", "valB", "valC"},
		Prices:          []uint64{100, 200, 300},
		ChainHeights:    []uint64{1000, 2000, 3000},
		StoredAts:       []uint64{10, 20, 30},
		MedianIndex:     1,
	}
	require.NoError(f.k.ChainMetas.Set(f.ctx, "eip155:1", entry))

	f.k.PruneValidatorVotes(f.ctx, "valB")

	got, err := f.k.ChainMetas.Get(f.ctx, "eip155:1")
	require.NoError(err)
	require.Len(got.Prices, 2)
	require.Equal(uint64(300), got.Prices[got.MedianIndex])
}

// TestPruneValidatorVotes_MidWalkMutationSafe addresses audit finding
// F-2026-16943, which claimed that the Set/Remove calls PruneValidatorVotes
// performs during its internal Walk over ChainMetas would invalidate the
// iterator and cause entries to be skipped, leaving stale votes in state.
//
// The test seeds 10 entries that interleave both branches the production
// code takes:
//   - even i  → two signers [valTarget, valOther]  → triggers the Set branch
//   - odd  i  → one signer  [valTarget]            → triggers the Remove branch
//
// After a single call to PruneValidatorVotes, every entry is asserted
// individually. If the iterator skipped any entry, that entry would either
// still contain valTarget (Set branch) or still exist (Remove branch) and the
// corresponding assertion would fail with the exact key in the error.
func TestPruneValidatorVotes_MidWalkMutationSafe(t *testing.T) {
	f := setupPruneFixture(t)
	require := require.New(t)

	const n = 10

	for i := 1; i <= n; i++ {
		chainId := fmt.Sprintf("eip155:%d", i)
		var entry types.ChainMeta
		if i%2 == 0 {
			// Two signers → PruneValidatorVotes will Set (rewrite) this entry.
			entry = types.ChainMeta{
				ObservedChainId: chainId,
				Signers:         []string{"valTarget", "valOther"},
				Prices:          []uint64{uint64(i * 10), uint64(i * 20)},
				ChainHeights:    []uint64{uint64(i * 100), uint64(i * 200)},
				StoredAts:       []uint64{uint64(i), uint64(i + 1)},
			}
		} else {
			// Sole signer → PruneValidatorVotes will Remove this entry.
			entry = types.ChainMeta{
				ObservedChainId: chainId,
				Signers:         []string{"valTarget"},
				Prices:          []uint64{uint64(i * 10)},
				ChainHeights:    []uint64{uint64(i * 100)},
				StoredAts:       []uint64{uint64(i)},
			}
		}
		require.NoError(f.k.ChainMetas.Set(f.ctx, chainId, entry))
	}

	f.k.PruneValidatorVotes(f.ctx, "valTarget")

	// Per-key assertions: a skip on either branch surfaces here with the
	// exact failing key.
	for i := 1; i <= n; i++ {
		chainId := fmt.Sprintf("eip155:%d", i)
		if i%2 == 0 {
			got, err := f.k.ChainMetas.Get(f.ctx, chainId)
			require.NoError(err, "Set-branch key %s missing post-prune (skipped?)", chainId)
			require.Equal([]string{"valOther"}, got.Signers, "Set-branch key %s still contains valTarget — skipped during walk", chainId)
			require.Equal([]uint64{uint64(i * 20)}, got.Prices, "Set-branch key %s wrong retained price", chainId)
		} else {
			has, err := f.k.ChainMetas.Has(f.ctx, chainId)
			require.NoError(err)
			require.False(has, "Remove-branch key %s should be gone post-prune (skipped during walk)", chainId)
		}
	}

	// Total count must be exactly n/2 — catches both skips (count too high,
	// some Remove-branch entries survived) and over-deletions (count too low).
	count := 0
	require.NoError(f.k.ChainMetas.Walk(f.ctx, nil, func(string, types.ChainMeta) (bool, error) {
		count++
		return false, nil
	}))
	require.Equal(n/2, count, "expected exactly %d entries post-prune", n/2)
}
