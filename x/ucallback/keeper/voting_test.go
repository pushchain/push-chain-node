package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

func obs(data byte) *types.ReadResult {
	return &types.ReadResult{
		Status:              types.ReadStatus_READ_STATUS_SUCCESS,
		ResultData:          []byte{data},
		ObservedBlockHeight: 8_000_000,
		ObservedBlockHash:   []byte{0xbe, 0xef},
	}
}

// seedVoters configures n eligible validators and returns their val addresses.
func seedVoters(t *testing.T, f *testFixture, n int) []sdk.ValAddress {
	t.Helper()
	addrs := make([]sdk.ValAddress, n)
	names := make([]string, n)
	for i := 0; i < n; i++ {
		addrs[i] = sdk.ValAddress(f.addrs[i%len(f.addrs)])
		// keep them distinct even when recycling the base accounts
		names[i] = addrs[i].String() + string(rune('a'+i))
		addrs[i] = sdk.ValAddress(names[i])
	}
	f.uvalidator.voters = names
	for _, nm := range names {
		f.uvalidator.bonded[nm] = true
	}
	return addrs
}

func seedRead(t *testing.T, f *testFixture, id string, expiry uint64) {
	t.Helper()
	require.NoError(t, f.k.SetUniversalRead(f.ctx,
		newRead(id, "0xTX", expiry, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)))
}

// A single vote below quorum moves the request to VOTING and attaches a ballot,
// but does not settle it.
func TestVoteReadResult_FirstVoteDoesNotFinalize(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4)
	seedRead(t, f, "0xaa", 500)

	finalized, err := f.k.VoteReadResult(f.ctx, v[0], "0xaa", obs(0x01))
	require.NoError(t, err)
	require.False(t, finalized, "1 of 4 is below the >2/3 threshold")

	ur, found := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.True(t, found)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status)
	require.NotEmpty(t, ur.BallotKey)
	require.Nil(t, ur.Result, "result is only attached once the ballot finalizes")

	// still offered to validators — the rest have not voted yet
	require.Equal(t, []string{"0xaa"}, pendingIDs(t, f))
}

// Agreement on the same observation reaches quorum at floor(2/3n)+1.
func TestVoteReadResult_QuorumFinalizes(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4) // votesNeeded = (2*4)/3 + 1 = 3
	seedRead(t, f, "0xaa", 500)

	for i := 0; i < 2; i++ {
		finalized, err := f.k.VoteReadResult(f.ctx, v[i], "0xaa", obs(0x01))
		require.NoError(t, err)
		require.False(t, finalized, "vote %d must not finalize", i+1)
	}

	finalized, err := f.k.VoteReadResult(f.ctx, v[2], "0xaa", obs(0x01))
	require.NoError(t, err)
	require.True(t, finalized, "third of four carries the ballot")

	ur, found := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.True(t, found)
	require.NotNil(t, ur.Result, "the winning observation is attached")
	require.Equal(t, []byte{0x01}, ur.Result.ResultData)

	require.Equal(t, 1, f.uvalidator.ballotCount(), "agreement means one ballot")
}

// Divergent observations open separate ballots and neither reaches quorum. This is
// the core property of the design: agreement is expressed by arriving at the same
// key, so disagreement simply fails to accumulate.
func TestVoteReadResult_DivergentObservationsSplitBallots(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4)
	seedRead(t, f, "0xaa", 500)

	for i, data := range []byte{0x01, 0x02, 0x03} {
		finalized, err := f.k.VoteReadResult(f.ctx, v[i], "0xaa", obs(data))
		require.NoError(t, err)
		require.False(t, finalized, "observation %d must not finalize alone", i)
	}

	require.Equal(t, 3, f.uvalidator.ballotCount(),
		"three distinct observations must produce three ballots")

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Nil(t, ur.Result, "no observation won, so none is recorded")
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, ur.Status)
}

// A minority that diverges cannot stop the majority from finalizing.
func TestVoteReadResult_MinorityDivergenceDoesNotBlock(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4)
	seedRead(t, f, "0xaa", 500)

	_, err := f.k.VoteReadResult(f.ctx, v[0], "0xaa", obs(0xff)) // the outlier
	require.NoError(t, err)

	for i := 1; i <= 2; i++ {
		_, err := f.k.VoteReadResult(f.ctx, v[i], "0xaa", obs(0x01))
		require.NoError(t, err)
	}
	finalized, err := f.k.VoteReadResult(f.ctx, v[3], "0xaa", obs(0x01))
	require.NoError(t, err)
	require.True(t, finalized)

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.Equal(t, []byte{0x01}, ur.Result.ResultData, "the majority observation wins")
}

func TestVoteReadResult_RejectsUnknownRequest(t *testing.T) {
	f := SetupTest(t)
	v := seedVoters(t, f, 4)

	_, err := f.k.VoteReadResult(f.ctx, v[0], "0xmissing", obs(0x01))
	require.ErrorContains(t, err, "not found")
}

// Once settled, further votes must be refused — otherwise the terminal hook could
// be driven a second time.
func TestVoteReadResult_RejectsSettled(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4)

	for _, st := range []types.UniversalReadStatus{
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FULFILLED,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_EXPIRED,
		types.UniversalReadStatus_UNIVERSAL_READ_STATUS_FAILED,
	} {
		id := "0x" + st.String()
		require.NoError(t, f.k.SetUniversalRead(f.ctx, newRead(id, "0xTX", 500, st)))
		_, err := f.k.VoteReadResult(f.ctx, v[0], id, obs(0x01))
		require.ErrorContains(t, err, "already", "status %s must reject votes", st)
	}
}

// Past its deadline a request stops accepting votes, independently of whether the
// sweeper has retired it yet.
func TestVoteReadResult_RejectsExpired(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)
	v := seedVoters(t, f, 4)

	seedRead(t, f, "0xpast", 50)
	_, err := f.k.VoteReadResult(f.ctx, v[0], "0xpast", obs(0x01))
	require.ErrorContains(t, err, "expired")

	// exactly at the expiry height is already too late, matching the query filter
	seedRead(t, f, "0xnow", 100)
	_, err = f.k.VoteReadResult(f.ctx, v[0], "0xnow", obs(0x01))
	require.ErrorContains(t, err, "expired")

	seedRead(t, f, "0xlive", 101)
	_, err = f.k.VoteReadResult(f.ctx, v[0], "0xlive", obs(0x01))
	require.NoError(t, err)
}

func TestVoteReadResult_RejectsNilResult(t *testing.T) {
	f := SetupTest(t)
	v := seedVoters(t, f, 4)
	seedRead(t, f, "0xaa", 500)

	_, err := f.k.VoteReadResult(f.ctx, v[0], "0xaa", nil)
	require.Error(t, err)
}

// A failure inside voting must leave no trace — the request stays exactly as it was.
func TestVoteReadResult_FailureIsAtomic(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4)
	seedRead(t, f, "0xaa", 500)

	f.uvalidator.voteErr = errTest
	_, err := f.k.VoteReadResult(f.ctx, v[0], "0xaa", obs(0x01))
	require.Error(t, err)

	ur, found := f.k.GetUniversalRead(f.ctx, "0xaa")
	require.True(t, found)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, ur.Status,
		"a failed vote must not advance the request")
	require.Empty(t, ur.BallotKey)
}

func TestVoteReadResult_RejectsWhenNoVoters(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	seedRead(t, f, "0xaa", 500)

	_, err := f.k.VoteReadResult(f.ctx, sdk.ValAddress("nobody"), "0xaa", obs(0x01))
	require.ErrorContains(t, err, "no eligible")
}

// The ballot the record points at is the one the terminal hook will resolve back.
func TestVoteReadResult_BallotResolvesBackToRequest(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(10)
	v := seedVoters(t, f, 4)
	seedRead(t, f, "0xaa", 500)

	_, err := f.k.VoteReadResult(f.ctx, v[0], "0xaa", obs(0x01))
	require.NoError(t, err)

	ur, _ := f.k.GetUniversalRead(f.ctx, "0xaa")
	back, found := f.k.GetUniversalReadByBallot(f.ctx, ur.BallotKey)
	require.True(t, found, "the terminal hook must be able to find this request")
	require.Equal(t, "0xaa", back.Id)
}
