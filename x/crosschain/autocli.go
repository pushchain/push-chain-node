package module

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	modulev1 "github.com/rollchains/pchain/api/crosschain/v1"
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
					RpcMethod: "AdminParams",
					Use:       "admin-params",
					Short:     "Query the current admin parameters",
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
					RpcMethod: "UpdateAdminParams",
					Skip:      false, // set to true if authority gated (to hide from cli)
				},
				{
					RpcMethod: "DeployNMSC",
					Use:       "deploy-nmsc [user-key] [caip-string] [owner-type] [tx-hash]",
				},
				{
					RpcMethod: "MintPush",
					Use:       "mint-push [tx-hash] [caip-string]",
				},
				{
					RpcMethod: "ExecutePayload",
					Use:       "execute-payload [caip-string] [target] [value] [data-hex] [gas-limit] [max-fee-per-gas] [max-priority-fee-per-gas] [nonce] [deadline] [signature-hex]",
				},
			},
		},
	}
}
