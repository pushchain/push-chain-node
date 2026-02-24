package module

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	modulev1 "github.com/pushchain/push-chain-node/api/uexecutor/v1"
	modulev2 "github.com/pushchain/push-chain-node/api/uexecutor/v2"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: modulev1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the current gov gated parameters",
				},
			},
			SubCommands: map[string]*autocliv1.ServiceCommandDescriptor{
				"v2": {
					Service: modulev2.Query_ServiceDesc.ServiceName,
					RpcCommandOptions: []*autocliv1.RpcCommandOptions{
						{
							RpcMethod: "GetUniversalTx",
							Use:       "get-universal-tx --id [id]",
							Short:     "Query a UniversalTx by ID (native type, no legacy mapping)",
						},
					},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: modulev1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      false, // set to true if authority gated (to hide from cli)
				},
				{
					RpcMethod: "ExecutePayload",
					Use:       "execute-payload --universal-account [universal-account] --universal-payload [universal-payload]",
				},
			},
		},
	}
}
