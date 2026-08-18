package types_test

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

func result() *types.ReadResult {
	return &types.ReadResult{
		Status:     types.ReadStatus_READ_STATUS_SUCCESS,
		ResultData: []byte{0xde, 0xad},
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
		"status":      func(r *types.ReadResult) { r.Status = types.ReadStatus_READ_STATUS_ERROR },
		"result_data": func(r *types.ReadResult) { r.ResultData = []byte{0x01} },
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

// Empty and nil byte fields must hash identically, or validators reporting the
// same ERROR would split across ballots depending on how their client happened to
// represent "no data". A placeholder byte must NOT be treated as empty.
func TestGetReadBallotKey_EmptyBytesAreCanonical(t *testing.T) {
	key := func(data []byte) string {
		k, err := types.GetReadBallotKey("0xaa", &types.ReadResult{
			Status:     types.ReadStatus_READ_STATUS_ERROR,
			ResultData: data,
		})
		require.NoError(t, err)
		return k
	}

	canonical := key(nil)
	require.Equal(t, canonical, key([]byte{}), "empty slice == nil")
	require.Equal(t, canonical, key([]byte("")), `[]byte("") == nil`)

	// A single zero byte is data, not absence — a validator that zero-fills would
	// land on its own ballot and quorum would never form.
	require.NotEqual(t, canonical, key([]byte{0x00}),
		"a placeholder byte must not be mistaken for empty")
}

// Disagreement about WHY a read failed is real disagreement, so the code must move
// the ballot key — otherwise REVERTED and NOT_FOUND would collapse onto one ballot.
func TestGetReadBallotKey_ErrorCodeIsBinding(t *testing.T) {
	errResult := func(code types.ReadErrorCode) *types.ReadResult {
		return &types.ReadResult{Status: types.ReadStatus_READ_STATUS_ERROR, ErrorCode: code}
	}

	seen := map[string]types.ReadErrorCode{}
	for _, code := range []types.ReadErrorCode{
		types.ReadErrorCode_READ_ERROR_UNSPECIFIED,
		types.ReadErrorCode_READ_ERROR_INVALID_QUERY,
		types.ReadErrorCode_READ_ERROR_UNSUPPORTED,
		types.ReadErrorCode_READ_ERROR_REVERTED,
		types.ReadErrorCode_READ_ERROR_NOT_FOUND,
		types.ReadErrorCode_READ_ERROR_INVALID_RESULT,
		types.ReadErrorCode_READ_ERROR_REJECTED,
	} {
		k := keyOf(t, "0xaa", errResult(code))
		if prev, dup := seen[k]; dup {
			t.Fatalf("%s and %s collide on one ballot", code, prev)
		}
		seen[k] = code
	}
	require.Len(t, seen, 7, "every error code must be its own ballot")
}

// Shapes that could never be produced by two honest validators alike are rejected
// at submission, so an operator sees an error instead of a ballot that silently
// never reaches quorum.
func TestValidateReadResult(t *testing.T) {

	for name, tc := range map[string]struct {
		result *types.ReadResult
		valid  bool
	}{
		"success": {&types.ReadResult{
			Status: types.ReadStatus_READ_STATUS_SUCCESS, ResultData: []byte{1}}, true},
		"success, empty data": {&types.ReadResult{
			Status: types.ReadStatus_READ_STATUS_SUCCESS}, true},
		"error with code": {&types.ReadResult{
			Status:    types.ReadStatus_READ_STATUS_ERROR,
			ErrorCode: types.ReadErrorCode_READ_ERROR_REVERTED}, true},
		"error without code": {&types.ReadResult{
			Status: types.ReadStatus_READ_STATUS_ERROR}, true},

		"nil":                {nil, false},
		"unspecified status": {&types.ReadResult{}, false},
		"success carrying an error code": {&types.ReadResult{
			Status:    types.ReadStatus_READ_STATUS_SUCCESS,
			ErrorCode: types.ReadErrorCode_READ_ERROR_REVERTED}, false},
		"error carrying result data": {&types.ReadResult{
			Status: types.ReadStatus_READ_STATUS_ERROR, ResultData: []byte{1}}, false},
		"v1 aggregates": {&types.ReadResult{
			Status:     types.ReadStatus_READ_STATUS_SUCCESS,
			Aggregates: []*types.AggregateValue{{Mode: 1}}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := types.ValidateReadResult(tc.result)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// Classification decides whether a failed call may be treated as terminal. Getting
// it wrong in the "already settled" direction strands the funder's deposit, since
// only the module can call expireExternalRead.
func TestClassifyCall(t *testing.T) {
	sel := func(sig string) []byte { return crypto.Keccak256([]byte(sig))[:4] }

	for name, tc := range map[string]struct {
		vmError    string
		revertData []byte
		callErr    error
		want       types.CallOutcome
	}{
		"success":                    {"", nil, nil, types.CallOK},
		"success ignores stale data": {"", sel("TransferFailed()"), nil, types.CallOK},

		"invalid callback target": {"execution reverted", sel("InvalidCallbackTarget()"), nil, types.CallAlreadySettled},

		"out of gas":            {"out of gas", nil, nil, types.CallOutOfGas},
		"code store out of gas": {"contract creation code storage out of gas", nil, nil, types.CallOutOfGas},

		"wrong module":          {"execution reverted", sel("CallerIsNotUCallbackModule()"), nil, types.CallUnsettled},
		"not yet expired":       {"execution reverted", sel("RequestNotYetExpired()"), nil, types.CallUnsettled},
		"vault refused":         {"execution reverted", sel("TransferFailed()"), nil, types.CallUnsettled},
		"unknown revert":        {"execution reverted", sel("SomethingElse()"), nil, types.CallUnsettled},
		"revert with no data":   {"execution reverted", nil, nil, types.CallUnsettled},
		"truncated revert data": {"execution reverted", []byte{0x01, 0x02}, nil, types.CallUnsettled},
		"dispatch error":        {"", nil, errTestTypes, types.CallUnsettled},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want,
				types.ClassifyCall(tc.vmError, tc.revertData, tc.callErr))
		})
	}
}

// An unrecognised revert must never be read as "settled" — that is the direction
// that loses money.
func TestClassifyCall_UnknownRevertIsNeverSettled(t *testing.T) {
	for _, sig := range []string{"Foo()", "Bar(uint256)", "Paused()", "ZeroAddress()"} {
		got := types.ClassifyCall("execution reverted", crypto.Keccak256([]byte(sig))[:4], nil)
		require.Equal(t, types.CallUnsettled, got, sig)
	}
}

var errTestTypes = errors.New("injected")

// InvalidRequestStatus replaced RequestAlreadyFulfilled, and it carries the actual
// status. Only SETTLED and EXPIRED are terminal — EXECUTED means the callback ran
// but reportCallbackGas has not, so the budget is still escrowed and the funder is
// still owed a refund.
func TestClassifyCall_InvalidRequestStatus(t *testing.T) {
	selector := crypto.Keccak256([]byte("InvalidRequestStatus(uint256,uint8,uint8)"))[:4]

	encode := func(actual uint8) []byte {
		word := func(v uint64) []byte {
			b := make([]byte, 32)
			b[31] = byte(v)
			return b
		}
		out := append([]byte{}, selector...)
		out = append(out, word(0xaa)...)           // requestId
		out = append(out, word(uint64(actual))...) // actual
		out = append(out, word(1)...)              // expected = PENDING
		return out
	}

	for name, tc := range map[string]struct {
		actual uint8
		want   types.CallOutcome
	}{
		"NONE — never existed":             {0, types.CallUnsettled},
		"PENDING — nothing happened yet":   {1, types.CallUnsettled},
		"EXECUTED — budget still escrowed": {2, types.CallUnsettled},
		"SETTLED — finished":               {3, types.CallAlreadySettled},
		"EXPIRED — finished":               {4, types.CallAlreadySettled},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want,
				types.ClassifyCall("execution reverted", encode(tc.actual), nil))
		})
	}

	// undecodable args must fall to the safe side
	require.Equal(t, types.CallUnsettled,
		types.ClassifyCall("execution reverted", selector, nil))
}

// DerivedEVMCall returns a response AND an error when the EVM reverts, so
// classification must read vmError before callErr. Checking the error first would
// discard the revert data and make CallAlreadySettled unreachable in production —
// every revert would look like "nothing settled".
func TestClassifyCall_RevertCarriesBothResponseAndError(t *testing.T) {
	sel := func(sig string) []byte { return crypto.Keccak256([]byte(sig))[:4] }
	wrapped := errors.New("failed to execute message; message index: 0: execution reverted")

	settled := append(sel("InvalidRequestStatus(uint256,uint8,uint8)"),
		make([]byte, 96)...)
	settled[4+63] = 3 // actual = SETTLED
	settled[4+95] = 1 // expected = PENDING

	require.Equal(t, types.CallAlreadySettled,
		types.ClassifyCall("execution reverted", settled, wrapped),
		"an accompanying error must not mask the revert reason")

	require.Equal(t, types.CallOutOfGas,
		types.ClassifyCall("out of gas", nil, wrapped))

	// only a revert-free failure is a true no-execution case
	require.Equal(t, types.CallUnsettled,
		types.ClassifyCall("", nil, wrapped))
}
