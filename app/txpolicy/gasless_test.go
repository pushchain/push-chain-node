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
