package app

// AllCapabilities returns the wasmvm capabilities enabled on this chain.
// See https://github.com/CosmWasm/cosmwasm/blob/main/docs/CAPABILITIES-BUILT-IN.md
//
// NOTE: "stargate" is deliberately NOT enabled. It lets a contract emit an
// arbitrary encoded sdk.Msg (CosmosMsg::Any / Stargate), which reaches the
// message router without the tx ever passing through the ante handler. That is
// the nested-dispatch vector reported as F-2026-18197: an MsgEthereumTx routed
// that way skips the EVM ante entirely (signature, nonce and gas checks). No
// contract deployed on Push requires it.
func AllCapabilities() []string {
	return []string{
		"iterator",
		"staking",
		"cosmwasm_1_1",
		"cosmwasm_1_2",
		"cosmwasm_1_3",
		"cosmwasm_1_4",
	}
}
