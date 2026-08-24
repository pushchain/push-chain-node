package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression tests for F-2026-18197 (nested message dispatch bypasses the EVM ante).
//
// Ethereum signature, nonce and gas checks live only in the EVM ante handler;
// x/vm's Keeper.EthereumTx assumes the ante already ran. Any module that unpacks
// and re-dispatches an embedded sdk.Msg therefore reaches the EVM executor with
// none of those checks applied. Push had two such dispatchers wired: x/group
// (MsgSubmitProposal/MsgExec) and the CosmWasm "stargate" capability
// (CosmosMsg::Any). Both are removed; these tests keep them removed.

// groupMsgTypeURLs are the x/group entry points that unpack and dispatch a
// nested sdk.Msg. They must not resolve or route.
var groupMsgTypeURLs = []string{
	"/cosmos.group.v1.MsgSubmitProposal",
	"/cosmos.group.v1.MsgExec",
	"/cosmos.group.v1.MsgCreateGroup",
	"/cosmos.group.v1.MsgCreateGroupWithPolicy",
	"/cosmos.group.v1.MsgCreateGroupPolicy",
}

// TestGroupModuleNotWired asserts x/group is gone from every wiring point: the
// module manager, the store keys, the message router and the interface registry.
func TestGroupModuleNotWired(t *testing.T) {
	// setup() constructs the app without InitChain, which is all these
	// assertions need: the module manager, store keys, message routes and
	// interface registry are populated by then. Setup() is avoided on purpose -
	// it passes the "testing" chain ID and only works once another test has
	// already initialised the global EVM configurator.
	gapp, _ := setup(t, ChainID, false, 0)

	t.Run("not in module manager", func(t *testing.T) {
		_, ok := gapp.ModuleManager.Modules["group"]
		require.False(t, ok, "x/group must not be registered in the module manager")
	})

	t.Run("no store key", func(t *testing.T) {
		require.Nil(t, gapp.GetKey("group"), "x/group must not have a KV store key")
	})

	t.Run("msgs unroutable", func(t *testing.T) {
		for _, typeURL := range groupMsgTypeURLs {
			require.Nil(t, gapp.MsgServiceRouter().HandlerByTypeURL(typeURL),
				"%s must have no handler on the msg service router", typeURL)
		}
	})

	t.Run("msgs unresolvable", func(t *testing.T) {
		for _, typeURL := range groupMsgTypeURLs {
			_, err := gapp.InterfaceRegistry().Resolve(typeURL)
			require.Error(t, err,
				"%s must not resolve in the interface registry (tx decoding must fail)", typeURL)
		}
	})
}

// TestWasmStargateCapabilityDisabled asserts the "stargate" wasmvm capability is
// off for both wasm VMs. With it enabled, an uploaded contract may emit an
// arbitrary encoded sdk.Msg (CosmosMsg::Any / Stargate) that the wasm message
// handler forwards straight to the message router, after ante has already run.
func TestWasmStargateCapabilityDisabled(t *testing.T) {
	t.Run("x/wasm", func(t *testing.T) {
		require.NotContains(t, AllCapabilities(), "stargate")
	})

	t.Run("08-wasm light client", func(t *testing.T) {
		require.NotContains(t, capabilities, "stargate")
	})
}
