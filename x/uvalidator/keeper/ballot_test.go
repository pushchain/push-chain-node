package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/x/uvalidator/types"
)

func TestCreateAndGetBallot(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	testCases := []struct {
		name       string
		proposalID string
		ballotType types.BallotObservationType
		voters     []string
		threshold  int64
		expiry     int64
		expectErr  bool
	}{
		{
			name:       "success; create and get ballot",
			proposalID: "proposal-1",
			ballotType: types.BallotObservationType_BALLOT_OBSERVATION_TYPE_UNSPECIFIED,
			voters:     []string{"voter1", "voter2"},
			threshold:  1,
			expiry:     10,
			expectErr:  false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ballot, err := f.k.CreateBallot(f.ctx, tc.proposalID, tc.ballotType, tc.voters, tc.threshold, tc.expiry)
			if tc.expectErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.Equal(tc.proposalID, ballot.Id)

			// Get ballot
			got, err := f.k.GetBallot(f.ctx, ballot.Id)
			require.NoError(err)
			require.Equal(ballot.Id, got.Id)
		})
	}
}

func TestGetOrCreateBallot(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// First call should create the ballot
	b1, err := f.k.GetOrCreateBallot(f.ctx, "p1", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)

	// Second call should return the same ballot without creating a new one
	b2, err := f.k.GetOrCreateBallot(f.ctx, "p1", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)
	require.Equal(b1.Id, b2.Id)
}

func TestMarkBallotExpiredAndFinalized(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	b, err := f.k.CreateBallot(f.ctx, "p2", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)

	// Mark expired
	err = f.k.MarkBallotExpired(f.ctx, b.Id)
	require.NoError(err)
	got, err := f.k.GetBallot(f.ctx, b.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)

	// Create another and mark finalized
	b2, err := f.k.CreateBallot(f.ctx, "p3", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)

	err = f.k.MarkBallotFinalized(f.ctx, b2.Id, types.BallotStatus_BALLOT_STATUS_PASSED)
	require.NoError(err)
	got2, err := f.k.GetBallot(f.ctx, b2.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PASSED, got2.Status)
}

func TestDeleteBallot(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	b, err := f.k.CreateBallot(f.ctx, "p4", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)

	err = f.k.DeleteBallot(f.ctx, b.Id)
	require.NoError(err)

	_, err = f.k.GetBallot(f.ctx, b.Id)
	require.Error(err)
}

func TestExpireBallotsBeforeHeight(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Create a ballot that will expire
	b, err := f.k.CreateBallot(f.ctx, "p5", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 1)
	require.NoError(err)

	err = f.k.ExpireBallotsBeforeHeight(f.ctx, b.BlockHeightCreated+2)
	require.NoError(err)

	got, err := f.k.GetBallot(f.ctx, b.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_EXPIRED, got.Status)
}

func TestExecuteBallot(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	b, err := f.k.CreateBallot(f.ctx, "p6", types.BallotObservationType_BALLOT_OBSERVATION_TYPE_INBOUND_TX, []string{"v1"}, 1, 5)
	require.NoError(err)

	// Simulate yes vote reaching threshold
	b.Votes = []types.VoteResult{
		types.VoteResult_VOTE_RESULT_SUCCESS,
	}

	err = f.k.SetBallot(f.ctx, b)
	require.NoError(err)

	err = f.k.ExecuteBallot(f.ctx, b.Id)
	require.NoError(err)

	got, err := f.k.GetBallot(f.ctx, b.Id)
	require.NoError(err)
	require.Equal(types.BallotStatus_BALLOT_STATUS_PASSED, got.Status)
}
