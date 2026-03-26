package module

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	modulev1 "github.com/pushchain/push-chain-node/api/utss/v1"
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
					RpcMethod: "GetTssEvent",
					Use:       "event [event_id]",
					Short:     "Query a single TSS event by auto-increment ID",
				},
				{
					RpcMethod: "GetPendingTssEvent",
					Use:       "pending-event [process_id]",
					Short:     "Query a single pending TSS event by process ID",
				},
				{
					RpcMethod: "AllPendingTssEvents",
					Use:       "pending-events",
					Short:     "Query all pending TSS events (paginated)",
				},
				{
					RpcMethod: "AllTssEvents",
					Use:       "all-events",
					Short:     "Query all TSS events (paginated, includes all statuses)",
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: modulev1.Msg_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      false, // set to true if authority gated
				},
			},
		},
	}
}
