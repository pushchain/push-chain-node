package ante_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app/ante"
)

// blockedVestingMsgURLs mirrors the list wired into NewCosmosAnteHandler.
var blockedVestingMsgURLs = []string{
	sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
	sdk.MsgTypeURL(&sdkvesting.MsgCreatePermanentLockedAccount{}),
	sdk.MsgTypeURL(&sdkvesting.MsgCreatePeriodicVestingAccount{}),
}

func vestingTestAddrs() (from, to sdk.AccAddress) {
	return sdk.AccAddress([]byte("from________________")), sdk.AccAddress([]byte("to__________________"))
}

// nestMsgExec wraps msgs in `depth` levels of authz.MsgExec.
func nestMsgExec(grantee sdk.AccAddress, depth int, msgs []sdk.Msg) sdk.Msg {
	inner := msgs
	var out sdk.Msg
	for i := 0; i < depth; i++ {
		exec := authz.NewMsgExec(grantee, inner)
		out = &exec
		inner = []sdk.Msg{out}
	}
	return out
}

// TestBlockedMsgsDecorator_VestingMsgs asserts that all three vesting-account
// creation msgs are rejected at the TOP LEVEL of a tx (F-2026-18201). Before
// this decorator only MsgCreateVestingAccount was blocked, and only when nested
// inside an authz.MsgExec, so a plain top-level tx created the vesting account
// that the staking-precompile underflow attack needs.
func TestBlockedMsgsDecorator_VestingMsgs(t *testing.T) {
	from, to := vestingTestAddrs()
	amount := sdk.NewCoins(sdk.NewInt64Coin("upc", 1_000_000))
	future := time.Date(9000, 1, 1, 0, 0, 0, 0, time.UTC)

	createVesting := sdkvesting.NewMsgCreateVestingAccount(from, to, amount, future.Unix(), false)
	createPermanentLocked := sdkvesting.NewMsgCreatePermanentLockedAccount(from, to, amount)
	createPeriodicVesting := sdkvesting.NewMsgCreatePeriodicVestingAccount(from, to, 0, []sdkvesting.Period{
		{Length: 3600, Amount: amount},
	})
	send := banktypes.NewMsgSend(from, to, amount)

	decorator := ante.NewBlockedMsgsDecorator(blockedVestingMsgURLs...)

	testCases := []struct {
		name    string
		msgs    []sdk.Msg
		expFail bool
	}{
		{"allowed msg passes", []sdk.Msg{send}, false},
		{"top-level MsgCreateVestingAccount", []sdk.Msg{createVesting}, true},
		{"top-level MsgCreatePermanentLockedAccount", []sdk.Msg{createPermanentLocked}, true},
		{"top-level MsgCreatePeriodicVestingAccount", []sdk.Msg{createPeriodicVesting}, true},
		{"blocked msg alongside allowed msgs", []sdk.Msg{send, createPermanentLocked, send}, true},
		{
			"blocked msg inside authz.MsgExec",
			[]sdk.Msg{nestMsgExec(from, 1, []sdk.Msg{createPermanentLocked})},
			true,
		},
		{
			"blocked msg inside deeply nested authz.MsgExec",
			[]sdk.Msg{nestMsgExec(from, 4, []sdk.Msg{createPeriodicVesting})},
			true,
		},
		{
			"allowed msg inside authz.MsgExec passes",
			[]sdk.Msg{nestMsgExec(from, 2, []sdk.Msg{send})},
			false,
		},
		{
			"nesting deeper than the cap is rejected",
			[]sdk.Msg{nestMsgExec(from, 8, []sdk.Msg{send})},
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx := mockFeeTx{msgs: tc.msgs}

			called := false
			next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
				called = true
				return ctx, nil
			}

			_, err := decorator.AnteHandle(sdk.Context{}, tx, false, next)
			if tc.expFail {
				require.Error(t, err)
				require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
				require.False(t, called, "blocked tx must not reach the next decorator")
				return
			}

			require.NoError(t, err)
			require.True(t, called, "allowed tx must reach the next decorator")
		})
	}
}

// TestBlockedMsgsDecorator_AuthzGrant asserts that an authz grant for a blocked
// vesting msg type cannot be created either, so the block cannot be sidestepped
// by pre-authorizing a grantee.
func TestBlockedMsgsDecorator_AuthzGrant(t *testing.T) {
	from, to := vestingTestAddrs()
	future := time.Date(9000, 1, 1, 0, 0, 0, 0, time.UTC)

	decorator := ante.NewBlockedMsgsDecorator(blockedVestingMsgURLs...)

	for _, msgURL := range blockedVestingMsgURLs {
		t.Run(msgURL, func(t *testing.T) {
			grant, err := authz.NewMsgGrant(from, to, authz.NewGenericAuthorization(msgURL), &future)
			require.NoError(t, err)

			_, err = decorator.AnteHandle(sdk.Context{}, mockFeeTx{msgs: []sdk.Msg{grant}}, false, noopAnteNext)
			require.Error(t, err)
			require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
		})
	}

	t.Run("grant for an allowed msg type passes", func(t *testing.T) {
		grant, err := authz.NewMsgGrant(from, to,
			authz.NewGenericAuthorization(sdk.MsgTypeURL(&banktypes.MsgSend{})), &future)
		require.NoError(t, err)

		_, err = decorator.AnteHandle(sdk.Context{}, mockFeeTx{msgs: []sdk.Msg{grant}}, false, noopAnteNext)
		require.NoError(t, err)
	})
}

func noopAnteNext(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
