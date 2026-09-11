package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"github.com/golang/mock/gomock"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

const (
	registeredChainID   = "eip155:11155111"
	unregisteredChainID = "eip155:999999999"
)

// setupChainMetaFixture builds the keeper fixture with a deterministic block
// time so storedAt/staleness arithmetic is stable.
func setupChainMetaFixture(t *testing.T) *testFixture {
	t.Helper()
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	return f
}

// registerChain makes the uregistry mock answer GetChainConfig for chain.
func registerChain(f *testFixture, chain string) {
	f.mockUregistryKeeper.EXPECT().
		GetChainConfig(gomock.Any(), chain).
		Return(uregistrytypes.ChainConfig{
			Chain:  chain,
			VmType: uregistrytypes.VmType_EVM,
			Enabled: &uregistrytypes.ChainEnabled{
				IsInboundEnabled:  true,
				IsOutboundEnabled: true,
			},
		}, nil).
		AnyTimes()
}

// unregisterChain makes the uregistry mock report chain as absent, exactly as
// the real keeper does (collections.ErrNotFound out of ChainConfigs.Get).
func unregisterChain(f *testFixture, chain string) {
	f.mockUregistryKeeper.EXPECT().
		GetChainConfig(gomock.Any(), chain).
		Return(uregistrytypes.ChainConfig{}, collections.ErrNotFound).
		AnyTimes()
}

// countChainMetas returns every key currently present in the ChainMetas map.
func countChainMetas(t *testing.T, f *testFixture) []string {
	t.Helper()
	var keys []string
	require.NoError(t, f.k.ChainMetas.Walk(f.ctx, nil, func(chainID string, _ types.ChainMeta) (bool, error) {
		keys = append(keys, chainID)
		return false, nil
	}))
	return keys
}

// F-2026-18803: a vote for a chain that is not in x/uregistry must be rejected
// *before* the keeper writes anything. The finding is not "an error is missing"
// — it is that the GetChainMeta miss creates the row on the cold-start path, so
// the store assertion is the one that matters.
func TestVoteChainMeta_UnregisteredChain_RejectedAndStoreUnchanged(t *testing.T) {
	f := setupChainMetaFixture(t)
	require := require.New(t)

	unregisterChain(f, unregisteredChainID)

	before := countChainMetas(t, f)
	require.Empty(before)

	err := f.k.VoteChainMeta(f.ctx, sdk.ValAddress(f.addrs[0]), unregisteredChainID, 100_000_000_000, 12345)

	// Store first, deliberately: the finding is the *row being written*, not a
	// missing error. Removing the registry gate must break this assertion.
	has, hasErr := f.k.ChainMetas.Has(f.ctx, unregisteredChainID)
	require.NoError(hasErr)
	require.False(has, "unregistered chain must not create a ChainMetas row")
	require.Equal(before, countChainMetas(t, f), "ChainMetas must be unchanged")

	require.Error(err)
	require.Contains(err.Error(), "is not registered")
}

// An attacker-shaped id (long, arbitrary) must not become an IAVL key either.
func TestVoteChainMeta_UnregisteredLongChainId_WritesNoKey(t *testing.T) {
	f := setupChainMetaFixture(t)
	require := require.New(t)

	longID := "eip155:"
	for i := 0; i < 200; i++ {
		longID += "9"
	}
	unregisterChain(f, longID)

	err := f.k.VoteChainMeta(f.ctx, sdk.ValAddress(f.addrs[0]), longID, 1, 1)

	require.Empty(countChainMetas(t, f), "no ChainMetas key may be minted for an unregistered id")
	require.Error(err)
}

// A registered chain keeps working: the first vote is recorded and creates the
// row (this is required — bootstrap quorum can never be reached otherwise).
func TestVoteChainMeta_RegisteredChain_CreatesRow(t *testing.T) {
	f := setupChainMetaFixture(t)
	require := require.New(t)

	registerChain(f, registeredChainID)

	valAddr := sdk.ValAddress(f.addrs[0])
	require.NoError(f.k.VoteChainMeta(f.ctx, valAddr, registeredChainID, 100_000_000_000, 12345))

	stored, found, err := f.k.GetChainMeta(f.ctx, registeredChainID)
	require.NoError(err)
	require.True(found)
	require.Equal(registeredChainID, stored.ObservedChainId)
	require.Equal([]string{valAddr.String()}, stored.Signers)
	require.Equal([]uint64{100_000_000_000}, stored.Prices)
	require.Equal([]uint64{12345}, stored.ChainHeights)
	require.Equal([]uint64{uint64(f.ctx.BlockTime().Unix())}, stored.StoredAts)
	// Below the bootstrap quorum the oracle is not written.
	require.Equal(uint64(0), stored.LastAppliedChainHeight)

	require.Equal([]string{registeredChainID}, countChainMetas(t, f))
}

// Existing pre-bootstrap accumulation behaviour is unchanged for a registered
// chain: votes below chainMetaMinVotesForFirstWrite are stored, not applied.
func TestVoteChainMeta_RegisteredChain_BootstrapAccumulationUnchanged(t *testing.T) {
	f := setupChainMetaFixture(t)
	require := require.New(t)

	registerChain(f, registeredChainID)

	val0 := sdk.ValAddress(f.addrs[0])
	val1 := sdk.ValAddress(f.addrs[1])

	require.NoError(f.k.VoteChainMeta(f.ctx, val0, registeredChainID, 100_000_000_000, 12345))
	require.NoError(f.k.VoteChainMeta(f.ctx, val1, registeredChainID, 200_000_000_000, 12346))

	stored, found, err := f.k.GetChainMeta(f.ctx, registeredChainID)
	require.NoError(err)
	require.True(found)
	require.Len(stored.Signers, 2)
	require.Equal([]uint64{100_000_000_000, 200_000_000_000}, stored.Prices)
	require.Equal(uint64(0), stored.LastAppliedChainHeight, "two votes must not bootstrap the oracle")

	// A re-vote from the same validator still updates in place, not appends.
	require.NoError(f.k.VoteChainMeta(f.ctx, val0, registeredChainID, 400_000_000_000, 12350))
	stored, _, err = f.k.GetChainMeta(f.ctx, registeredChainID)
	require.NoError(err)
	require.Len(stored.Signers, 2)
	require.Equal(uint64(400_000_000_000), stored.Prices[0])
	require.Equal(uint64(12350), stored.ChainHeights[0])
}

// Registering one chain must not implicitly admit its neighbours.
func TestVoteChainMeta_OnlyRegisteredChainAdmitted(t *testing.T) {
	f := setupChainMetaFixture(t)
	require := require.New(t)

	registerChain(f, registeredChainID)
	unregisterChain(f, unregisteredChainID)

	valAddr := sdk.ValAddress(f.addrs[0])
	require.NoError(f.k.VoteChainMeta(f.ctx, valAddr, registeredChainID, 1, 1))
	require.Error(f.k.VoteChainMeta(f.ctx, valAddr, unregisteredChainID, 1, 1))

	require.Equal([]string{registeredChainID}, countChainMetas(t, f))
}
