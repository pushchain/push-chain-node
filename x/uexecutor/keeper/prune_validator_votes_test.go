package keeper_test

import (
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
