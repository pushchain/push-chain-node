package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

func result() *types.ReadResult {
	return &types.ReadResult{
		Status:              types.ReadStatus_READ_STATUS_SUCCESS,
		ResultData:          []byte{0xde, 0xad},
		ObservedBlockHeight: 8_000_000,
		ObservedBlockHash:   []byte{0xbe, 0xef},
	}
}

func keyOf(t *testing.T, id string, r *types.ReadResult) string {
	t.Helper()
	k, err := types.GetReadBallotKey(id, r)
	require.NoError(t, err)
	return k
}

// Identical observations must converge, or quorum can never form.
func TestGetReadBallotKey_IdenticalObservationsAgree(t *testing.T) {
	require.Equal(t, keyOf(t, "0xaa", result()), keyOf(t, "0xaa", result()))
}

// Every consensus-relevant field must move the key. If one did not, validators
// could finalize a ballot while disagreeing about that field.
func TestGetReadBallotKey_EveryFieldIsBinding(t *testing.T) {
	base := keyOf(t, "0xaa", result())

	for name, mutate := range map[string]func(*types.ReadResult){
		"status":       func(r *types.ReadResult) { r.Status = types.ReadStatus_READ_STATUS_ERROR },
		"result_data":  func(r *types.ReadResult) { r.ResultData = []byte{0x01} },
		"block_height": func(r *types.ReadResult) { r.ObservedBlockHeight = 8_000_001 },
		"block_hash":   func(r *types.ReadResult) { r.ObservedBlockHash = []byte{0x02} },
	} {
		t.Run(name, func(t *testing.T) {
			r := result()
			mutate(r)
			require.NotEqual(t, base, keyOf(t, "0xaa", r),
				"%s must change the ballot key", name)
		})
	}

	// and the request id itself
	require.NotEqual(t, base, keyOf(t, "0xbb", result()))
}

// Aggregates are reserved for v2 MEDIAN and must NOT participate. Hashing them now
// would make the v2 rollout consensus-breaking: the same read would map to a
// different ballot before and after aggregates start being populated.
func TestGetReadBallotKey_ExcludesAggregates(t *testing.T) {
	withAgg := result()
	withAgg.Aggregates = []*types.AggregateValue{
		{ExtractIndex: 0, Mode: 1, Value: []byte{0x09}},
	}

	require.Equal(t, keyOf(t, "0xaa", result()), keyOf(t, "0xaa", withAgg),
		"aggregates must not affect the ballot key")
}

// Field boundaries must not be forgeable by embedding the join character.
func TestGetReadBallotKey_FieldsCannotBleed(t *testing.T) {
	a := result()
	a.ResultData = []byte("A:B")
	b := result()
	b.ResultData = []byte("A")
	b.ObservedBlockHash = []byte("B")

	require.NotEqual(t, keyOf(t, "0xaa", a), keyOf(t, "0xaa", b))
}

// Request ids differing only in case are the same request.
func TestGetReadBallotKey_RequestIDCaseInsensitive(t *testing.T) {
	require.Equal(t, keyOf(t, "0xAABB", result()), keyOf(t, "0xaabb", result()))
}

func TestGetReadBallotKey_Rejects(t *testing.T) {
	_, err := types.GetReadBallotKey("", result())
	require.Error(t, err)

	_, err = types.GetReadBallotKey("0xaa", nil)
	require.Error(t, err)
}

// The ballot's deadline must land exactly on the request's, since x/uvalidator
// stores expiry as created + delta while the request carries an absolute height.
func TestBallotExpiryAfterBlocks_LandsOnRequestDeadline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expiry  uint64
		current int64
		want    int64
	}{
		{"future deadline", 500, 100, 400},
		{"next block", 101, 100, 1},
		{"from genesis", 900_000, 0, 900_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := types.BallotExpiryAfterBlocks(tc.expiry, tc.current)
			require.Equal(t, tc.want, got)
			require.Equal(t, int64(tc.expiry), tc.current+got,
				"created + delta must equal the request's own deadline")
		})
	}
}

// A ballot must never be born already expired, even if the caller slipped a
// past-deadline request through.
func TestBallotExpiryAfterBlocks_NeverBornExpired(t *testing.T) {
	for _, tc := range []struct {
		expiry  uint64
		current int64
	}{
		{100, 100}, // exactly at the deadline
		{50, 100},  // past it
		{0, 100},   // unset
	} {
		require.Equal(t, int64(1), types.BallotExpiryAfterBlocks(tc.expiry, tc.current),
			"expiry=%d current=%d", tc.expiry, tc.current)
	}
}
