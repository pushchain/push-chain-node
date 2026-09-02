package txpolicy_test

import (
	"testing"

	protov2 "google.golang.org/protobuf/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app/txpolicy"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// msgsOnlyTx is the minimal sdk.Tx IsGaslessTx needs.
type msgsOnlyTx struct{ msgs []sdk.Msg }

func (t msgsOnlyTx) GetMsgs() []sdk.Msg                    { return t.msgs }
func (t msgsOnlyTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

// TestGaslessMsgTypesExcludeEthereumTx is one half of the F-2026-18197 invariant
// guard (see test/integration/uexecutor/gasless_module_sender_test.go for the
// other half).
//
// x/vm's Keeper.EthereumTx now rejects any MsgEthereumTx whose From is not the
// ECDSA signer of the raw transaction. A module account is derived from a name
// and has no key pair, so a module-signed MsgEthereumTx could never pass that
// check. Push's gasless flows are safe precisely because none of them is a
// MsgEthereumTx - they reach the EVM through CallEVM / DerivedEVMCall, which
// call ApplyMessageWithConfig directly. If a MsgEthereumTx were ever added to
// the gasless set, that flow would break 100% of the time; this test fails first.
func TestGaslessMsgTypesExcludeEthereumTx(t *testing.T) {
	t.Run("MsgEthereumTx is not gasless", func(t *testing.T) {
		tx := msgsOnlyTx{msgs: []sdk.Msg{&evmtypes.MsgEthereumTx{}}}
		require.False(t, txpolicy.IsGaslessTx(tx),
			"MsgEthereumTx must never be a gasless message type")
	})

	t.Run("MsgEthereumTx nested in authz is not gasless", func(t *testing.T) {
		inner, err := codectypes.NewAnyWithValue(&evmtypes.MsgEthereumTx{})
		require.NoError(t, err)
		tx := msgsOnlyTx{msgs: []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{inner}}}}
		require.False(t, txpolicy.IsGaslessTx(tx),
			"MsgEthereumTx nested in authz.MsgExec must never be a gasless message type")
	})

	t.Run("MsgExecutePayload stays gasless", func(t *testing.T) {
		tx := msgsOnlyTx{msgs: []sdk.Msg{&uexecutortypes.MsgExecutePayload{}}}
		require.True(t, txpolicy.IsGaslessTx(tx))
	})
}

// TestIsGaslessTxAuthzExecNesting guards the authz.MsgExec branch of IsGaslessTx.
//
// The inner-message loop is an "all must be allowlisted" check, so an empty nest
// satisfies it vacuously and would make the whole tx gasless - skipping
// DeductFeeDecorator and MinGasPriceDecorator for a zero-fee tx, and handing the
// signer a free on-chain account via AccountInitDecorator, which gates on this
// same predicate. Nothing upstream catches it: authz.MsgExec has no ValidateBasic
// in SDK v0.53.7, and the empty check lives only in the msg server, which runs
// after the fee decorators (F-2026-18816).
func TestIsGaslessTxAuthzExecNesting(t *testing.T) {
	anyOf := func(t *testing.T, msg sdk.Msg) *codectypes.Any {
		t.Helper()
		a, err := codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)
		return a
	}

	tests := []struct {
		name    string
		inner   []sdk.Msg
		gasless bool
		reason  string
	}{
		{
			name:    "empty nest is not gasless",
			inner:   nil,
			gasless: false,
			reason:  "an empty authz.MsgExec must not pass the inner allowlist loop vacuously",
		},
		{
			name:    "empty non-nil nest is not gasless",
			inner:   []sdk.Msg{},
			gasless: false,
			reason:  "a zero-length (but non-nil) inner message list must be rejected too",
		},
		{
			name:    "all-allowlisted nest stays gasless",
			inner:   []sdk.Msg{&uexecutortypes.MsgVoteInbound{}, &uexecutortypes.MsgVoteOutbound{}},
			gasless: true,
			reason:  "a nest of only allowlisted messages must remain gasless",
		},
		{
			name:    "mixed nest is not gasless",
			inner:   []sdk.Msg{&uexecutortypes.MsgVoteInbound{}, &evmtypes.MsgEthereumTx{}},
			gasless: false,
			reason:  "one non-allowlisted inner message must disqualify the whole tx",
		},
		{
			name:    "nested MsgExec is not gasless",
			inner:   []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{}}},
			gasless: false,
			reason:  "authz.MsgExec is not itself an allowlisted type, so nesting one must not recurse into a vacuous pass",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Preserve the nil vs. zero-length distinction: len() treats them the
			// same, but constructing both proves the guard does not depend on it.
			var inner []*codectypes.Any
			if tc.inner != nil {
				inner = make([]*codectypes.Any, 0, len(tc.inner))
				for _, m := range tc.inner {
					inner = append(inner, anyOf(t, m))
				}
			}

			tx := msgsOnlyTx{msgs: []sdk.Msg{&authz.MsgExec{Msgs: inner}}}
			require.Equal(t, tc.gasless, txpolicy.IsGaslessTx(tx), tc.reason)
		})
	}
}

// TestIsGaslessTxEmptyExecAlongsideAllowedMsg pins the multi-message case: the
// outer loop must not let an allowlisted sibling carry an empty nest through.
func TestIsGaslessTxEmptyExecAlongsideAllowedMsg(t *testing.T) {
	tx := msgsOnlyTx{msgs: []sdk.Msg{
		&uexecutortypes.MsgVoteInbound{},
		&authz.MsgExec{},
	}}
	require.False(t, txpolicy.IsGaslessTx(tx),
		"an empty authz.MsgExec must disqualify the tx even next to an allowlisted message")
}
