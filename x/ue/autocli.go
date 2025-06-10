package module

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	modulev1 "github.com/rollchains/pchain/api/ue/v1"
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
				{
					RpcMethod: "ChainConfig",
					Use:       "chain-config --chain-id [chain-id]",
					Short:     "Query the chain configuration for a specific chain ID",
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
					RpcMethod: "DeployNMSC",
					Use:       "deploy-nmsc --account-id [account-id] --tx-hash [tx-hash]",
				},
				{
					RpcMethod: "MintPush",
					Use:       "mint-push --account-id [account-id] --tx-hash [tx-hash]",
				},
				{
					RpcMethod: "ExecutePayload",
					Use:       "execute-payload --account-id [account-id] --crosschain_payload [crosschain_payload]",
				},
				{
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
