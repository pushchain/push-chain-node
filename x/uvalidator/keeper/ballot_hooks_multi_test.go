package keeper_test

import (
	"errors"
	"testing"

	"cosmossdk.io/log"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

type spyBallotHook struct {
	calls int
	err   error
}

func (s *spyBallotHook) AfterBallotTerminal(
	sdk.Context, string, types.BallotObservationType, types.BallotStatus,
) error {
	s.calls++
	return s.err
}

func fire(t *testing.T, hooks keeper.MultiBallotHooks) error {
	t.Helper()
	return hooks.AfterBallotTerminal(
		sdk.Context{}.WithLogger(log.NewNopLogger()),
		"ballot-1",
		types.BallotObservationType_BALLOT_OBSERVATION_TYPE_READ_RESULT,
		types.BallotStatus_BALLOT_STATUS_PASSED,
	)
}

func TestMultiBallotHooks_FansOutToAll(t *testing.T) {
	a, b := &spyBallotHook{}, &spyBallotHook{}
	require.NoError(t, fire(t, keeper.NewMultiBallotHooks(a, b)))
	require.Equal(t, 1, a.calls)
	require.Equal(t, 1, b.calls)
}

// One module failing must not deny the others their notification — the terminal
// status is already decided by the time this runs.
func TestMultiBallotHooks_ErrorDoesNotStopLaterHooks(t *testing.T) {
	failing := &spyBallotHook{err: errors.New("boom")}
	after := &spyBallotHook{}

	err := fire(t, keeper.NewMultiBallotHooks(failing, after))
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
	require.Equal(t, 1, after.calls, "the hook after the failure still ran")
}

func TestMultiBallotHooks_Empty(t *testing.T) {
	require.NoError(t, fire(t, keeper.NewMultiBallotHooks()))
}
