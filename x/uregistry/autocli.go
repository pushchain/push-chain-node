package module

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	modulev1 "github.com/pushchain/push-chain-node/api/uregistry/v1"
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
					Short:     "Query the current consensus parameters",
				},
				{
					RpcMethod: "ChainConfig",
					Use:       "chain-config --chain [chain]",
					Short:     "Query the chain configuration for a specific chain",
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: modulev1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      false, // set to true if authority gated
				}, {
					RpcMethod: "AddChainConfig",
					Use:       "add-chain-config --chain-config [chain-config]",
				},
				{
					RpcMethod: "UpdateChainConfig",
					Use:       "update-chain-config --chain-config [chain-config]",
				},
			},
		},
	}
}
